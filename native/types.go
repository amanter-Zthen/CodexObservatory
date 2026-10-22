package main

import (
	"codex-observatory/internal/completion"
	"codex-observatory/internal/floatui"
	"codex-observatory/internal/routefeed"
	"codex-observatory/internal/routetrace"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const Version = "0.4.8-nacre"
const ParserVersion = "obs-rollout-1"
const maxLineBytes = 32 * 1024 * 1024
const recentLimit = 120

type Obj = map[string]any

type Usage struct {
	Input      int64 `json:"input_tokens"`
	Cached     int64 `json:"cached_input_tokens"`
	CacheWrite int64 `json:"cache_write_input_tokens"`
	Output     int64 `json:"output_tokens"`
	Reasoning  int64 `json:"reasoning_output_tokens"`
	Total      int64 `json:"total_tokens"`
}
type Window struct {
	Used    float64 `json:"used_percent"`
	Minutes *int64  `json:"window_minutes,omitempty"`
	Reset   *int64  `json:"resets_at,omitempty"`
}
type Quota struct {
	ID        string  `json:"id"`
	Name      string  `json:"name,omitempty"`
	Plan      string  `json:"plan,omitempty"`
	Primary   *Window `json:"primary,omitempty"`
	Secondary *Window `json:"secondary,omitempty"`
	At        string  `json:"at"`
	Source    string  `json:"source"`
}
type Event struct {
	ID         string  `json:"id"`
	At         string  `json:"at"`
	ObservedAt string  `json:"observed_at"`
	Kind       string  `json:"kind"`
	Source     string  `json:"source"`
	Field      string  `json:"field"`
	Offset     int64   `json:"offset"`
	Length     int64   `json:"length"`
	LineSHA256 string  `json:"line_sha256"`
	Thread     string  `json:"thread_id,omitempty"`
	Turn       string  `json:"turn_id,omitempty"`
	Response   string  `json:"response_id,omitempty"`
	Model      string  `json:"model,omitempty"`
	Effort     string  `json:"effort,omitempty"`
	Detail     Obj     `json:"detail,omitempty"`
	Usage      *Usage  `json:"usage,omitempty"`
	Total      *Usage  `json:"total_usage,omitempty"`
	Quotas     []Quota `json:"quotas,omitempty"`
}
type Summary struct {
	Key             string           `json:"key"`
	Thread          string           `json:"thread_id,omitempty"`
	RootThread      string           `json:"root_thread_id,omitempty"`
	Parent          string           `json:"parent_thread_id,omitempty"`
	Forked          string           `json:"forked_from_id,omitempty"`
	Title           string           `json:"title,omitempty"`
	Home            string           `json:"home"`
	File            string           `json:"file"`
	CWD             string           `json:"cwd,omitempty"`
	Source          string           `json:"source,omitempty"`
	Originator      string           `json:"originator,omitempty"`
	ClientVersion   string           `json:"client_version,omitempty"`
	Model           string           `json:"model,omitempty"`
	Effort          string           `json:"effort,omitempty"`
	Tier            string           `json:"tier,omitempty"`
	TierSource      string           `json:"service_tier_source,omitempty"`
	TierAt          string           `json:"service_tier_at,omitempty"`
	ThreadTier      string           `json:"applied_service_tier,omitempty"`
	ThreadTierAt    string           `json:"applied_service_tier_at,omitempty"`
	ThreadTierOwner string           `json:"applied_service_tier_thread_id,omitempty"`
	ModelField      string           `json:"model_field,omitempty"`
	SettingsAt      string           `json:"settings_at,omitempty"`
	CurrentTurn     string           `json:"current_turn_id,omitempty"`
	StartedAt       string           `json:"started_at,omitempty"`
	At              string           `json:"at,omitempty"`
	UsageAt         string           `json:"usage_at,omitempty"`
	Latest          *Usage           `json:"latest,omitempty"`
	Total           *Usage           `json:"total,omitempty"`
	ContextWindow   int64            `json:"context_window,omitempty"`
	ContextValid    bool             `json:"context_valid"`
	Quotas          map[string]Quota `json:"quotas"`
	LastReroute     *Event           `json:"last_reroute,omitempty"`
	LastError       *Event           `json:"last_error,omitempty"`
	Turns           int64            `json:"turns"`
	ToolCalls       int64            `json:"tool_calls"`
	Compactions     int64            `json:"compactions"`
	ParseErrors     int64            `json:"parse_errors"`
	OversizeLines   int64            `json:"oversize_lines"`
	UnknownEvents   int64            `json:"unknown_events"`
	HasHistoryBase  bool             `json:"has_history_base"`
	Archived        bool             `json:"archived"`
	Offset          int64            `json:"offset"`
	Bytes           int64            `json:"bytes"`
	FileModified    string           `json:"file_modified"`
	CaughtUp        bool             `json:"caught_up"`
	LastScan        string           `json:"last_scan"`
	ReadError       string           `json:"read_error,omitempty"`
}
type FileState struct {
	Schema          string          `json:"schema"`
	TierSchema      int             `json:"service_tier_schema,omitempty"`
	Summary         Summary         `json:"summary"`
	Offset          int64           `json:"offset"`
	Generation      int64           `json:"generation"`
	Anchor          string          `json:"anchor,omitempty"`
	LastMod         int64           `json:"last_mod"`
	LastSize        int64           `json:"last_size"`
	WaitingPartial  bool            `json:"waiting_partial"`
	UsageSignature  string          `json:"usage_signature,omitempty"`
	QuotaSignature  string          `json:"quota_signature,omitempty"`
	LastStartedTurn string          `json:"last_started_turn,omitempty"`
	Recent          []Event         `json:"recent"`
	SeenToolIDs     map[string]bool `json:"seen_tool_ids,omitempty"`
}
type Mark struct {
	ID      string  `json:"id"`
	At      string  `json:"at"`
	Kind    string  `json:"kind"`
	Note    string  `json:"note"`
	Session Summary `json:"session"`
	Nearby  []Event `json:"nearby"`
}
type Config struct {
	Roots       []string          `json:"roots"`
	PollSeconds int               `json:"poll_seconds"`
	CodexBinary string            `json:"codex_binary,omitempty"`
	Pinned      string            `json:"pinned,omitempty"`
	Aliases     map[string]string `json:"aliases"`
}
type Candidate struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}
type App struct {
	routeSyncOnce      sync.Once
	routeSyncWake      chan struct{}
	routeSyncBusy      atomic.Bool
	routeSyncState     atomic.Pointer[RouteSyncStatus]
	routeAuditOnce     sync.Once
	routeAudit         atomic.Pointer[routefeed.Sink]
	routeAuditError    atomic.Value
	routeFileCount     atomic.Uint64
	floatFrameObserved func(floatui.Scene)
	floatMenuObserved  func(bool)
	inspectWindow      bool
	completionOnce     sync.Once
	completed          *completion.Inbox
	previewViewed      atomic.Uint64
	preview            bool
	visualQuality      string
	floatLifeMu        sync.Mutex
	floatLife          map[string]Event
	floatOnce          sync.Once
	floatEngine        *floatui.Engine
	floatSnapshot      atomic.Pointer[floatui.Snapshot]
	mu                 sync.RWMutex
	journalMu          sync.RWMutex
	retention          RetentionStatus
	liveRoutes         map[string]routetrace.Record
	routeResults       map[string]routetrace.Record
	routeAnomalies     map[string]routetrace.Record
	data               string
	cfg                Config
	files              map[string]*FileState
	marks              []Mark
	probes             []*Probe
	probeRunning       bool
	probeCancel        func()
	ctxDone            <-chan struct{}
	stop               func()
	scanKick           chan struct{}
	lastScan           string
	scanError          string
	scanBusy           bool
	discovered         int
	scanBytes          int64
	scanDuration       int64
	started            time.Time
	token              string
	address            string
	demo               bool
}

func now() string               { return time.Now().UTC().Format(time.RFC3339Nano) }
func hashBytes(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func shortHash(s string) string { return hashBytes([]byte(s))[:24] }
func randomID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func obj(x any) Obj {
	if v, ok := x.(map[string]any); ok && v != nil {
		return v
	}
	return Obj{}
}
func str(x any) string {
	if v, ok := x.(string); ok {
		return limitText(v, 512)
	}
	return ""
}
func ident(x any) string { return limitText(str(x), 256) }
func first(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
func num(x any) int64 {
	switch n := x.(type) {
	case json.Number:
		i, _ := n.Int64()
		return i
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}
func decimal(x any) float64 {
	switch n := x.(type) {
	case json.Number:
		v, _ := n.Float64()
		return v
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	}
	return 0
}
func limitText(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
func jsonBytes(v any) []byte { b, _ := json.Marshal(v); return b }
func decodeObj(b []byte) (Obj, error) {
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.UseNumber()
	var x Obj
	e := d.Decode(&x)
	return x, e
}
func usage(x any) *Usage {
	m, ok := x.(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	return &Usage{Input: num(m["input_tokens"]), Cached: num(m["cached_input_tokens"]), CacheWrite: num(m["cache_write_input_tokens"]), Output: num(m["output_tokens"]), Reasoning: num(m["reasoning_output_tokens"]), Total: num(m["total_tokens"])}
}
func saveJSON(path string, v any) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	t := path + ".tmp"
	f, e := os.OpenFile(t, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return replaceFile(t, path)
}
func loadJSON(path string, v any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func newState(home, path, key string) *FileState {
	return &FileState{Schema: ParserVersion, Summary: Summary{Key: key, Home: home, File: path, Quotas: map[string]Quota{}}, SeenToolIDs: map[string]bool{}, Recent: []Event{}}
}
func eventFor(f *FileState, r Obj, b []byte, offset int64, kind, field string) Event {
	at := str(r["timestamp"])
	if at == "" {
		at = f.Summary.FileModified
	}
	if at == "" {
		at = now()
	}
	sha := hashBytes(b)
	id := shortHash(fmt.Sprintf("%s:%d:%d:%s:%s", f.Summary.Key, f.Generation, offset, sha, kind))
	return Event{ID: id, At: at, ObservedAt: now(), Kind: kind, Source: "LOCAL_ROLLOUT", Field: field, Offset: offset, Length: int64(len(b)), LineSHA256: sha, Thread: f.Summary.Thread, Turn: f.Summary.CurrentTurn, Model: f.Summary.Model, Effort: f.Summary.Effort}
}
func appendRecent(f *FileState, e Event) {
	f.Recent = append(f.Recent, e)
	if len(f.Recent) > recentLimit {
		f.Recent = append([]Event(nil), f.Recent[len(f.Recent)-recentLimit:]...)
	}
}
