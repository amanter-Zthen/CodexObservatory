package routetrace

import (
	"encoding/json"
	"strings"
	"testing"
)

func note(c *Collector, thread, turn string) {
	b, _ := json.Marshal(map[string]any{"method": "turn/started", "params": map[string]any{"threadId": thread, "turn": map[string]any{"id": turn}}})
	c.FeedIPC(b, false)
}
func trace(turn, model, served string, warmup bool, stage string) []byte {
	frame, _ := json.Marshal(map[string]any{"type": stage, "response": map[string]any{"id": "resp-" + turn, "model": served, "output": "PRIVATE_BODY", "headers": map[string]any{"Authorization": "PRIVATE_TOKEN"}}})
	b, _ := json.Marshal(map[string]any{"target": "tungstenite::protocol", "level": "TRACE", "fields": map[string]any{"message": "Received message " + string(frame)}, "spans": []any{map[string]any{"name": "try_run_sampling_request", "turn_id": turn}, map[string]any{"name": "model_client.stream_responses_websocket", "model": model, "websocket.warmup": warmup}}})
	return b
}
func TestConcurrentOriginalTurnRoutingAndPrivacy(t *testing.T) {
	c := New(1)
	note(c, "thread-a", "turn-a")
	note(c, "thread-b", "turn-b")
	a, _ := c.FeedTrace(trace("turn-a", "model-a", "model-a", false, "response.completed"))
	b, _ := c.FeedTrace(trace("turn-b", "model-b", "model-c", false, "response.completed"))
	if len(a) != 1 || a[0].Thread != "thread-a" || a[0].Verdict != "match" || len(b) != 1 || b[0].Thread != "thread-b" || b[0].Verdict != "mismatch" {
		t.Fatalf("wrong association: %v %v", a, b)
	}
	data, _ := json.Marshal(append(a, b...))
	if strings.Contains(string(data), "PRIVATE") || strings.Contains(string(data), "Authorization") {
		t.Fatal("raw contents escaped allowlist")
	}
}
func TestWarmupAndUnassociatedFramesAreExcluded(t *testing.T) {
	c := New(1)
	note(c, "thread-a", "turn-a")
	for _, b := range [][]byte{trace("turn-a", "model-a", "model-b", true, "response.completed"), trace("unknown-turn", "model-a", "model-b", false, "response.completed")} {
		rows, consumed := c.FeedTrace(b)
		if len(rows) != 0 || !consumed {
			t.Fatal("warmup/helper should be private and excluded")
		}
	}
}
func TestAmbiguousTurnIDsAreExcluded(t *testing.T) {
	c := New(1)
	note(c, "thread-a", "0")
	note(c, "thread-b", "0")
	rows, _ := c.FeedTrace(trace("0", "model-a", "model-b", false, "response.completed"))
	if len(rows) != 0 {
		t.Fatal("ambiguous turn cannot be assigned by most recent activity")
	}
}
func TestFailureAndConflictingHeadersAreUnknown(t *testing.T) {
	c := New(1)
	note(c, "thread-a", "turn-a")
	rows, _ := c.FeedTrace(trace("turn-a", "model-a", "model-a", false, "response.failed"))
	if len(rows) != 1 || rows[0].Verdict != "request_failed" {
		t.Fatal("failed response is not a normal route")
	}
	var log map[string]any
	json.Unmarshal(trace("turn-a", "model-a", "model-a", false, "response.completed"), &log)
	fields := object(log["fields"])
	var frame map[string]any
	json.Unmarshal([]byte(strings.TrimPrefix(text(fields["message"]), "Received message ")), &frame)
	object(frame["response"])["headers"] = map[string]any{"openai-model": "model-b"}
	payload, _ := json.Marshal(frame)
	fields["message"] = "Received message " + string(payload)
	b, _ := json.Marshal(log)
	rows, _ = c.FeedTrace(b)
	if len(rows) != 1 || rows[0].Verdict != "conflicting_evidence" {
		t.Fatal("conflicting evidence must not show green")
	}
}
func TestWrongDirectionAndOpaqueDiagnostics(t *testing.T) {
	c := New(1)
	note(c, "a", "a")
	b := trace("a", "model-a", "model-a", false, "response.completed")
	b = []byte(strings.Replace(string(b), "Received message", "Sending message", 1))
	rows, private := c.FeedTrace(b)
	if len(rows) != 0 || !private {
		t.Fatal("outbound frame cannot prove served model")
	}
	rows, private = c.FeedTrace([]byte("TRACE tungstenite::protocol: Received message PRIVATE"))
	if len(rows) != 0 || !private {
		t.Fatal("unparsed wire contents must not reach parent log")
	}
}
