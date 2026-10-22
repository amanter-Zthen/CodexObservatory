package completion

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const Retention = 5 * 24 * time.Hour
const Capacity = 256

var threadID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func ThreadURL(thread string) (string, bool) {
	if !threadID.MatchString(thread) {
		return "", false
	}
	return "codex://threads/" + thread, true
}
func clean(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > 120 {
		r = r[:120]
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, string(r))
}
func ID(thread, turn string) string {
	h := sha256.Sum256([]byte(thread + "\x00" + turn))
	return hex.EncodeToString(h[:12])
}

type Notice struct {
	ID       string    `json:"id"`
	Thread   string    `json:"thread_id"`
	Turn     string    `json:"turn_id"`
	Title    string    `json:"title,omitempty"`
	Model    string    `json:"model,omitempty"`
	Started  time.Time `json:"started_at"`
	Finished time.Time `json:"completed_at,omitempty"`
	Status   string    `json:"status"`
	Pending  bool      `json:"pending"`
}
type State struct {
	Version int      `json:"version"`
	Records []Notice `json:"records"`
}
type Inbox struct {
	mu       sync.RWMutex
	records  map[string]Notice
	revision uint64
}

func New() *Inbox { return &Inbox{records: make(map[string]Notice)} }
func fresh(n Notice, now time.Time) bool {
	at := n.Started
	if n.Finished.After(at) {
		at = n.Finished
	}
	return !at.IsZero() && !at.Before(now.Add(-Retention)) && !at.After(now.Add(time.Minute))
}
func (i *Inbox) Restore(state State, now time.Time) {
	if state.Version != 1 {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, n := range state.Records {
		if len(i.records) >= Capacity {
			break
		}
		if _, ok := ThreadURL(n.Thread); !ok || n.Turn == "" || n.ID != ID(n.Thread, n.Turn) || !fresh(n, now) {
			continue
		}
		n.Title, n.Model = clean(n.Title), clean(n.Model)
		n.Pending = n.Pending && n.Status == "completed"
		i.records[n.Thread] = n
	}
	i.revision++
}
func (i *Inbox) Begin(thread, turn, title, model string, at, now time.Time) bool {
	if _, ok := ThreadURL(thread); !ok || turn == "" || len(turn) > 256 || at.Before(now.Add(-Retention)) || at.After(now.Add(time.Minute)) {
		return false
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	old, exists := i.records[thread]
	if exists && (old.Turn == turn || at.Before(old.Started) || !old.Finished.IsZero() && !at.After(old.Finished)) {
		return false
	}
	if !exists {
		i.makeSpace()
	}
	i.records[thread] = Notice{ID: ID(thread, turn), Thread: thread, Turn: turn, Title: clean(title), Model: clean(model), Started: at, Status: "running"}
	i.revision++
	return true
}
func (i *Inbox) Finish(thread, turn, title, model, status string, at, now time.Time) bool {
	if _, ok := ThreadURL(thread); !ok || turn == "" || len(turn) > 256 || at.Before(now.Add(-Retention)) || at.After(now.Add(time.Minute)) {
		return false
	}
	if status != "completed" && status != "failed" && status != "cancelled" {
		return false
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	old, exists := i.records[thread]
	if exists && (old.Turn != turn || at.Before(old.Started)) {
		return false
	}
	if exists && !old.Finished.IsZero() {
		if old.Status == "completed" && status != "completed" {
			old.Status = status
			old.Pending = false
			i.records[thread] = old
			i.revision++
			return true
		}
		return false
	}
	if !exists {
		i.makeSpace()
		old = Notice{ID: ID(thread, turn), Thread: thread, Turn: turn, Started: at}
	}
	old.Finished, old.Status, old.Pending = at, status, status == "completed"
	if title != "" {
		old.Title = clean(title)
	}
	if model != "" {
		old.Model = clean(model)
	}
	i.records[thread] = old
	i.revision++
	return true
}
func (i *Inbox) makeSpace() {
	if len(i.records) < Capacity {
		return
	}
	var key string
	var oldest time.Time
	for k, n := range i.records {
		at := n.Started
		if n.Finished.After(at) {
			at = n.Finished
		}
		if key == "" || at.Before(oldest) {
			key, oldest = k, at
		}
	}
	delete(i.records, key)
}
func (i *Inbox) Prune(now time.Time) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	changed := false
	for k, n := range i.records {
		if !fresh(n, now) {
			delete(i.records, k)
			changed = true
		}
	}
	if changed {
		i.revision++
	}
	return changed
}
func (i *Inbox) Count() (count int, revision uint64) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	for _, n := range i.records {
		if n.Pending {
			count++
		}
	}
	return count, i.revision
}
func (i *Inbox) Suppress(thread string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if _, exists := i.records[thread]; !exists {
		return false
	}
	delete(i.records, thread)
	i.revision++
	return true
}
func (i *Inbox) Pending() []Notice {
	i.mu.RLock()
	defer i.mu.RUnlock()
	rows := []Notice{}
	for _, n := range i.records {
		if n.Pending {
			rows = append(rows, n)
		}
	}
	sort.Slice(rows, func(a, b int) bool {
		if rows[a].Finished.Equal(rows[b].Finished) {
			return rows[a].ID < rows[b].ID
		}
		return rows[a].Finished.After(rows[b].Finished)
	})
	return rows
}
func (i *Inbox) Get(id string) (Notice, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	for _, n := range i.records {
		if n.ID == id && n.Pending {
			return n, true
		}
	}
	return Notice{}, false
}
func (i *Inbox) Viewed(id string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	for k, n := range i.records {
		if n.ID == id && n.Pending {
			n.Pending = false
			i.records[k] = n
			i.revision++
			return true
		}
	}
	return false
}
func (i *Inbox) Export() (State, uint64) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	s := State{Version: 1, Records: []Notice{}}
	for _, n := range i.records {
		s.Records = append(s.Records, n)
	}
	sort.Slice(s.Records, func(a, b int) bool { return s.Records[a].Thread < s.Records[b].Thread })
	return s, i.revision
}
