package routetrace

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWireContextUsesOriginalTurnMetadataAndKeepsMissingEvidenceUnknown(t *testing.T) {
	c := New(9)
	body := map[string]any{"model": "model-a", "client_metadata": map[string]any{"x-codex-turn-metadata": `{"thread_id":"thread-a","turn_id":"turn-a"}`}, "input": "PRIVATE_PROMPT"}
	ctx := RequestFromWire(body, nil)
	if ctx.Thread != "thread-a" || ctx.Turn != "turn-a" {
		t.Fatalf("context=%+v", ctx)
	}
	pending := c.BeginWire(ctx, "sse")
	if len(pending) != 1 || pending[0].Verdict != "pending" {
		t.Fatal("request not pending")
	}
	cases := []struct{ model, stage, want string }{{"model-a", "response.completed", "match"}, {"other", "response.completed", "mismatch"}, {"", "response.completed", "unknown"}, {"model-a-2026-09-30", "response.completed", "version_variant"}, {"model-a", "response.failed", "request_failed"}}
	for _, tc := range cases {
		r := c.ObserveWire(ctx, map[string]any{"type": tc.stage, "response": map[string]any{"id": "response-a", "model": tc.model, "output": "PRIVATE_OUTPUT"}}, "sse", nil)
		if len(r) != 1 || r[0].Verdict != tc.want {
			t.Fatalf("%+v -> %+v", tc, r)
		}
		b, _ := json.Marshal(r)
		if strings.Contains(string(b), "PRIVATE_") {
			t.Fatal("content leak")
		}
	}
	body["generate"] = false
	warmup := RequestFromWire(body, nil)
	if len(c.BeginWire(warmup, "websocket")) != 0 || len(c.ObserveWire(warmup, map[string]any{"type": "response.created", "response": map[string]any{"id": "response-a", "model": "model-a"}}, "websocket", nil)) != 0 {
		t.Fatal("warmup became route proof")
	}
}
func TestTurnFailureOverridesEarlierGreenAndKeepsExplicitMismatch(t *testing.T) {
	c := New(1)
	ctx := RequestContext{Thread: "thread", Turn: "turn", Model: "wanted"}
	frame := map[string]any{"type": "response.created", "response": map[string]any{"id": "response", "model": "wanted"}}
	c.ObserveWire(ctx, frame, "sse", nil)
	failed := []byte(`{"method":"turn/completed","params":{"threadId":"thread","turn":{"id":"turn","status":"failed"}}}`)
	rows := c.FeedIPC(failed, false)
	if len(rows) != 1 || rows[0].Verdict != "request_failed" || rows[0].Requested != "wanted" {
		t.Fatalf("failure stayed green: %+v", rows)
	}
	frame["response"].(map[string]any)["model"] = "other"
	c.ObserveWire(ctx, frame, "sse", nil)
	rows = c.FeedIPC(failed, false)
	if len(rows) != 1 || rows[0].Verdict != "mismatch" {
		t.Fatal("failure erased explicit routing mismatch")
	}
}
func TestEffectiveModelFromTopLevelMetadataAndArrayHeader(t *testing.T) {
	c := New(1)
	ctx := RequestContext{Thread: "thread", Turn: "turn", Model: "requested"}
	frame := map[string]any{"type": "response.metadata", "response_id": "resp", "headers": map[string]any{"OpenAI-Model": []any{"served"}}}
	rows := c.ObserveWire(ctx, frame, "websocket", nil)
	if len(rows) != 1 || rows[0].Reported != "served" || rows[0].Verdict != "mismatch" {
		t.Fatalf("metadata actual label missing: %+v", rows)
	}
	frame = map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp", "model": "requested", "headers": map[string]any{"openai-model": []any{"served"}}}}
	rows = c.ObserveWire(ctx, frame, "websocket", nil)
	if len(rows) != 1 || rows[0].Reported != "served" || rows[0].BodyModel != "requested" || rows[0].Verdict != "mismatch" {
		t.Fatalf("nominal body hid effective header model: %+v", rows)
	}
	c.ObserveWire(RequestContext{}, frame, "websocket", nil)
	stats := c.Stats()
	if stats["unassociated_frames"] != 1 || stats["response_frames"] != 3 {
		t.Fatalf("unassociated frames hidden: %v", stats)
	}
}
