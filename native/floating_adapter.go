package main

import (
	"codex-observatory/internal/floatui"
	"codex-observatory/internal/routefeed"
	"codex-observatory/internal/routetrace"
	"context"
	"net/http"
	"path/filepath"
	"sort"
	"time"
)

func (a *App) floatingEngine() *floatui.Engine {
	a.floatOnce.Do(func() { a.floatEngine = floatui.New(floatui.DefaultOptions()) })
	return a.floatEngine
}
func (a *App) acceptFloatingRoute(r routetrace.Record) {
	a.acceptFloatingRouteReplay(r, false)
}
func (a *App) acceptFloatingRouteReplay(r routetrace.Record, replay bool) {
	if r.ID == "" || r.Scope != "original_desktop_request" || r.Thread == "" || r.Turn == "" {
		return
	}
	at, err := time.Parse(time.RFC3339Nano, r.At)
	if err != nil {
		return
	}
	var kind floatui.Kind
	switch r.Stage {
	case "request.sent":
		kind = floatui.RequestSent
	case "turn.started":
		kind = floatui.TurnStarted
	case "turn.failed", "turn.interrupted", "turn.completed":
		kind = floatui.TurnFinished
	case "response.created":
		kind = floatui.ResponseCreated
	case "response.metadata", "codex.response.metadata":
		kind = floatui.ResponseMetadata
	case "response.output_text.delta":
		kind = floatui.OutputProgress
	case "response.completed":
		kind = floatui.ResponseFinished
	case "response.failed", "response.incomplete", "response.cancelled":
		kind = floatui.ResponseFailed
	default:
		return
	}
	a.cacheConsoleRoute(r)
	trusted := r.Source == "native_wire_response" || r.Source == "native_response_trace" || r.Source == "native_wire_request" || r.Source == "native_turn_lifecycle"
	// First-read rollout metadata establishes whether this is a human chat.
	// Unknown native identities are picked up by the local completion event.
	a.mu.RLock()
	knownHuman := false
	knownTurn := r.Stage == "turn.started" || r.Stage == "turn.completed" || r.Stage == "turn.failed" || r.Stage == "turn.interrupted"
	for _, file := range a.files {
		if file.Summary.Thread == r.Thread {
			knownHuman = file.Summary.Source != "subagent"
			knownTurn = knownTurn || file.Summary.CurrentTurn == r.Turn || file.LastStartedTurn == r.Turn
			for _, event := range file.Recent {
				if event.Thread == r.Thread && event.Turn == r.Turn && (event.Kind == "turn_started" || event.Kind == "turn_completed" || event.Kind == "tool_call") {
					knownTurn = true
					break
				}
			}
			break
		}
	}
	a.mu.RUnlock()
	if trusted && knownHuman && knownTurn {
		if r.Stage == "turn.started" || r.Stage == "request.sent" {
			a.observeCompletion(r.Thread, r.Turn, "start", "", r.Requested, at)
		}
		if r.Stage == "turn.completed" || r.Stage == "turn.failed" || r.Stage == "turn.interrupted" {
			status := "completed"
			if r.Stage == "turn.failed" {
				status = "failed"
			}
			if r.Stage == "turn.interrupted" {
				status = "cancelled"
			}
			a.observeCompletion(r.Thread, r.Turn, "finish", status, r.Requested, at)
		}
	}
	apply := a.floatingEngine().Apply
	if replay {
		apply = a.floatingEngine().Replay
	}
	apply(floatui.Event{ID: r.ID, Thread: r.Thread, Turn: r.Turn, Response: r.Response, At: at, Kind: kind,
		PID: r.PID, Requested: r.Requested, Reported: r.Reported, Verdict: r.Verdict, Trusted: trusted, OutputTokens: r.OutputTokens,
		ElapsedSeconds: r.ElapsedSeconds, TokenRate: r.TokensPerSecond, CharacterRate: r.CharactersPerSecond,
		EstimatedTokenRate: r.EstimatedTokensPerSecond,
		ResponseScoped:     !knownTurn,
		Cancelled:          r.Stage == "turn.interrupted"}, time.Now())
	a.restoreFloatingLifecycle(r.Thread, r.Turn)
}

func (a *App) acceptFloatingEvents(events []Event, metadata ...Summary) {
	for _, event := range events {
		at, err := time.Parse(time.RFC3339Nano, event.At)
		if err != nil || event.Thread == "" || event.Turn == "" {
			continue
		}
		var kind floatui.Kind
		switch event.Kind {
		case "turn_started":
			kind = floatui.TurnStarted
		case "turn_completed":
			kind = floatui.TurnFinished
		case "tool_call":
			kind = floatui.ToolStarted
		default:
			continue
		}
		if event.Kind == "turn_completed" && time.Since(at) < 2*time.Hour {
			a.floatLifeMu.Lock()
			if a.floatLife == nil {
				a.floatLife = make(map[string]Event)
			}
			key := event.Thread + "\x00" + event.Turn
			if old, ok := a.floatLife[key]; !ok || event.At > old.At {
				if len(a.floatLife) >= 1024 {
					for oldKey, oldEvent := range a.floatLife {
						oldAt, _ := time.Parse(time.RFC3339Nano, oldEvent.At)
						if time.Since(oldAt) > 2*time.Hour {
							delete(a.floatLife, oldKey)
						}
					}
				}
				if len(a.floatLife) < 1024 {
					a.floatLife[key] = event
				}
			}
			a.floatLifeMu.Unlock()
		}
		if at.Before(a.started) && !a.floatingEngine().HasTurn(event.Thread, event.Turn) {
			if event.Kind == "turn_started" {
				a.observeCompletion(event.Thread, event.Turn, "start", "", event.Model, at, metadata...)
			}
			continue
		}
		if event.Kind == "turn_started" {
			a.observeCompletion(event.Thread, event.Turn, "start", "", event.Model, at, metadata...)
		}
		if event.Kind == "turn_completed" {
			status := "completed"
			if str(event.Detail["status"]) == "turn_aborted" {
				status = "cancelled"
			}
			if str(event.Detail["status"]) == "interrupted" || str(event.Detail["status"]) == "cancelled" {
				status = "cancelled"
			}
			if str(event.Detail["status"]) == "failed" {
				status = "failed"
			}
			a.observeCompletion(event.Thread, event.Turn, "finish", status, event.Model, at, metadata...)
		}
		id := shortHash(event.Thread + "\x00" + event.Turn + "\x00" + event.Kind + "\x00" + event.At + "\x00" + event.LineSHA256)
		a.floatingEngine().Apply(floatui.Event{ID: id, Thread: event.Thread, Turn: event.Turn, At: at, Kind: kind,
			Requested: event.Model, Cancelled: str(event.Detail["status"]) == "turn_aborted"}, time.Now())
	}
}

func (a *App) restoreFloatingLifecycle(thread, turn string) {
	a.floatLifeMu.Lock()
	event, ok := a.floatLife[thread+"\x00"+turn]
	a.floatLifeMu.Unlock()
	if ok && a.floatingEngine().HasTurn(thread, turn) {
		at, err := time.Parse(time.RFC3339Nano, event.At)
		if err == nil {
			id := shortHash(event.Thread + "\x00" + event.Turn + "\x00" + event.Kind + "\x00" + event.At + "\x00" + event.LineSHA256)
			a.floatingEngine().Apply(floatui.Event{ID: id, Thread: thread, Turn: turn, At: at, Kind: floatui.TurnFinished, Cancelled: str(event.Detail["status"]) == "turn_aborted"}, time.Now())
		}
	}
}

// refreshFloating runs on the data path. Native paint reads only the published value.
func (a *App) refreshFloating() {
	e := a.floatingEngine()
	health := a.bridgeStatus()
	pids := []int{}
	if clients, ok := health["clients"].([]Obj); ok {
		for _, client := range clients {
			pids = append(pids, int(num(client["client_pid"])))
		}
	}
	e.SetHealth(floatui.Health{At: time.Now(), Ready: health["active"] == true, PIDs: pids})
	e.Prune(time.Now())
	a.publishFloating()
}

func (a *App) observeFloatingQuotas(home string, quotas map[string]Quota) {
	resolved, err := filepath.EvalSymlinks(home)
	if err == nil {
		home = resolved
	}
	rows := make([]floatui.QuotaEvent, 0, len(quotas))
	for _, q := range quotas {
		at, err := time.Parse(time.RFC3339Nano, q.At)
		if err != nil {
			continue
		}
		for _, w := range []*Window{q.Primary, q.Secondary} {
			if w != nil && w.Minutes != nil && *w.Minutes == 10080 {
				rows = append(rows, floatui.QuotaEvent{Bucket: q.ID, Source: canonical(home), At: at, Used: w.Used, WindowMinutes: *w.Minutes})
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].At.Equal(rows[j].At) {
			return rows[i].Bucket < rows[j].Bucket
		}
		return rows[i].At.After(rows[j].At)
	})
	for _, q := range rows {
		a.floatingEngine().ObserveQuota(q)
	}
}
func (a *App) publishFloating() {
	snapshot := a.floatingEngine().Snapshot(time.Now())
	count, completionRevision := a.completionCount()
	snapshot.PendingCompleted = count
	snapshot.Revision += completionRevision
	for {
		old := a.floatSnapshot.Load()
		if old != nil && (old.Revision > snapshot.Revision || old.Revision == snapshot.Revision && old.At.After(snapshot.At)) {
			return
		}
		if a.floatSnapshot.CompareAndSwap(old, &snapshot) {
			return
		}
	}
}

func (a *App) readFloatingSnapshot() floatui.Snapshot {
	if a.preview {
		fixtureTime := time.Since(a.started).Seconds()
		if a.inspectWindow {
			fixtureTime = 29
		}
		snapshot := floatingPreviewSnapshot(fixtureTime)
		count, revision := a.completionCount()
		snapshot.PendingCompleted = count
		snapshot.Revision += revision
		return snapshot
	}
	if state := a.floatSnapshot.Load(); state != nil {
		return *state
	}
	return floatui.Snapshot{}
}

func floatingPreviewSnapshot(t float64) floatui.Snapshot { return floatui.PreviewSnapshot(t) }

func (a *App) liveFeedLoop() {
	defer a.closeRouteAudit()
	client := &http.Client{Timeout: 250 * time.Millisecond, Transport: &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 1}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var description routefeed.Description
	var epoch string
	var sequence uint64
	var discovered time.Time
	var syncDeadline time.Time
	wake := a.routeSyncChannel()
	for {
		select {
		case <-a.ctxDone:
			return
		case <-ticker.C:
		case <-wake:
			// Reconnect only this monitor's read-only local feed subscription.
			// The collector and all Codex connections keep running.
			client.CloseIdleConnections()
			description = routefeed.Description{}
			epoch, sequence, discovered = "", 0, time.Time{}
			syncDeadline = time.Now().Add(3 * time.Second)
		}
		if !syncDeadline.IsZero() && time.Now().After(syncDeadline) {
			a.finishRouteSync("unavailable", "暂未连接采集器，监视器会继续自动重连")
			syncDeadline = time.Time{}
		}
		if time.Since(discovered) > 2*time.Second {
			var next routefeed.Description
			if loadJSON(filepath.Join(a.data, routefeed.DiscoveryFile), &next) == nil && routefeed.ValidDescription(next) {
				description = next
			}
			discovered = time.Now()
		}
		if !routefeed.ValidDescription(description) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		batch, err := routefeed.Fetch(ctx, client, description, epoch, sequence)
		cancel()
		if err != nil {
			continue
		}
		for _, item := range batch.Items {
			a.acceptFloatingRouteReplay(item.Record, !syncDeadline.IsZero())
		}
		epoch, sequence = batch.Epoch, batch.Sequence
		// Fast state publishing does not wait for report writes or a rollout scan.
		a.publishFloating()
		if !syncDeadline.IsZero() {
			a.refreshFloating()
			a.completeRouteSync()
			syncDeadline = time.Time{}
		}
		if len(batch.Items) > 0 {
			if sink := a.routeAuditSink(); sink != nil {
				records := make([]routetrace.Record, 0, len(batch.Items))
				for _, item := range batch.Items {
					records = append(records, item.Record)
				}
				sink.Publish(records)
			}
		}
	}
}
