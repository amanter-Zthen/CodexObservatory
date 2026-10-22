package main

import (
	"codex-observatory/internal/floatui"
	"time"
)

type RouteSyncStatus struct {
	State   string `json:"state"`
	Message string `json:"message"`
	At      string `json:"at"`
}

func (a *App) routeSyncChannel() <-chan struct{} {
	a.routeSyncOnce.Do(func() { a.routeSyncWake = make(chan struct{}, 1) })
	return a.routeSyncWake
}

// Called directly by the UI. One worker owns reconnection and checkpoint
// replay. Repeated clicks coalesce without I/O or new goroutines on the UI path.
func (a *App) requestRouteSync() bool {
	select {
	case <-a.ctxDone:
		return false
	default:
	}
	if a.preview || a.demo || !a.routeSyncBusy.CompareAndSwap(false, true) {
		return false
	}
	a.routeSyncChannel()
	a.routeSyncState.Store(&RouteSyncStatus{State: "syncing", Message: "正在重新同步路由…", At: now()})
	select {
	case a.routeSyncWake <- struct{}{}:
		return true
	default:
		a.routeSyncBusy.Store(false)
		return false
	}
}

func (a *App) finishRouteSync(state, message string) {
	a.routeSyncState.Store(&RouteSyncStatus{State: state, Message: message, At: now()})
	a.routeSyncBusy.Store(false)
}

func (a *App) routeSyncStatus() RouteSyncStatus {
	if s := a.routeSyncState.Load(); s != nil {
		return *s
	}
	return RouteSyncStatus{State: "idle"}
}

func (a *App) completeRouteSync() {
	snapshot := a.readFloatingSnapshot()
	health := a.bridgeStatus()
	if !snapshot.Ready {
		a.finishRouteSync("health_pending", "数据已同步，采集状态仍待确认")
		return
	}
	oldCollector := false
	if clients, ok := health["clients"].([]Obj); ok {
		for _, client := range clients {
			oldCollector = oldCollector || str(client["capture_revision"]) == "native-home-v1"
		}
	}
	awaiting, anomaly, pendingSSE := false, false, false
	a.mu.RLock()
	for _, row := range snapshot.Rows {
		anomaly = anomaly || row.Anomaly
		if row.Color == floatui.Yellow {
			awaiting = true
			latest := a.liveRoutes[row.Thread]
			pendingSSE = pendingSSE || latest.Turn == row.Turn && latest.Transport == "sse" && latest.Stage == "request.sent" && latest.Reported == ""
		}
	}
	a.mu.RUnlock()
	if oldCollector && pendingSSE {
		a.finishRouteSync("collector_update_required", "已同步；旧采集器可能漏记此类响应，请更新采集器")
	} else if awaiting {
		a.finishRouteSync("awaiting_evidence", "已重新同步，等待新的响应证据")
	} else if anomaly {
		a.finishRouteSync("synced_with_anomaly", "已重新同步；异常记录已保留")
	} else if len(snapshot.Rows) == 0 {
		a.finishRouteSync("synced", "已重新同步，当前没有活动对话")
	} else {
		a.finishRouteSync("synced", "已重新同步，当前模型证据已恢复")
	}
}

// The UI can display a short-lived result in its next popup without stopping
// the animation or taking focus with a message box.
func (a *App) routeSyncMenuLabel() string {
	s := a.routeSyncStatus()
	at, err := time.Parse(time.RFC3339Nano, s.At)
	if s.State == "idle" || err != nil || time.Since(at) > 5*time.Minute {
		return ""
	}
	return s.Message
}
