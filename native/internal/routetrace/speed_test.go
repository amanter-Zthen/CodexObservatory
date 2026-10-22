package routetrace

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestStreamingSpeedUsesAggregateCharactersThenReportedOutputTokens(t *testing.T) {
	c := New(1)
	ctx := RequestContext{Thread: "thread", Turn: "turn", Model: "wanted"}
	c.ObserveWire(ctx, map[string]any{"type": "response.created", "response": map[string]any{"id": "response", "model": "wanted", "headers": map[string]any{"openai-model": "other"}}}, "websocket", nil)
	key := "thread\x00turn\x00response"
	c.speeds[key].Started = time.Now().Add(-2 * time.Second)
	c.speeds[key].LastEmitted = time.Now().Add(-time.Second)
	rows := c.ObserveWire(ctx, map[string]any{"type": "response.output_text.delta", "response_id": "response", "delta": "你好ab"}, "websocket", nil)
	if len(rows) != 1 || rows[0].OutputCharacters != 4 || rows[0].CharactersPerSecond < 3.8 || rows[0].CharactersPerSecond > 4.2 || rows[0].TokensPerSecond != 0 || rows[0].Verdict != "mismatch" {
		t.Fatalf("live metric or routing evidence changed: %+v", rows)
	}
	if len(c.ObserveWire(ctx, map[string]any{"type": "response.output_text.delta", "response_id": "response", "delta": "PRIVATE_TEXT"}, "websocket", nil)) != 0 {
		t.Fatal("stream update not bounded")
	}
	rows = c.ObserveWire(ctx, map[string]any{"type": "response.completed", "response": map[string]any{"id": "response", "model": "wanted", "usage": map[string]any{"output_tokens": float64(120)}}}, "websocket", nil)
	if len(rows) != 1 || rows[0].OutputTokens != 120 || rows[0].TokensPerSecond < 59 || rows[0].TokensPerSecond > 61 || len(c.speeds) != 0 {
		t.Fatalf("reported token throughput: %+v", rows)
	}
	persisted, _ := json.Marshal(rows)
	if strings.Contains(string(persisted), "PRIVATE_TEXT") || strings.Contains(string(persisted), "你好") {
		t.Fatal("stream text retained")
	}
}

func TestTextEstimateHasNoModelTokenCounterSideEffects(t *testing.T) {
	if estimateTextUnits("你好abcd") != 3 || estimateTextUnits("") != 0 {
		t.Fatal("unexpected text-unit estimate")
	}
	c := New(1)
	ctx := RequestContext{Thread: "thread", Turn: "turn", Model: "wanted"}
	c.ObserveWire(ctx, map[string]any{"type": "response.created", "response": map[string]any{"id": "one", "model": "wanted"}}, "websocket", nil)
	c.speeds["thread\x00turn\x00one"].LastEmitted = time.Now().Add(-time.Second)
	rows := c.ObserveWire(ctx, map[string]any{"type": "response.output_text.delta", "response_id": "one", "delta": "你好abcd"}, "websocket", nil)
	if len(rows) != 1 || rows[0].EstimatedTokensPerSecond < 2.9 || rows[0].EstimatedTokensPerSecond > 3.1 || rows[0].OutputTokens != 0 || rows[0].TokensPerSecond != 0 {
		t.Fatal(rows)
	}
}
