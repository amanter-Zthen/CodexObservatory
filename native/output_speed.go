package main

import (
	"codex-observatory/internal/routetrace"
	"fmt"
	"math"
	"time"
)

func outputSpeedLabel(r routetrace.Record) string {
	if r.Stage == "response.completed" && r.OutputTokens > 0 && r.TokensPerSecond > 0 && !math.IsNaN(r.TokensPerSecond) && !math.IsInf(r.TokensPerSecond, 0) {
		return fmt.Sprintf("平均 %.1f token/秒（本次响应）", r.TokensPerSecond)
	}
	if r.Stage == "response.output_text.delta" && r.CharactersPerSecond > 0 && !math.IsNaN(r.CharactersPerSecond) && !math.IsInf(r.CharactersPerSecond, 0) {
		return fmt.Sprintf("输出 %.1f 字/秒 · 正在回答", r.CharactersPerSecond)
	}
	return ""
}

func (a *App) floatingSpeed() string {
	a.mu.RLock()
	rows := a.liveRouteSnapshot()
	a.mu.RUnlock()
	health := a.bridgeStatus()
	active := map[int]bool{}
	if clients, ok := health["clients"].([]Obj); ok {
		for _, client := range clients {
			active[int(num(client["client_pid"]))] = true
		}
	}
	for _, row := range rows {
		if !active[row.PID] {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, row.At)
		if err != nil || time.Since(at) > 10*time.Minute {
			continue
		}
		return outputSpeedLabel(row)
	}
	return ""
}
