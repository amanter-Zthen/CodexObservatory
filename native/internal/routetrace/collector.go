// Package routetrace extracts only routing metadata from the original client's
// diagnostic stream. It never retains prompt, output, headers or raw frames.
package routetrace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type Record struct {
	ID                       string  `json:"id"`
	At                       string  `json:"at"`
	Thread                   string  `json:"thread_id"`
	Turn                     string  `json:"turn_id"`
	Response                 string  `json:"response_id,omitempty"`
	Requested                string  `json:"requested_model"`
	Reported                 string  `json:"reported_model,omitempty"`
	HeaderModel              string  `json:"header_model,omitempty"`
	BodyModel                string  `json:"body_model,omitempty"`
	Stage                    string  `json:"stage"`
	Verdict                  string  `json:"verdict"`
	Source                   string  `json:"source"`
	Scope                    string  `json:"scope"`
	Transport                string  `json:"transport"`
	PID                      int     `json:"client_pid"`
	OutputTokens             int64   `json:"output_tokens,omitempty"`
	OutputCharacters         int64   `json:"output_characters,omitempty"`
	ElapsedSeconds           float64 `json:"elapsed_seconds,omitempty"`
	TokensPerSecond          float64 `json:"tokens_per_second,omitempty"`
	CharactersPerSecond      float64 `json:"characters_per_second,omitempty"`
	EstimatedTokensPerSecond float64 `json:"estimated_visible_tokens_per_second,omitempty"`
}

type RequestContext struct {
	Thread, Turn, Model string
	Warmup              bool
}

func RequestFromWire(body map[string]any, headers map[string]string) RequestContext {
	meta := object(body["client_metadata"])
	extra := map[string]any{}
	raw := text(meta["x-codex-turn-metadata"])
	if raw == "" {
		raw = headers["x-codex-turn-metadata"]
	}
	_ = json.Unmarshal([]byte(raw), &extra)
	thread := text(meta["thread_id"])
	if thread == "" {
		thread = headers["thread-id"]
	}
	if thread == "" {
		thread = text(extra["thread_id"])
	}
	turn := text(meta["turn_id"])
	if turn == "" {
		turn = text(extra["turn_id"])
	}
	warmup := body["generate"] == false || text(meta["request_kind"]) == "warmup"
	return RequestContext{Thread: validID(thread), Turn: validID(turn), Model: validModel(text(body["model"])), Warmup: warmup}
}
func (c *Collector) ObserveWire(ctx RequestContext, frame map[string]any, transport string, headers map[string]any) []Record {
	stage := text(frame["type"])
	if stage != "response.created" && stage != "response.completed" && stage != "response.failed" && stage != "response.incomplete" && stage != "response.cancelled" && stage != "response.metadata" && stage != "codex.response.metadata" && stage != "response.output_text.delta" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.frames++
	if ctx.Warmup {
		c.warmupFrames++
		return nil
	}
	if ctx.Thread == "" || ctx.Turn == "" || ctx.Model == "" {
		c.unassociated++
		return nil
	}
	response := object(frame["response"])
	id := validID(text(response["id"]))
	if id == "" {
		id = validID(text(frame["response_id"]))
	}
	if id == "" {
		c.unassociated++
		return nil
	}
	body := validModel(text(response["model"]))
	header := headerModel(object(response["headers"]))
	if header == "" {
		header = headerModel(object(frame["headers"]))
	}
	if header == "" {
		header = headerModel(headers)
	}
	previous := c.latest[ctx.Thread+"\x00"+ctx.Turn]
	if previous.Response == id {
		if header == "" {
			header = previous.HeaderModel
		}
		if body == "" && (stage == "response.output_text.delta" || stage == "response.metadata" || stage == "codex.response.metadata") {
			body = previous.BodyModel
		}
	}
	key := ctx.Thread + "\x00" + ctx.Turn + "\x00" + id
	measurement := c.speeds[key]
	if measurement == nil {
		measurement = &responseSpeed{Started: time.Now(), LastEmitted: time.Now()}
		c.speeds[key] = measurement
	}
	if stage == "response.output_text.delta" {
		measurement.Characters += int64(utf8.RuneCountInString(text(frame["delta"])))
		measurement.Estimated += estimateTextUnits(text(frame["delta"]))
		if time.Since(measurement.LastEmitted) < 200*time.Millisecond {
			return nil
		}
	}
	reported := header
	if reported == "" {
		reported = body
	}
	verdict := "unknown"
	if reported != "" {
		if strings.EqualFold(ctx.Model, reported) {
			verdict = "match"
		} else {
			verdict = "mismatch"
		}
	}
	if verdict == "mismatch" && datedSuffix.MatchString(reported) && strings.EqualFold(datedSuffix.ReplaceAllString(reported, ""), ctx.Model) {
		verdict = "version_variant"
	}
	if header != "" && body != "" && !strings.EqualFold(header, body) && verdict == "match" {
		verdict = "conflicting_evidence"
	}
	if stage == "response.failed" || stage == "response.incomplete" || stage == "response.cancelled" {
		verdict = "request_failed"
	}
	r := c.record(ctx.Thread, ctx.Turn, id, ctx.Model, reported, header, stage, verdict, "native_wire_response", transport)
	r.BodyModel = body
	r.ElapsedSeconds = time.Since(measurement.Started).Seconds()
	r.OutputCharacters = measurement.Characters
	if elapsed := time.Since(measurement.LastEmitted).Seconds(); elapsed > 0 && stage == "response.output_text.delta" {
		r.CharactersPerSecond = float64(measurement.Characters-measurement.LastCharacters) / elapsed
		r.EstimatedTokensPerSecond = (measurement.Estimated - measurement.LastEstimated) / elapsed
	} else if r.ElapsedSeconds > 0 {
		r.CharactersPerSecond = float64(measurement.Characters) / r.ElapsedSeconds
	}
	if stage == "response.completed" {
		if tokens, ok := object(response["usage"])["output_tokens"].(float64); ok && tokens > 0 && tokens < 1e10 {
			r.OutputTokens = int64(tokens)
			r.TokensPerSecond = float64(r.OutputTokens) / r.ElapsedSeconds
		}
	}
	measurement.LastEmitted = time.Now()
	measurement.LastCharacters = measurement.Characters
	measurement.LastEstimated = measurement.Estimated
	c.latest[ctx.Thread+"\x00"+ctx.Turn] = r
	if stage == "response.completed" || stage == "response.failed" || stage == "response.incomplete" || stage == "response.cancelled" {
		delete(c.speeds, key)
	}
	if len(c.speeds) > 4096 {
		c.speeds = map[string]*responseSpeed{}
	}
	return []Record{r}
}
func (c *Collector) BeginWire(ctx RequestContext, transport string) []Record {
	if ctx.Warmup || ctx.Thread == "" || ctx.Turn == "" || ctx.Model == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return []Record{c.record(ctx.Thread, ctx.Turn, "", ctx.Model, "", "", "request.sent", "pending", "native_wire_request", transport)}
}

type Collector struct {
	mu                                                                       sync.Mutex
	PID                                                                      int
	turns                                                                    map[string]map[string]bool
	requests                                                                 map[string]string
	counter                                                                  uint64
	frames                                                                   uint64
	unassociated                                                             uint64
	latest                                                                   map[string]Record
	speeds                                                                   map[string]*responseSpeed
	wireMessages, wireParseErrors, wireMetadata, wireAuxiliary, warmupFrames uint64
}

type responseSpeed struct {
	Started, LastEmitted       time.Time
	Characters, LastCharacters int64
	Estimated, LastEstimated   float64
}

// Lightweight visual units, deliberately separate from service token counts.
func estimateTextUnits(s string) float64 {
	units := 0.0
	for _, r := range s {
		switch {
		case r >= 0x2e80 && r <= 0x9fff || r >= 0xac00 && r <= 0xd7af || r >= 0xf900 && r <= 0xfaff || r >= 0x20000:
			units++
		case r < 128:
			units += 0.25
		default:
			units += 0.5
		}
	}
	return units
}

func (c *Collector) Stats() map[string]uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return map[string]uint64{"response_frames": c.frames, "unassociated_frames": c.unassociated, "metadata_records": c.counter, "wire_messages": c.wireMessages, "wire_parse_errors": c.wireParseErrors, "wire_metadata_messages": c.wireMetadata, "wire_auxiliary_messages": c.wireAuxiliary, "excluded_warmup_frames": c.warmupFrames}
}

// Count only message shapes, keeping raw frames and text out of diagnostics.
func (c *Collector) NoteWireMessage(stage string, parsed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wireMessages++
	if !parsed {
		c.wireParseErrors++
		return
	}
	switch stage {
	case "response.metadata", "codex.response.metadata":
		c.wireMetadata++
	case "response.created", "response.completed", "response.failed", "response.incomplete", "response.cancelled":
	default:
		c.wireAuxiliary++
	}
}

var datedSuffix = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)

func New(pid int) *Collector {
	return &Collector{PID: pid, turns: map[string]map[string]bool{}, requests: map[string]string{}, latest: map[string]Record{}, speeds: map[string]*responseSpeed{}}
}
func (c *Collector) SetPID(pid int) { c.mu.Lock(); defer c.mu.Unlock(); c.PID = pid }
func text(v any) string             { s, _ := v.(string); return s }
func object(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
func validModel(s string) string {
	if len(s) == 0 || len(s) > 160 {
		return ""
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:/+-", r)) {
			return ""
		}
	}
	return s
}
func validID(s string) string {
	if len(s) == 0 || len(s) > 256 {
		return ""
	}
	for _, r := range s {
		if r < 33 || r > 126 {
			return ""
		}
	}
	return s
}
func (c *Collector) noteTurn(thread, turn string) {
	thread, turn = validID(thread), validID(turn)
	if thread == "" || turn == "" {
		return
	}
	if c.turns[turn] == nil {
		c.turns[turn] = map[string]bool{}
	}
	c.turns[turn][thread] = true
	if len(c.turns) > 4096 {
		c.turns = map[string]map[string]bool{turn: {thread: true}}
	}
}
func (c *Collector) FeedIPC(line []byte, fromClient bool) []Record {
	var m map[string]any
	if json.Unmarshal(line, &m) != nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	method := text(m["method"])
	p := object(m["params"])
	idBytes, _ := json.Marshal(m["id"])
	id := string(idBytes)
	if fromClient {
		if method == "turn/start" {
			c.requests[id] = validID(text(p["threadId"]))
			if len(c.requests) > 4096 {
				c.requests = map[string]string{id: c.requests[id]}
			}
		}
		return nil
	}
	if method == "turn/started" || method == "turn/completed" {
		thread, turn := text(p["threadId"]), text(object(p["turn"])["id"])
		c.noteTurn(thread, turn)
		if method == "turn/started" && validID(thread) != "" && validID(turn) != "" {
			return []Record{c.record(thread, turn, "", "", "", "", "turn.started", "pending", "native_turn_lifecycle", "rpc")}
		}
		status := text(object(p["turn"])["status"])
		if method == "turn/completed" && (status == "failed" || status == "interrupted") && validID(thread) != "" && validID(turn) != "" {
			last := c.latest[thread+"\x00"+turn]
			verdict := "request_failed"
			if last.Verdict == "mismatch" {
				verdict = "mismatch"
			}
			return []Record{c.record(thread, turn, last.Response, last.Requested, last.Reported, last.HeaderModel, "turn."+status, verdict, "native_turn_lifecycle", "rpc")}
		}
	}
	if thread := c.requests[id]; thread != "" {
		c.noteTurn(thread, text(object(object(m["result"])["turn"])["id"]))
		delete(c.requests, id)
	}
	if method == "model/rerouted" {
		thread, turn := validID(text(p["threadId"])), validID(text(p["turnId"]))
		from, to := validModel(text(p["fromModel"])), validModel(text(p["toModel"]))
		if thread != "" && turn != "" && from != "" && to != "" && !strings.EqualFold(from, to) {
			return []Record{c.record(thread, turn, "", from, to, "", "rerouted", "mismatch", "native_model_rerouted", "rpc")}
		}
	}
	return nil
}
func (c *Collector) record(thread, turn, response, requested, reported, header, stage, verdict, source, transport string) Record {
	c.counter++
	at := time.Now().UTC().Format(time.RFC3339Nano)
	hash := sha256.Sum256([]byte(at + thread + turn + response + stage))
	r := Record{ID: hex.EncodeToString(hash[:16]), At: at, Thread: thread, Turn: turn, Response: response, Requested: requested, Reported: reported, HeaderModel: header, Stage: stage, Verdict: verdict, Source: source, Scope: "original_desktop_request", Transport: transport, PID: c.PID}
	c.latest[thread+"\x00"+turn] = r
	if len(c.latest) > 4096 {
		c.latest = map[string]Record{thread + "\x00" + turn: r}
	}
	return r
}
func headerModel(headers map[string]any) string {
	for _, key := range []string{"openai-model", "x-openai-model"} {
		for k, v := range headers {
			if !strings.EqualFold(k, key) {
				continue
			}
			switch values := v.(type) {
			case string:
				if model := validModel(values); model != "" {
					return model
				}
			case []any:
				for _, value := range values {
					if model := validModel(text(value)); model != "" {
						return model
					}
				}
			case []string:
				for _, value := range values {
					if model := validModel(value); model != "" {
						return model
					}
				}
			}
		}
	}
	return ""
}

// consumed means the diagnostic line is private input to the collector and is
// withheld from the parent's logs, including unrecognized diagnostic frames.
func (c *Collector) FeedTrace(line []byte) (records []Record, consumed bool) {
	var m map[string]any
	if json.Unmarshal(line, &m) != nil {
		return nil, strings.Contains(string(line), "Received message") || strings.Contains(string(line), "SSE event:") || strings.Contains(string(line), "tungstenite::")
	}
	target, level := text(m["target"]), text(m["level"])
	isWS := strings.HasPrefix(target, "tungstenite::protocol")
	isSSE := strings.HasPrefix(target, "codex_api::sse")
	// Core TRACE spans contain only context, but do not need forwarding either.
	consumed = isWS || isSSE || level == "TRACE" || level == "DEBUG"
	if !isWS && !isSSE {
		return nil, consumed
	}
	message := text(object(m["fields"])["message"])
	prefix := "Received message "
	if isSSE {
		prefix = "SSE event: "
	}
	if !strings.HasPrefix(message, prefix) {
		return nil, consumed
	}
	var frame map[string]any
	if json.Unmarshal([]byte(strings.TrimPrefix(message, prefix)), &frame) != nil {
		return nil, true
	}
	stage := text(frame["type"])
	if stage != "response.created" && stage != "response.completed" && stage != "response.failed" && stage != "response.incomplete" && stage != "response.cancelled" {
		return nil, true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	turn, thread, requested, transport := "", "", "", ""
	c.frames++
	warmupKnown, warmup := false, false
	spans, _ := m["spans"].([]any)
	if span := m["span"]; span != nil {
		spans = append(spans, span)
	}
	for _, raw := range spans {
		span := object(raw)
		nested := object(span["fields"])
		for k, v := range nested {
			if span[k] == nil {
				span[k] = v
			}
		}
		if v := validID(text(span["turn_id"])); v != "" {
			turn = v
		}
		if v := validID(text(span["thread_id"])); v != "" {
			thread = v
		}
		if isSSE && (text(span["name"]) == "try_run_sampling_request" || text(span["name"]) == "run_sampling_request") && requested == "" {
			requested = validModel(text(span["model"]))
			transport = "sse"
			warmupKnown, warmup = true, false
		}
		switch text(span["name"]) {
		case "model_client.stream_responses_websocket":
			requested = validModel(text(span["model"]))
			transport = "websocket"
			if v, ok := span["websocket.warmup"].(bool); ok {
				warmupKnown, warmup = true, v
			}
		case "model_client.stream_responses_api":
			requested = validModel(text(span["model"]))
			transport = "sse"
			warmupKnown, warmup = true, false
		}
	}
	if !warmupKnown || warmup || turn == "" || requested == "" {
		c.unassociated++
		return nil, true
	}
	if thread == "" {
		known := c.turns[turn]
		if len(known) != 1 {
			c.unassociated++
			return nil, true
		}
		for id := range known {
			thread = id
		}
	} else if !c.turns[turn][thread] {
		return nil, true
	}
	response := object(frame["response"])
	responseID := validID(text(response["id"]))
	if responseID == "" {
		return nil, true
	}
	reported := validModel(text(response["model"]))
	header := headerModel(object(response["headers"]))
	if header == "" {
		header = headerModel(object(frame["headers"]))
	}
	if reported == "" {
		reported = header
	}
	verdict := "unknown"
	if reported != "" {
		if strings.EqualFold(reported, requested) {
			verdict = "match"
		} else {
			verdict = "mismatch"
		}
	}
	if verdict == "mismatch" && datedSuffix.MatchString(reported) && strings.EqualFold(datedSuffix.ReplaceAllString(reported, ""), requested) {
		verdict = "version_variant"
	}
	if header != "" && reported != "" && !strings.EqualFold(header, reported) {
		verdict = "conflicting_evidence"
	}
	if stage == "response.failed" || stage == "response.incomplete" || stage == "response.cancelled" {
		verdict = "request_failed"
	}
	return []Record{c.record(thread, turn, responseID, requested, reported, header, stage, verdict, "native_response_trace", transport)}, true
}
