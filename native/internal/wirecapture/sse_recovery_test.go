package wirecapture

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// A real loopback TLS connection reproduces the observed fallback: successful
// chunked Responses events without Content-Type, followed by another request
// on the same conversation. No account or external endpoint is involved.
func TestHeaderlessSSEFallbackContinuesWithoutRestart(t *testing.T) {
	for _, encoding := range []string{"identity", "gzip", "zstd"} {
		t.Run(encoding, func(t *testing.T) {
			var calls atomic.Int32
			stream := []byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"response-fallback\",\"model\":\"wanted\"}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"response-fallback\",\"model\":\"wanted\",\"usage\":{\"output_tokens\":12}}}\n\n")
			wire := encoded(t, stream, encoding)
			backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				// Nil suppresses net/http's automatic content-type sniffing.
				w.Header()["Content-Type"] = nil
				w.Header().Set("Content-Encoding", encoding)
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				w.Write(wire)
				calls.Add(1)
			}))
			defer backend.Close()
			_, client, records := testCapture(t, backend)
			for _, turn := range []string{"after-disconnect", "continue-same-conversation"} {
				requestBody, _ := json.Marshal(map[string]any{"model": "wanted", "stream": true, "client_metadata": map[string]string{"thread_id": "affected-thread", "turn_id": turn}})
				request, _ := http.NewRequest("POST", backend.URL+"/responses", bytes.NewReader(requestBody))
				response, err := client.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				actual, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil || !bytes.Equal(actual, wire) || response.StatusCode != 200 || response.Header.Get("Content-Type") != "" {
					t.Fatalf("fallback transport changed: status=%d content-type=%q error=%v", response.StatusCode, response.Header.Get("Content-Type"), err)
				}
			}
			deadline := time.Now().Add(time.Second)
			for len(records()) < 4 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			rows := records()
			if len(rows) != 4 {
				t.Fatalf("SSE responses were delivered to the client but not observed: records=%d, want 4", len(rows))
			}
			for i, row := range rows {
				wantTurn := "after-disconnect"
				if i >= 2 {
					wantTurn = "continue-same-conversation"
				}
				if row.Thread != "affected-thread" || row.Turn != wantTurn || row.Verdict != "match" || row.Reported != "wanted" || row.Transport != "sse" {
					t.Fatalf("wrong recovered association: %+v", row)
				}
			}
			if calls.Load() != 2 {
				t.Fatal("capture created or retried a request")
			}
		})
	}
}

func TestSSEObservationScopeAndMediaTypes(t *testing.T) {
	for _, test := range []struct {
		name, contentType string
		status            int
		stream            any
		want              bool
	}{
		{"explicit-sse", "text/event-stream; charset=utf-8", 200, nil, true},
		{"mixed-case", "Text/Event-Stream; Charset=UTF-8", 200, true, true},
		{"headerless-stream", "", 200, true, true},
		{"headerless-nonstream", "", 200, false, false},
		{"missing-stream-flag", "", 200, nil, false},
		{"string-stream-flag", "", 200, "true", false},
		{"json", "application/json", 200, true, false},
		{"html", "text/html", 200, true, false},
		{"fake-substring", "text/plain; note=text/event-stream", 200, true, false},
		{"failed-request", "", 502, true, false},
		{"unauthorized", "text/event-stream", 401, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := &http.Response{StatusCode: test.status, Header: make(http.Header)}
			if test.contentType != "" {
				response.Header.Set("Content-Type", test.contentType)
			}
			if got := observesSSE(response, map[string]any{"stream": test.stream}); got != test.want {
				t.Fatalf("observed=%v want=%v", got, test.want)
			}
		})
	}
}

func TestHeaderlessNonEventsNeverManufactureModelEvidence(t *testing.T) {
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header()["Content-Type"] = nil
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		io.WriteString(w, "ordinary text with wanted model\n\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"unlabelled\"}}\n\n")
	}))
	defer backend.Close()
	_, client, records := testCapture(t, backend)
	requestBody := []byte(`{"model":"wanted","stream":true,"client_metadata":{"thread_id":"thread","turn_id":"turn"}}`)
	response, err := client.Post(backend.URL+"/responses", "application/json", bytes.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	deadline := time.Now().Add(time.Second)
	for len(records()) < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	rows := records()
	if len(rows) != 1 || rows[0].Reported != "" || rows[0].Verdict != "unknown" {
		t.Fatalf("missing model must stay unknown: %+v", rows)
	}
}
