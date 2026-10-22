package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func defaultRoots() []string {
	home, _ := os.UserHomeDir()
	candidates := []string{os.Getenv("CODEX_HOME"), `D:\OpenAI\CodexHome`, filepath.Join(home, ".codex")}
	out := []string{}
	seen := map[string]bool{}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		p = filepath.Clean(p)
		if !seen[canonical(p)] && isDir(p) && (isDir(filepath.Join(p, "sessions")) || isDir(filepath.Join(p, "archived_sessions"))) {
			seen[canonical(p)] = true
			out = append(out, p)
		}
	}
	return out
}
func canonical(p string) string {
	p = filepath.Clean(p)
	if isWindows() {
		return strings.ToLower(p)
	}
	return p
}
func isDir(p string) bool { s, e := os.Stat(p); return e == nil && s.IsDir() }
func (a *App) initStore() error {
	for _, d := range []string{"sources", "history", "probes", "exports"} {
		if e := os.MkdirAll(filepath.Join(a.data, d), 0700); e != nil {
			return e
		}
	}
	a.cfg = Config{Roots: defaultRoots(), PollSeconds: 3, Aliases: map[string]string{}}
	if e := loadJSON(filepath.Join(a.data, "settings.json"), &a.cfg); e != nil && !errors.Is(e, os.ErrNotExist) {
		a.scanError = "settings.json could not be read; defaults used"
	}
	if a.cfg.PollSeconds < 2 || a.cfg.PollSeconds > 60 {
		a.cfg.PollSeconds = 3
	}
	if a.cfg.Aliases == nil {
		a.cfg.Aliases = map[string]string{}
	}
	a.files = map[string]*FileState{}
	paths, _ := filepath.Glob(filepath.Join(a.data, "sources", "*.json"))
	for _, p := range paths {
		var f FileState
		if loadJSON(p, &f) == nil && f.Schema == ParserVersion && f.Summary.Key != "" {
			if f.Summary.Quotas == nil {
				f.Summary.Quotas = map[string]Quota{}
			}
			if f.TierSchema < 1 {
				if err := a.backfillServiceTier(&f); err != nil {
					log.Printf("service tier checkpoint recovery: %v", err)
				} else if err := saveJSON(p, &f); err != nil {
					log.Printf("service tier checkpoint save: %v", err)
				}
			}
			a.files[f.Summary.Key] = &f
		}
	}
	a.marks = []Mark{}
	_ = loadJSON(filepath.Join(a.data, "marks.json"), &a.marks)
	a.probes = []*Probe{}
	pp, _ := filepath.Glob(filepath.Join(a.data, "probes", "*.json"))
	for _, p := range pp {
		var x Probe
		if loadJSON(p, &x) == nil {
			if x.Status == "RUNNING" {
				x.Status = "INTERRUPTED"
				x.Note = "Application stopped before a final outcome was saved; no retry performed."
			}
			a.probes = append(a.probes, &x)
		}
	}
	sort.Slice(a.probes, func(i, j int) bool { return a.probes[i].StartedAt > a.probes[j].StartedAt })
	seed := make([]*FileState, 0, len(a.files))
	for _, f := range a.files {
		seed = append(seed, f)
	}
	sort.Slice(seed, func(i, j int) bool { return seed[i].Summary.At > seed[j].Summary.At })
	for _, f := range seed {
		a.observeFloatingQuotas(f.Summary.Home, f.Summary.Quotas)
		a.acceptFloatingEvents(f.Recent, f.Summary)
	}
	return nil
}
func (a *App) rootEnabled(root string) bool {
	for _, r := range a.cfg.Roots {
		if canonical(r) == canonical(root) {
			return true
		}
	}
	return false
}
func (a *App) scanLoop() {
	for {
		select {
		case <-a.ctxDone:
			return
		default:
		}
		pending := a.scanOnce()
		a.mu.RLock()
		cleanupAt := a.retention.CheckedAt
		a.mu.RUnlock()
		if since, _ := time.Parse(time.RFC3339Nano, cleanupAt); time.Since(since) >= time.Minute {
			if e := a.pruneRetention(time.Now()); e != nil {
				log.Printf("retention cleanup: %v", e)
			}
		}
		a.mu.RLock()
		wait := time.Duration(a.cfg.PollSeconds) * time.Second
		a.mu.RUnlock()
		if pending {
			wait = 180 * time.Millisecond
		}
		select {
		case <-a.ctxDone:
			return
		case <-time.After(wait):
		case <-a.scanKick:
		}
	}
}

type scanFile struct {
	home, path string
	info       fs.FileInfo
}

func (a *App) scanOnce() bool {
	start := time.Now()
	a.mu.Lock()
	a.scanBusy = true
	roots := append([]string(nil), a.cfg.Roots...)
	a.mu.Unlock()
	list := []scanFile{}
	seenPaths := map[string]bool{}
	errorsSeen := []string{}
	for _, home := range roots {
		if !isDir(home) {
			errorsSeen = append(errorsSeen, "Cannot read root: "+home)
			continue
		}
		scanRoots := []string{filepath.Join(home, "sessions"), filepath.Join(home, "archived_sessions")}
		base := strings.ToLower(filepath.Base(home))
		if base == "sessions" || base == "archived_sessions" {
			scanRoots = []string{home}
		}
		for _, root := range scanRoots {
			if !isDir(root) {
				continue
			}
			_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
				if e != nil {
					if len(errorsSeen) < 8 {
						errorsSeen = append(errorsSeen, "Read denied: "+path)
					}
					return nil
				}
				select {
				case <-a.ctxDone:
					return fs.SkipAll
				default:
				}
				if d.Type()&os.ModeSymlink != 0 {
					return nil
				}
				if d.IsDir() {
					return nil
				}
				if strings.ToLower(filepath.Ext(path)) != ".jsonl" {
					return nil
				}
				if seenPaths[canonical(path)] {
					return nil
				}
				seenPaths[canonical(path)] = true
				inf, e := d.Info()
				if e != nil {
					return nil
				}
				list = append(list, scanFile{home, path, inf})
				return nil
			})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].info.ModTime().After(list[j].info.ModTime()) })
	pending := false
	var bytesRead int64
	// Each pass is bounded. Newest sessions become visible first, but old files are not skipped.
	for _, item := range list {
		select {
		case <-a.ctxDone:
			return false
		default:
		}
		key := shortHash(canonical(item.home) + "\x00" + filepath.Base(item.path))
		size := item.info.Size()
		mod := item.info.ModTime().UnixNano()
		a.mu.RLock()
		cached := a.files[key]
		unchanged := cached != nil && cached.LastMod == mod && cached.LastSize == size && cached.Summary.ReadError == "" && (cached.Offset >= size || cached.WaitingPartial) && cached.Summary.File == item.path && (a.cfg.Aliases[key] == "" || a.cfg.Aliases[key] == cached.Summary.Title)
		a.mu.RUnlock()
		if unchanged {
			continue
		}
		a.mu.RLock()
		old := a.files[key]
		var f *FileState
		if old != nil {
			f = cloneState(old)
		}
		alias := a.cfg.Aliases[key]
		a.mu.RUnlock()
		if f == nil {
			f = newState(item.home, item.path, key)
		}
		f.Summary.File = item.path
		f.Summary.Archived = strings.Contains(canonical(item.path), "archived_sessions")
		if alias != "" {
			f.Summary.Title = alias
		}
		if f.LastMod == mod && f.LastSize == size && f.Summary.ReadError == "" && (f.Offset >= size || f.WaitingPartial) {
			// Preserve archive path changes even when the content is identical.
			a.mu.Lock()
			a.files[key] = f
			a.mu.Unlock()
			continue
		}
		if time.Since(start) > 1200*time.Millisecond {
			pending = true
			continue
		}
		f.Summary.Bytes = size
		f.Summary.FileModified = item.info.ModTime().UTC().Format(time.RFC3339Nano)
		used, more, err := a.readIncrement(f, item.path, size, mod, 4*1024*1024)
		bytesRead += used
		if more {
			pending = true
		}
		f.Summary.LastScan = now()
		f.Summary.ReadError = ""
		if err != nil {
			// Never advance the committed read offset when journal persistence failed.
			if old != nil {
				f = cloneState(old)
			} else {
				f = newState(item.home, item.path, key)
			}
			f.Summary.Bytes = size
			f.Summary.FileModified = item.info.ModTime().UTC().Format(time.RFC3339Nano)
			f.Summary.LastScan = now()
			f.Summary.ReadError = limitText(err.Error(), 300)
		}
		if err == nil {
			trimStateReports(f, time.Now())
			if e := saveJSON(filepath.Join(a.data, "sources", key+".json"), f); e != nil {
				f.Summary.ReadError = "Cache write failed: " + e.Error()
			}
		}
		a.mu.Lock()
		a.files[key] = f
		a.mu.Unlock()
	}
	a.mu.Lock()
	a.lastScan = now()
	a.scanBusy = false
	a.discovered = len(list)
	a.scanBytes += bytesRead
	a.scanDuration = time.Since(start).Milliseconds()
	if len(errorsSeen) > 0 {
		a.scanError = strings.Join(errorsSeen, "; ")
	} else {
		a.scanError = ""
	}
	a.mu.Unlock()
	return pending
}
func anchorAt(file *os.File, offset int64) string {
	if offset <= 0 {
		return ""
	}
	start := offset - 512
	if start < 0 {
		start = 0
	}
	b := make([]byte, offset-start)
	n, e := file.ReadAt(b, start)
	if e != nil && e != io.EOF {
		return ""
	}
	return hashBytes(b[:n])
}
func (a *App) readIncrement(f *FileState, path string, size, mod, budget int64) (int64, bool, error) {
	a.journalMu.Lock()
	defer a.journalMu.Unlock()
	file, err := os.Open(path)
	if err != nil {
		return 0, false, err
	}
	defer file.Close()
	var journal []Event
	if f.Offset > size || (f.Offset > 0 && f.LastMod != 0 && mod != f.LastMod && size <= f.LastSize && !f.WaitingPartial) || (f.Offset > 0 && f.Anchor != "" && anchorAt(file, f.Offset) != f.Anchor) {
		gen := f.Generation + 1
		fresh := newState(f.Summary.Home, path, f.Summary.Key)
		fresh.Generation = gen
		fresh.Summary.Bytes = size
		fresh.Summary.FileModified = f.Summary.FileModified
		fresh.Summary.Title = f.Summary.Title
		*f = *fresh
		e := eventFor(f, Obj{"timestamp": now()}, []byte("source-reset"), 0, "source_reset", "file")
		e.Detail = Obj{"generation": gen, "note": "source truncated/replaced; previous history retained separately by event identity"}
		appendRecent(f, e)
		journal = append(journal, e)
	}
	start := f.Offset
	if _, err = file.Seek(start, io.SeekStart); err != nil {
		return 0, false, err
	}
	rd := bufio.NewReaderSize(file, 128*1024)
	f.WaitingPartial = false
	for f.Offset-start < budget {
		select {
		case <-a.ctxDone:
			return f.Offset - start, true, nil
		default:
		}
		b, n, complete, oversize, e := readRecord(rd)
		if e != nil && e != io.EOF {
			return f.Offset - start, false, e
		}
		if !complete {
			f.WaitingPartial = n > 0
			break
		}
		off := f.Offset
		f.Offset += n
		if oversize {
			f.Summary.OversizeLines++
			ev := eventFor(f, Obj{"timestamp": now()}, []byte("oversize-line"), off, "parser_limit", "jsonl")
			ev.Length = n
			ev.LineSHA256 = ""
			ev.Detail = Obj{"note": "record exceeds 32 MiB; content not parsed", "bytes": n}
			appendRecent(f, ev)
			journal = append(journal, ev)
			continue
		}
		b = bytes.TrimSpace(b)
		if len(b) == 0 {
			continue
		}
		journal = append(journal, parseLine(f, b, off)...)
	}
	f.Anchor = anchorAt(file, f.Offset)
	f.LastMod = mod
	f.LastSize = size
	f.Summary.Offset = f.Offset
	f.Summary.CaughtUp = f.Offset >= size
	a.acceptFloatingEvents(journal, f.Summary)
	a.observeFloatingQuotas(f.Summary.Home, f.Summary.Quotas)
	retained := []Event{}
	for _, e := range journal {
		if routeErrorEvent(e) {
			if er := a.archiveRouteEvidence("event", e.ID, e); er != nil {
				return f.Offset - start, false, er
			}
		}
		if keepEvent(e, time.Now()) {
			retained = append(retained, e)
		}
	}
	journal = retained
	if len(journal) > 0 {
		journalPath := filepath.Join(a.data, "history", f.Summary.Key+".jsonl")
		if er := repairJournalTail(journalPath); er != nil {
			return f.Offset - start, false, er
		}
		j, er := os.OpenFile(journalPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if er != nil {
			return f.Offset - start, false, er
		}
		w := bufio.NewWriter(j)
		for _, e := range journal {
			if _, er = w.Write(append(jsonBytes(e), '\n')); er != nil {
				break
			}
		}
		if er == nil {
			er = w.Flush()
		}
		if er == nil {
			er = j.Sync()
		}
		_ = j.Close()
		if er != nil {
			return f.Offset - start, false, er
		}
	}
	return f.Offset - start, f.Offset < size && !f.WaitingPartial, nil
}
func readRecord(r *bufio.Reader) ([]byte, int64, bool, bool, error) {
	var out []byte
	var n int64
	over := false
	for {
		frag, err := r.ReadSlice('\n')
		n += int64(len(frag))
		if n > maxLineBytes {
			over = true
			out = nil
		}
		if !over {
			out = append(out, frag...)
		}
		if err == nil {
			return out, n, true, over, nil
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF {
			return out, n, false, over, io.EOF
		}
		return nil, n, false, over, err
	}
}
func (a *App) history(key string, limit int) ([]Event, bool, error) {
	a.journalMu.RLock()
	defer a.journalMu.RUnlock()
	if limit < 1 || limit > 5000 {
		limit = 1200
	}
	p := filepath.Join(a.data, "history", key+".jsonl")
	f, e := os.Open(p)
	if errors.Is(e, os.ErrNotExist) {
		return []Event{}, false, nil
	}
	if e != nil {
		return nil, false, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return nil, false, e
	}
	const tailSize = 4 * 1024 * 1024
	offset := int64(0)
	if info.Size() > tailSize {
		offset = info.Size() - tailSize
	}
	_, _ = f.Seek(offset, io.SeekStart)
	r := bufio.NewReader(f)
	if offset > 0 {
		_, _ = r.ReadBytes('\n')
	}
	out := []Event{}
	seen := map[string]bool{}
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var x Event
			if loadEvent(line, &x) == nil && x.ID != "" && !seen[x.ID] && keepEvent(x, time.Now()) {
				out = append(out, x)
				seen[x.ID] = true
			}
		}
		if err != nil {
			break
		}
	}
	truncated := offset > 0 || len(out) > limit
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, truncated, nil
}
func loadEvent(b []byte, e *Event) error { return jsonDecode(b, e) }
func (a *App) addMark(key, note string) (Mark, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f := a.files[key]
	if f == nil {
		return Mark{}, fmt.Errorf("session not found")
	}
	m := Mark{ID: randomID(), At: now(), Kind: "HUMAN_OBSERVATION", Note: limitText(note, 2000), Session: cloneState(f).Summary, Nearby: append([]Event(nil), f.Recent...)}
	if len(m.Nearby) > 30 {
		m.Nearby = m.Nearby[len(m.Nearby)-30:]
	}
	a.marks = append(a.marks, m)
	if e := saveJSON(filepath.Join(a.data, "marks.json"), a.marks); e != nil {
		a.marks = a.marks[:len(a.marks)-1]
		return Mark{}, e
	}
	return m, nil
}

// A killed write can leave an incomplete final JSON line in our own journal.
// Repair only that application-owned tail, never a Codex source file.
func repairJournalTail(path string) error {
	f, e := os.OpenFile(path, os.O_RDWR, 0600)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return e
	}
	if st.Size() == 0 {
		return nil
	}
	last := []byte{0}
	if _, e = f.ReadAt(last, st.Size()-1); e != nil {
		return e
	}
	if last[0] == '\n' {
		return nil
	}
	end := st.Size()
	for end > 0 {
		start := end - int64(65536)
		if start < 0 {
			start = 0
		}
		b := make([]byte, end-start)
		if _, e = f.ReadAt(b, start); e != nil && e != io.EOF {
			return e
		}
		if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
			return f.Truncate(start + int64(i) + 1)
		}
		end = start
	}
	return f.Truncate(0)
}
