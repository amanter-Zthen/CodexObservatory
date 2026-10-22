package main

import (
	"bufio"
	"codex-observatory/internal/completion"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (a *App) completionInbox() *completion.Inbox {
	a.completionOnce.Do(func() {
		a.completed = completion.New()
		var state completion.State
		if loadJSON(filepath.Join(a.data, "completed-notifications.json"), &state) == nil {
			a.completed.Restore(state, time.Now())
		}
	})
	return a.completed
}
func (a *App) completionLabel(thread string, metadata ...Summary) (title, model string, human bool) {
	human = true
	a.mu.RLock()
	var home string
	for _, summary := range metadata {
		if summary.Thread == thread {
			human = summary.Source != "subagent"
			title = first(a.cfg.Aliases[summary.Key], summary.Title)
			model, home = summary.Model, summary.Home
			break
		}
	}
	for key, f := range a.files {
		if f.Summary.Thread == thread {
			if f.Summary.Source == "subagent" {
				human = false
			}
			title = first(title, a.cfg.Aliases[key], f.Summary.Title)
			model = first(model, f.Summary.Model)
			home = first(home, f.Summary.Home)
			break
		}
	}
	a.mu.RUnlock()
	if title == "" && home != "" {
		title = completionIndexTitle(home, thread)
	}
	return
}
func completionIndexTitle(home, thread string) string {
	f, err := os.Open(filepath.Join(home, "session_index.jsonl"))
	if err != nil {
		return ""
	}
	defer f.Close()
	scan := bufio.NewScanner(io.LimitReader(f, 2<<20))
	scan.Buffer(make([]byte, 4096), 64<<10)
	title := ""
	for scan.Scan() {
		var row struct {
			ID   string `json:"id"`
			Name string `json:"thread_name"`
		}
		if json.Unmarshal(scan.Bytes(), &row) == nil && row.ID == thread {
			title = limitText(row.Name, 120)
		}
	}
	return title
}
func (a *App) observeCompletion(thread, turn, kind, status, model string, at time.Time, metadata ...Summary) {
	if kind != "start" && kind != "finish" {
		return
	}
	title, localModel, human := a.completionLabel(thread, metadata...)
	if !human {
		if a.completionInbox().Suppress(thread) {
			a.publishFloating()
		}
		return
	}
	model = first(model, localModel)
	box := a.completionInbox()
	changed := false
	if kind == "start" {
		if time.Since(at) < 2*time.Hour {
			changed = box.Begin(thread, turn, title, model, at, time.Now())
		}
	} else if !at.Before(a.started) {
		changed = box.Finish(thread, turn, title, model, status, at, time.Now())
	}
	if changed {
		a.publishFloating()
	}
}
func (a *App) completionCount() (int, uint64) {
	if !a.preview {
		return a.completionInbox().Count()
	}
	items := a.pendingCompletions()
	return len(items), a.previewViewed.Load()
}
func (a *App) pendingCompletions() []completion.Notice {
	if !a.preview {
		return a.completionInbox().Pending()
	}
	t := time.Since(a.started).Seconds()
	if a.inspectWindow {
		t = 29
	}
	cycle := uint64(t/40) + 1
	phase := math.Mod(t, 40)
	if phase < 28 || a.previewViewed.Load() == cycle {
		return []completion.Notice{}
	}
	return []completion.Notice{{ID: "synthetic-completed", Thread: "synthetic-one", Turn: "synthetic-turn", Title: "完成示例（合成预览）", Model: "gpt-6.1-sol", Status: "completed", Pending: true, Finished: a.started.Add(time.Duration((float64(cycle)-1)*40+28) * time.Second)}}
}
func (a *App) openCompleted(id string, opener func(string) error) error {
	if a.preview {
		if id != "synthetic-completed" {
			return fmt.Errorf("unknown preview item")
		}
		a.previewViewed.Store(uint64(time.Since(a.started).Seconds()/40) + 1)
		if a.inspectWindow {
			a.previewViewed.Store(1)
		}
		return nil
	}
	box := a.completionInbox()
	notice, ok := box.Get(id)
	if !ok {
		return fmt.Errorf("this completion is no longer pending")
	}
	url, valid := completion.ThreadURL(notice.Thread)
	if !valid {
		return fmt.Errorf("invalid thread identity")
	}
	if err := opener(url); err != nil {
		return err
	}
	box.Viewed(id)
	a.publishFloating()
	return nil
}
func (a *App) completionLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var saved uint64
	flush := func() {
		box := a.completionInbox()
		box.Prune(time.Now())
		state, revision := box.Export()
		if revision == saved {
			return
		}
		if err := saveJSON(filepath.Join(a.data, "completed-notifications.json"), state); err != nil {
			log.Printf("completion state save: %v", err)
			return
		}
		saved = revision
		a.publishFloating()
	}
	for {
		select {
		case <-a.ctxDone:
			flush()
			return
		case <-ticker.C:
			flush()
		}
	}
}
func (a *App) apiCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, 405, "GET required")
		return
	}
	items := a.pendingCompletions()
	writeJSON(w, Obj{"items": items, "pending_count": len(items), "synthetic": a.preview})
}
func (a *App) apiOpenCompletion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if !body(w, r, &req) {
		return
	}
	if err := a.openCompleted(req.ID, openUI); err != nil {
		fail(w, 400, err.Error())
		return
	}
	writeJSON(w, Obj{"ok": true, "synthetic": a.preview})
}
func completionMenuLabel(n completion.Notice) string {
	title := n.Title
	if title == "" {
		title = first(n.Model, "对话") + " · " + shortHash(n.Thread)[:6]
	}
	title = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ", "&", "&&").Replace(limitText(title, 55))
	return "已完成 · " + title + " · " + n.Finished.Local().Format("15:04")
}
