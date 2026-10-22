package wirecapture

import (
	"bufio"
	"bytes"
	"codex-observatory/internal/routetrace"
	"compress/flate"
	"compress/gzip"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"github.com/klauspost/compress/zstd"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testCapture(t *testing.T, backend *httptest.Server) (*Proxy, *http.Client, func() []routetrace.Record) {
	t.Helper()
	if backend.TLS != nil {
		cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: backend.Certificate().Raw})
		path := filepath.Join(t.TempDir(), "upstream.pem")
		if err := os.WriteFile(path, cert, 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CODEX_CA_CERTIFICATE", path)
	}
	var mu sync.Mutex
	rows := []routetrace.Record{}
	p, err := Start(routetrace.New(42), func(r []routetrace.Record) { mu.Lock(); rows = append(rows, r...); mu.Unlock() }, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	// Local fixture transport bypasses the host's upstream proxy configuration.
	p.transport.Proxy = nil
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(p.CA)
	proxyURL, _ := url.Parse(p.URL)
	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{RootCAs: roots}, DisableCompression: true}
	t.Cleanup(tr.CloseIdleConnections)
	return p, &http.Client{Transport: tr, Timeout: 5 * time.Second}, func() []routetrace.Record {
		mu.Lock()
		defer mu.Unlock()
		result := []routetrace.Record{}
		for _, r := range rows {
			if r.Source == "native_wire_response" {
				result = append(result, r)
			}
		}
		return result
	}
}
func encoded(t *testing.T, b []byte, kind string) []byte {
	t.Helper()
	var out bytes.Buffer
	switch kind {
	case "gzip":
		w := gzip.NewWriter(&out)
		w.Write(b)
		w.Close()
		return out.Bytes()
	case "zstd":
		w, err := zstd.NewWriter(&out)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(b)
		w.Close()
		return out.Bytes()
	default:
		return b
	}
}
func TestHTTPSStreamingPreservesCompressedBytesAndAssociatesOriginalRequest(t *testing.T) {
	for _, kind := range []string{"identity", "gzip", "zstd"} {
		t.Run(kind, func(t *testing.T) {
			original := []byte(`{"model":"wanted-model","client_metadata":{"thread_id":"thread-A","turn_id":"turn-A"},"input":"PRIVATE_PROMPT_SENTINEL"}`)
			payload := encoded(t, original, kind)
			sse := []byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-A\",\"model\":\"other-model\",\"output\":\"PRIVATE_OUTPUT_SENTINEL\"}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-A\",\"model\":\"other-model\"}}\n\n")
			transmitted := encoded(t, sse, kind)
			var backendError string
			backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				if !bytes.Equal(b, payload) || r.Header.Get("Authorization") != "Bearer PRIVATE_CREDENTIAL_SENTINEL" {
					backendError = "request bytes or authentication changed"
				}
				if r.Header.Get("Proxy-Authorization") != "" {
					backendError = "local proxy credentials forwarded"
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Content-Encoding", kind)
				w.Write(transmitted)
			}))
			defer backend.Close()
			_, client, rows := testCapture(t, backend)
			req, _ := http.NewRequest("POST", backend.URL+"/v1/responses", bytes.NewReader(payload))
			req.Header.Set("Content-Encoding", kind)
			req.Header.Set("Authorization", "Bearer PRIVATE_CREDENTIAL_SENTINEL")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if backendError != "" {
				t.Fatal(backendError)
			}
			if !bytes.Equal(actual, transmitted) {
				t.Fatal("response bytes changed")
			}
			deadline := time.Now().Add(time.Second)
			for len(rows()) < 2 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			records := rows()
			if len(records) != 2 {
				t.Fatalf("records=%d", len(records))
			}
			for _, r := range records {
				if r.Thread != "thread-A" || r.Turn != "turn-A" || r.Verdict != "mismatch" || r.Transport != "sse" {
					t.Fatalf("wrong metadata: %+v", r)
				}
			}
			persisted, _ := json.Marshal(records)
			if strings.Contains(string(persisted), "PRIVATE_") {
				t.Fatal("content leaked into metadata")
			}
		})
	}
}
func TestUpstreamTLSIsVerifiedAndProxyRequiresAuthentication(t *testing.T) {
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer backend.Close()
	p, client, _ := testCapture(t, backend)
	p.transport.TLSClientConfig.RootCAs = x509.NewCertPool()
	resp, err := client.Get(backend.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 502 {
		t.Fatal("untrusted upstream TLS was accepted")
	}
	proxyURL, _ := url.Parse(p.URL)
	proxyURL.User = nil
	unauth := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: time.Second}
	resp, err = unauth.Get("http://127.0.0.1:1/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 407 {
		t.Fatal("proxy did not require authentication")
	}
}

func TestRetainedEndpointCAAndAuthenticationSurviveHostRestart(t *testing.T) {
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "retained identity fixture") }))
	defer backend.Close()
	first, client, _ := testCapture(t, backend)
	identity := first.Identity()
	for pass := 0; pass < 2; pass++ {
		response, err := client.Get(backend.URL + "/status")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != 200 || string(body) != "retained identity fixture" {
			t.Fatal("retained TLS identity was rejected")
		}
		if pass == 0 {
			first.Close()
			client.Transport.(*http.Transport).CloseIdleConnections()
			second, err := StartWithOptions(routetrace.New(42), func([]routetrace.Record) {}, true, identity)
			if err != nil {
				t.Fatal(err)
			}
			defer second.Close()
			second.transport.Proxy = nil
			if second.URL != first.URL || !bytes.Equal(second.CA, first.CA) {
				t.Fatal("client startup endpoint or trust changed across restart")
			}
		}
	}
}

func TestRetainedIdentityRejectsPublicListenersAndMismatchedKeys(t *testing.T) {
	p, err := Start(routetrace.New(0), func([]routetrace.Record) {}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	identity := p.Identity()
	identity.Address = "0.0.0.0:0"
	if invalid, err := StartWithOptions(routetrace.New(0), func([]routetrace.Record) {}, true, identity); err == nil {
		invalid.Close()
		t.Fatal("public listener accepted")
	}
	other, err := Start(routetrace.New(0), func([]routetrace.Record) {}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	identity.Address = "127.0.0.1:0"
	identity.Key = other.Identity().Key
	if invalid, err := StartWithOptions(routetrace.New(0), func([]routetrace.Record) {}, true, identity); err == nil {
		invalid.Close()
		t.Fatal("unmatched retained key accepted")
	}
}
func wsTestFrame(data []byte, opcode byte, fin, masked, compressed bool) []byte {
	b := opcode
	if fin {
		b |= 128
	}
	if compressed {
		b |= 64
	}
	maskFlag := byte(0)
	if masked {
		maskFlag = 128
	}
	out := []byte{b}
	if len(data) < 126 {
		out = append(out, byte(len(data))|maskFlag)
	} else {
		out = append(out, 126|maskFlag, byte(len(data)>>8), byte(len(data)))
	}
	payload := append([]byte{}, data...)
	if masked {
		mask := []byte{2, 3, 5, 7}
		out = append(out, mask...)
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return append(out, payload...)
}
func deflated(t *testing.T, data []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w, _ := flate.NewWriter(&b, flate.DefaultCompression)
	w.Write(data)
	w.Flush()
	out := append([]byte{}, b.Bytes()...)
	w.Close()
	if len(out) < 4 {
		t.Fatal("empty deflate")
	}
	return out[:len(out)-4]
}
func TestWebSocketRelayPreservesMasksFragmentationCompressionAndConcurrency(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer backend.Close()
	p, _, rows := testCapture(t, backend)
	a := &websocketContext{responses: map[string]*websocketRequest{}, headers: map[string]string{}, responseHeaders: map[string]any{}}
	b := &websocketContext{responses: map[string]*websocketRequest{}, headers: map[string]string{}, responseHeaders: map[string]any{}}
	request := func(thread, turn, model string) []byte {
		return []byte(`{"type":"response.create","model":"` + model + `","client_metadata":{"thread_id":"` + thread + `","turn_id":"` + turn + `"},"input":"PRIVATE_PROMPT_SENTINEL"}`)
	}
	relay := func(raw []byte, client bool, ctx *websocketContext) {
		var forwarded bytes.Buffer
		p.wsRelay(&forwarded, bufio.NewReader(bytes.NewReader(raw)), client, ctx)
		if !bytes.Equal(raw, forwarded.Bytes()) {
			t.Fatal("WebSocket bytes changed")
		}
	}
	data := deflated(t, request("thread-A", "turn-A", "model-A"))
	split := len(data) / 2
	fragmented := append(wsTestFrame(data[:split], 1, false, true, true), wsTestFrame([]byte("ping"), 9, true, true, false)...)
	fragmented = append(fragmented, wsTestFrame(data[split:], 0, true, true, false)...)
	relay(fragmented, true, a)
	relay(wsTestFrame(request("thread-B", "turn-B", "model-B"), 1, true, true, false), true, b)
	relay(wsTestFrame([]byte(`{"type":"response.created","response":{"id":"resp-B","model":"wrong-B","output":"PRIVATE_OUTPUT_SENTINEL"}}`), 1, true, false, false), false, b)
	relay(wsTestFrame(deflated(t, []byte(`{"type":"response.created","response":{"id":"resp-A","model":"model-A"}}`)), 1, true, false, true), false, a)
	records := rows()
	if len(records) != 2 || records[0].Thread != "thread-B" || records[0].Verdict != "mismatch" || records[1].Thread != "thread-A" || records[1].Verdict != "match" {
		t.Fatalf("wrong association: %+v", records)
	}
	persisted, _ := json.Marshal(records)
	if strings.Contains(string(persisted), "PRIVATE_") {
		t.Fatal("content persisted")
	}
}
func TestTLSWebSocketUpgradeRemainsDuplex(t *testing.T) {
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		// Consume one small masked request and send the unmodified fixture response.
		h := make([]byte, 2)
		if _, err = io.ReadFull(rw, h); err != nil {
			return
		}
		n := int(h[1] & 127)
		if n == 126 {
			var x [2]byte
			io.ReadFull(rw, x[:])
			n = int(binary.BigEndian.Uint16(x[:]))
		}
		mask := make([]byte, 4)
		io.ReadFull(rw, mask)
		body := make([]byte, n)
		io.ReadFull(rw, body)
		conn.Write(wsTestFrame([]byte(`{"type":"response.completed","response":{"id":"resp-ws","model":"ws-model"}}`), 1, true, false, false))
	}))
	defer backend.Close()
	p, _, rows := testCapture(t, backend)
	proxyURL, _ := url.Parse(p.URL)
	dest, _ := url.Parse(backend.URL)
	conn, err := net.DialTimeout("tcp", proxyURL.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(4 * time.Second))
	io.WriteString(conn, "CONNECT "+dest.Host+" HTTP/1.1\r\nHost: "+dest.Host+"\r\nProxy-Authorization: "+p.auth+"\r\n\r\n")
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: "CONNECT"})
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("CONNECT: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(p.CA)
	secured := tls.Client(&bufferedConn{Conn: conn, reader: br}, &tls.Config{RootCAs: roots, ServerName: dest.Hostname()})
	if err = secured.Handshake(); err != nil {
		t.Fatal(err)
	}
	io.WriteString(secured, "GET /v1/responses HTTP/1.1\r\nHost: "+dest.Host+"\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	wsreader := bufio.NewReader(secured)
	resp, err = http.ReadResponse(wsreader, &http.Request{Method: "GET"})
	if err != nil || resp.StatusCode != 101 {
		t.Fatalf("upgrade: %v", err)
	}
	secured.Write(wsTestFrame([]byte(`{"type":"response.create","model":"ws-model","client_metadata":{"thread_id":"thread-ws","turn_id":"turn-ws"}}`), 1, true, true, false))
	frame := make([]byte, 2)
	if _, err = io.ReadFull(wsreader, frame); err != nil {
		t.Fatal(err)
	}
	body := make([]byte, int(frame[1]&127))
	io.ReadFull(wsreader, body)
	deadline := time.Now().Add(time.Second)
	for len(rows()) < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(rows()) != 1 || rows()[0].Thread != "thread-ws" || rows()[0].Verdict != "match" {
		t.Fatalf("WebSocket evidence: %+v", rows())
	}
}

func websocketObserver(t *testing.T) (*Proxy, *websocketContext, func() []routetrace.Record) {
	t.Helper()
	var mu sync.Mutex
	var rows []routetrace.Record
	p := &Proxy{Collector: routetrace.New(42), Emit: func(records []routetrace.Record) {
		mu.Lock()
		rows = append(rows, records...)
		mu.Unlock()
	}}
	ctx := &websocketContext{responses: map[string]*websocketRequest{}, headers: map[string]string{}, responseHeaders: map[string]any{}}
	return p, ctx, func() []routetrace.Record {
		mu.Lock()
		defer mu.Unlock()
		var result []routetrace.Record
		for _, r := range rows {
			if r.Source == "native_wire_response" {
				result = append(result, r)
			}
		}
		return result
	}
}

func relayWebsocketFixture(t *testing.T, p *Proxy, ctx *websocketContext, raw []byte, fromClient bool) {
	t.Helper()
	var forwarded bytes.Buffer
	p.wsRelay(&forwarded, bufio.NewReader(bytes.NewReader(raw)), fromClient, ctx)
	if !bytes.Equal(raw, forwarded.Bytes()) {
		t.Fatal("original WebSocket bytes changed")
	}
}

func TestWebSocketAuxiliaryMetadataPreservesPendingAndSuppliesModel(t *testing.T) {
	p, ctx, rows := websocketObserver(t)
	create := []byte(`{"type":"response.create","model":"wanted","client_metadata":{"thread_id":"thread-a","turn_id":"turn-a"}}`)
	relayWebsocketFixture(t, p, ctx, wsTestFrame(create, 1, true, true, false), true)
	auxiliary := wsTestFrame([]byte(`{"type":"codex.rate_limits","rate_limits":{}}`), 1, true, false, false)
	metadata := []byte(`{"type":"codex.response.metadata","headers":{"openai-model":["wanted"],"authorization":"PRIVATE_CREDENTIAL_SENTINEL"}}`)
	auxiliary = append(auxiliary, wsTestFrame(deflated(t, metadata), 1, true, false, true)...)
	auxiliary = append(auxiliary, wsTestFrame([]byte(`{"type":"response.output_text.delta","delta":"PRIVATE_OUTPUT_SENTINEL"}`), 1, true, false, false)...)
	relayWebsocketFixture(t, p, ctx, auxiliary, false)
	if len(ctx.pending) != 1 || len(rows()) != 0 {
		t.Fatal("auxiliary event consumed a request or became model proof")
	}
	for key := range ctx.pending[0].headers {
		if key != "openai-model" {
			t.Fatalf("non-model metadata retained: %s", key)
		}
	}
	created := []byte(`{"type":"response.created","response":{"id":"response-a"}}`)
	relayWebsocketFixture(t, p, ctx, wsTestFrame(deflated(t, created), 1, true, false, true), false)
	if len(rows()) != 1 || rows()[0].Verdict != "match" || rows()[0].Reported != "wanted" {
		t.Fatalf("metadata-only model not associated: %+v", rows())
	}
	// Later metadata with an explicit response ID updates that response only.
	metadata = []byte(`{"type":"response.metadata","response_id":"response-a","headers":{"x-openai-model":"other"}}`)
	relayWebsocketFixture(t, p, ctx, wsTestFrame(metadata, 1, true, false, false), false)
	if len(rows()) != 2 || rows()[1].Verdict != "mismatch" || rows()[1].Reported != "other" {
		t.Fatalf("late metadata did not update the live response: %+v", rows())
	}
	relayWebsocketFixture(t, p, ctx, wsTestFrame([]byte(`{"type":"response.completed","response":{"id":"response-a"}}`), 1, true, false, false), false)
	if len(rows()) != 3 || rows()[2].Verdict != "mismatch" || rows()[2].Reported != "other" || len(ctx.pending) != 0 {
		t.Fatalf("response metadata association: %+v", rows())
	}
	persisted, _ := json.Marshal(rows())
	if strings.Contains(string(persisted), "PRIVATE_") {
		t.Fatal("private content retained in routing records")
	}
}

func TestWebSocketWarmupAndSequentialSamplingKeepPerRequestModels(t *testing.T) {
	p, ctx, rows := websocketObserver(t)
	ctx.responseHeaders["openai-model"] = "handshake-warmup-model"
	create := func(model string, warmup bool) []byte {
		body := map[string]any{"type": "response.create", "model": model, "client_metadata": map[string]any{"thread_id": "thread-a", "turn_id": "turn-a"}}
		if warmup {
			body["generate"] = false
		}
		b, _ := json.Marshal(body)
		return wsTestFrame(b, 1, true, true, false)
	}
	server := func(b string) {
		relayWebsocketFixture(t, p, ctx, wsTestFrame([]byte(b), 1, true, false, false), false)
	}
	relayWebsocketFixture(t, p, ctx, create("wanted", true), true)
	server(`{"type":"codex.rate_limits"}`)
	server(`{"type":"codex.response.metadata","headers":{"openai-model":"warmup-model"}}`)
	server(`{"type":"response.created","response":{"id":"response-warmup"}}`)
	server(`{"type":"response.completed","response":{"id":"response-warmup"}}`)
	if len(rows()) != 0 {
		t.Fatal("warmup became a routing proof")
	}
	relayWebsocketFixture(t, p, ctx, create("wanted", false), true)
	server(`{"type":"response.created","response":{"id":"response-first"}}`)
	if len(rows()) != 1 || rows()[0].Verdict != "unknown" {
		t.Fatal("warmup or handshake label leaked into the first real sample")
	}
	server(`{"type":"response.metadata","headers":{"openai-model":"wanted"}}`)
	server(`{"type":"response.completed","response":{"id":"response-first"}}`)
	relayWebsocketFixture(t, p, ctx, create("second-model", false), true)
	server(`{"type":"codex.response.metadata","headers":{"openai-model":"second-model"}}`)
	server(`{"type":"response.created","response":{"id":"response-second"}}`)
	server(`{"type":"response.output_text.delta","delta":"PRIVATE_OUTPUT"}`)
	server(`{"type":"response.completed","response":{"id":"response-second"}}`)
	server(`{"type":"codex.response.metadata","headers":{"openai-model":"completed-response-label"}}`)
	relayWebsocketFixture(t, p, ctx, create("third-model", false), true)
	server(`{"type":"response.created","response":{"id":"response-third"}}`)
	server(`{"type":"response.completed","response":{"id":"response-third"}}`)
	records := rows()
	if len(records) != 7 {
		t.Fatalf("real response records=%d", len(records))
	}
	want := []string{"unknown", "match", "match", "match", "match", "unknown", "unknown"}
	for i, verdict := range want {
		if records[i].Verdict != verdict || records[i].Thread != "thread-a" || records[i].Turn != "turn-a" {
			t.Fatalf("sample %d: %+v", i, records[i])
		}
	}
}

type synchronousResponseWriter struct {
	bytes.Buffer
	threshold int
	reply     func()
	called    bool
}

func (w *synchronousResponseWriter) Write(b []byte) (int, error) {
	n, err := w.Buffer.Write(b)
	if !w.called && w.Len() >= w.threshold {
		w.called = true
		w.reply()
	}
	return n, err
}

func TestWebSocketRegistersRequestBeforeFinalPayloadWriteReturns(t *testing.T) {
	p, ctx, rows := websocketObserver(t)
	create := wsTestFrame([]byte(`{"type":"response.create","model":"wanted","client_metadata":{"thread_id":"thread-a","turn_id":"turn-a"}}`), 1, true, true, false)
	writer := &synchronousResponseWriter{threshold: len(create)}
	writer.reply = func() {
		response := wsTestFrame([]byte(`{"type":"response.created","response":{"id":"response-immediate","model":"wanted"}}`), 1, true, false, false)
		response = append(response, wsTestFrame([]byte(`{"type":"response.completed","response":{"id":"response-immediate","model":"wanted"}}`), 1, true, false, false)...)
		relayWebsocketFixture(t, p, ctx, response, false)
	}
	p.wsRelay(writer, bufio.NewReader(bytes.NewReader(create)), true, ctx)
	if !writer.called || !bytes.Equal(create, writer.Bytes()) {
		t.Fatal("immediate response fixture did not preserve original bytes")
	}
	if len(rows()) != 2 || rows()[0].Verdict != "match" || rows()[1].Verdict != "match" || rows()[0].Turn != "turn-a" {
		t.Fatalf("immediate response lost its request: %+v", rows())
	}
}

func TestWebSocketOverlappingCreatesKeepMetadataAssociationUncertain(t *testing.T) {
	p, ctx, rows := websocketObserver(t)
	for _, suffix := range []string{"a", "b"} {
		create := []byte(`{"type":"response.create","model":"wanted","client_metadata":{"thread_id":"thread-` + suffix + `","turn_id":"turn-` + suffix + `"}}`)
		relayWebsocketFixture(t, p, ctx, wsTestFrame(create, 1, true, true, false), true)
	}
	relayWebsocketFixture(t, p, ctx, wsTestFrame([]byte(`{"type":"codex.response.metadata","headers":{"openai-model":"wanted"}}`), 1, true, false, false), false)
	if len(ctx.pending) != 2 {
		t.Fatal("ambiguous metadata consumed pending requests")
	}
	for _, suffix := range []string{"a", "b"} {
		for _, stage := range []string{"response.created", "response.completed"} {
			frame := []byte(`{"type":"` + stage + `","response":{"id":"response-` + suffix + `","model":"wanted"}}`)
			relayWebsocketFixture(t, p, ctx, wsTestFrame(frame, 1, true, false, false), false)
		}
	}
	if len(rows()) != 0 {
		t.Fatalf("overlapping creates produced an unsupported route proof: %+v", rows())
	}
}

func TestWebSocketLateMetadataKeepsResponseIdentityAndBodyConflict(t *testing.T) {
	p, ctx, rows := websocketObserver(t)
	server := func(value string) {
		relayWebsocketFixture(t, p, ctx, wsTestFrame([]byte(value), 1, true, false, false), false)
	}
	relayWebsocketFixture(t, p, ctx, wsTestFrame([]byte(`{"type":"response.create","model":"wanted","client_metadata":{"thread_id":"thread-a","turn_id":"turn-a"}}`), 1, true, true, false), true)
	server(`{"type":"response.created","response":{"id":"response-a","model":"body-other"}}`)
	server(`{"type":"response.metadata","response_id":"foreign-response","headers":{"openai-model":"foreign-model"}}`)
	if len(rows()) != 1 {
		t.Fatal("foreign response metadata changed the active route")
	}
	server(`{"type":"codex.response.metadata","headers":{"openai-model":"wanted"}}`)
	if len(rows()) != 2 || rows()[1].Response != "response-a" || rows()[1].Verdict != "conflicting_evidence" || rows()[1].BodyModel != "body-other" {
		t.Fatalf("late metadata lost established evidence: %+v", rows())
	}
}

func TestSSEMetadataLabelsRemainScopedToOneOriginalRequest(t *testing.T) {
	requestNumber := 0
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		requestNumber++
		w.Header().Set("Content-Type", "text/event-stream")
		if requestNumber == 1 {
			io.WriteString(w, "data: {\"type\":\"codex.response.metadata\",\"headers\":{\"openai-model\":[\"wanted\"],\"authorization\":\"PRIVATE_CREDENTIAL\"}}\n\n")
		}
		io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"response-sse\"}}\n\n")
		if requestNumber == 1 {
			io.WriteString(w, "data: {\"type\":\"response.metadata\",\"headers\":{\"x-openai-model\":\"other\"}}\n\n")
		}
		io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"response-sse\"}}\n\ndata: [DONE]\n\n")
	}))
	defer backend.Close()
	_, client, rows := testCapture(t, backend)
	for i := 0; i < 2; i++ {
		body := []byte(`{"model":"wanted","client_metadata":{"thread_id":"thread-sse","turn_id":"turn-sse"}}`)
		response, err := client.Post(backend.URL+"/v1/responses", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
	}
	deadline := time.Now().Add(time.Second)
	for len(rows()) < 4 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	records := rows()
	want := []string{"match", "mismatch", "unknown", "unknown"}
	if len(records) != len(want) {
		t.Fatalf("SSE response records=%d", len(records))
	}
	for i, verdict := range want {
		if records[i].Verdict != verdict {
			t.Fatalf("SSE sample %d: %+v", i, records[i])
		}
	}
	persisted, _ := json.Marshal(records)
	if strings.Contains(string(persisted), "PRIVATE_") {
		t.Fatal("non-model SSE metadata retained")
	}
}
