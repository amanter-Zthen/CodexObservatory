package floatui

import (
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Route string

const (
	Pending  Route = "pending"
	Match    Route = "match"
	Mismatch Route = "mismatch"
	Variant  Route = "version_variant"
	Conflict Route = "conflicting_evidence"
)

type Activity string

const (
	Running   Activity = "running"
	ToolWait  Activity = "tool_wait"
	Completed Activity = "completed"
	Failed    Activity = "failed"
	Cancelled Activity = "cancelled"
)

type response struct {
	id                             string
	at                             time.Time
	started                        bool
	createdAt                      time.Time
	outputTokens                   int64
	elapsed                        float64
	requested, reported, speedText string
	route                          Route
	tokenRate, characterRate       float64
	speedValid, finished           bool
	lastOutput                     time.Time
	estimatedRate                  float64
	intervalRate                   float64
	rateAt                         time.Time
}
type session struct {
	thread, turn, requested            string
	started, changed, ended, requestAt time.Time
	activity                           Activity
	pid                                int
	current                            string
	responses                          map[string]*response
	anomaly                            bool
	mismatchModel                      string
	turnTracked, responseEnded         bool
}

type Diagnostics struct {
	Duplicates        uint64 `json:"duplicates"`
	Ignored           uint64 `json:"ignored"`
	CapacityEvictions uint64 `json:"capacity_evictions"`
}

type Engine struct {
	mu          sync.RWMutex
	opts        Options
	threads     map[string]*session
	order       []string
	seen        map[string]bool
	ring        []string
	ringAt      int
	health      Health
	pids        map[int]bool
	quotas      map[string]QuotaEvent
	quotaKey    string
	diagnostics Diagnostics
	revision    uint64
}

func New(opts Options) *Engine {
	defaults := DefaultOptions()
	if opts.MaxThreads <= 0 {
		opts.MaxThreads = defaults.MaxThreads
	}
	if opts.MaxResponses <= 0 {
		opts.MaxResponses = defaults.MaxResponses
	}
	if opts.DedupCapacity <= 0 {
		opts.DedupCapacity = defaults.DedupCapacity
	}
	if opts.HealthTTL <= 0 {
		opts.HealthTTL = defaults.HealthTTL
	}
	if opts.QuotaTTL <= 0 {
		opts.QuotaTTL = defaults.QuotaTTL
	}
	if opts.ActivityTTL <= 0 {
		opts.ActivityTTL = defaults.ActivityTTL
	}
	if opts.StateTTL <= 0 {
		opts.StateTTL = defaults.StateTTL
	}
	return &Engine{opts: opts, threads: make(map[string]*session), seen: make(map[string]bool),
		ring: make([]string, opts.DedupCapacity), pids: make(map[int]bool), quotas: make(map[string]QuotaEvent)}
}

func (e *Engine) SetHealth(h Health) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if h.At.Before(e.health.At) {
		return
	}
	e.health = Health{At: h.At, Ready: h.Ready}
	e.revision++
	clear(e.pids)
	for _, pid := range h.PIDs {
		if pid > 0 {
			e.pids[pid] = true
		}
	}
}

func (e *Engine) ObserveQuota(q QuotaEvent) {
	if q.Bucket == "" || q.Source == "" || q.At.IsZero() || q.WindowMinutes != 10080 ||
		math.IsNaN(q.Used) || math.IsInf(q.Used, 0) || q.Used < 0 || q.Used > 100 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	key := q.Source + "\x00" + q.Bucket
	if old, ok := e.quotas[key]; ok && !q.At.After(old.At) {
		return
	}
	if len(e.quotas) >= 64 {
		if _, ok := e.quotas[key]; !ok {
			e.diagnostics.Ignored++
			return
		}
	}
	e.quotas[key] = q
	e.revision++
	if e.quotaKey == "" || q.Bucket == e.opts.PreferredQuota {
		e.quotaKey = key
	}
}

func (e *Engine) Apply(v Event, now time.Time) {
	e.apply(v, now, false)
}

// Replay re-evaluates an existing immutable feed record during a user-requested
// resynchronization. Turn ordering, response identity and anomalies remain
// intact; a new request still needs its own response evidence.
func (e *Engine) Replay(v Event, now time.Time) {
	e.apply(v, now, true)
}

func (e *Engine) apply(v Event, now time.Time, replay bool) {
	if v.Thread == "" || v.Turn == "" || v.At.IsZero() || v.At.After(now.Add(time.Minute)) {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.revision++
	if v.ID != "" {
		if e.seen[v.ID] {
			if !replay {
				e.diagnostics.Duplicates++
				return
			}
		} else {
			delete(e.seen, e.ring[e.ringAt])
			e.ring[e.ringAt] = v.ID
			e.ringAt = (e.ringAt + 1) % len(e.ring)
			e.seen[v.ID] = true
		}
	}
	s := e.threads[v.Thread]
	// An auxiliary turn cannot replace a known, still-running human/subagent
	// turn on the same thread. The adapter still records its routing evidence.
	if s != nil && s.turn != v.Turn && v.ResponseScoped && s.turnTracked && s.ended.IsZero() {
		e.diagnostics.Ignored++
		return
	}
	if s == nil || s.turn != v.Turn {
		// Only explicit lifecycle/request starts can replace a known current turn.
		if s != nil && (v.Kind != TurnStarted && v.Kind != RequestSent || v.At.Before(s.started)) {
			e.diagnostics.Ignored++
			return
		}
		if now.Sub(v.At) > e.opts.ActivityTTL {
			e.diagnostics.Ignored++
			return
		}
		if s == nil {
			if len(e.threads) >= e.opts.MaxThreads {
				e.evict()
			}
			e.order = append(e.order, v.Thread)
		}
		s = &session{thread: v.Thread, turn: v.Turn, started: v.At, changed: v.At,
			activity: Running, responses: make(map[string]*response)}
		e.threads[v.Thread] = s
	}
	if !v.ResponseScoped {
		s.turnTracked = true
		// Local task metadata can arrive after an early response-only finish.
		// Restore task ownership only for that provisional finish; genuine
		// turn-completed events remain terminal even if older starts arrive.
		if s.responseEnded && v.Kind != TurnFinished {
			s.ended, s.responseEnded, s.activity = time.Time{}, false, Running
		}
	}
	if !s.turnTracked && s.responseEnded && v.At.After(s.ended) &&
		(v.Kind == RequestSent || v.Kind == ResponseCreated && v.Response != s.current) {
		s.ended, s.responseEnded, s.activity = time.Time{}, false, Running
	}
	if v.At.After(s.changed) {
		s.changed = v.At
	}
	switch v.Kind {
	case TurnStarted:
		if v.At.Before(s.started) {
			s.started = v.At
		}
		if v.Requested != "" && s.requested == "" {
			s.requested = v.Requested
		}
	case TurnFinished:
		s.responseEnded = false
		if s.ended.IsZero() || v.At.After(s.ended) {
			s.ended = v.At
			s.activity = Completed
			if v.Cancelled {
				s.activity = Cancelled
			}
			if v.Verdict == "request_failed" {
				s.activity = Failed
			}
		}
	case ToolStarted:
		if s.ended.IsZero() && !v.At.Before(s.requestAt) {
			s.activity = ToolWait
		}
	case RequestSent:
		if !v.At.After(s.requestAt) {
			return
		}
		if current := s.responses[s.current]; current == nil || current.at.Before(v.At) {
			s.current = ""
		}
		s.requestAt, s.requested, s.pid = v.At, v.Requested, v.PID
		if s.ended.IsZero() {
			s.activity = Running
		}
	case ResponseCreated, ResponseMetadata, OutputProgress, ResponseFinished, ResponseFailed:
		if v.Response == "" || !v.Trusted {
			return
		}
		r := s.responses[v.Response]
		if r == nil {
			if len(s.responses) >= e.opts.MaxResponses {
				oldID := ""
				var oldest time.Time
				for id, previous := range s.responses {
					if id != s.current && previous.finished && (oldID == "" || previous.at.Before(oldest)) {
						oldID, oldest = id, previous.at
					}
				}
				if oldID == "" {
					e.diagnostics.Ignored++
					return
				}
				delete(s.responses, oldID)
			}
			r = &response{id: v.Response, route: Pending}
			s.responses[v.Response] = r
		}
		// A created frame arriving after completed can establish interval coverage,
		// but never replaces the newer model verdict or revives its output phase.
		if v.Kind == ResponseCreated {
			r.started, r.createdAt = true, v.At
			updateSpeed(r)
		}
		if !r.at.IsZero() && v.At.Before(r.at) {
			return
		}
		r.at = v.At
		if v.Requested != "" {
			r.requested = v.Requested
		}
		if v.Reported != "" {
			r.reported = v.Reported
			r.route = compare(r.requested, r.reported, v.Verdict)
			if r.route == Mismatch {
				s.anomaly = true
				s.mismatchModel = r.reported
			}
		}
		if finitePositive(v.CharacterRate) {
			r.characterRate = v.CharacterRate
			if v.Kind == OutputProgress {
				r.lastOutput = v.At
			}
		}
		if finitePositive(v.EstimatedTokenRate) {
			r.estimatedRate = v.EstimatedTokenRate
		}
		if v.Kind == OutputProgress && (finitePositive(v.CharacterRate) || finitePositive(v.EstimatedTokenRate)) {
			r.lastOutput = v.At
		}
		if v.Kind == ResponseFinished || v.Kind == ResponseFailed {
			r.finished = true
			if v.Kind == ResponseFinished {
				r.outputTokens, r.elapsed = v.OutputTokens, v.ElapsedSeconds
				r.intervalRate = v.TokenRate
				r.rateAt = v.At
				updateSpeed(r)
			} else {
				r.speedValid = false
			}
		}
		if !v.At.Before(s.requestAt) {
			current := s.responses[s.current]
			if current == nil || !v.At.Before(current.at) {
				s.current = v.Response
				if s.requested == "" {
					s.requested = v.Requested
				}
				s.pid = v.PID
			}
		}
		if !s.turnTracked && s.current == v.Response && !v.At.Before(s.requestAt) &&
			(v.Kind == ResponseFinished || v.Kind == ResponseFailed) {
			s.ended, s.responseEnded, s.activity = v.At, true, Completed
			if v.Kind == ResponseFailed {
				s.activity = Failed
			}
		}
	}
}

func updateSpeed(r *response) {
	r.speedValid = false
	// A completed source record can carry its measured interval even when a
	// feed reconnect/replay omitted the response-created frame.
	covered := r.started && !r.createdAt.After(r.at)
	if !covered && r.outputTokens > 0 && finitePositive(r.elapsed) && finitePositive(r.intervalRate) {
		rate := float64(r.outputTokens) / r.elapsed
		covered = math.Abs(rate-r.intervalRate) <= math.Max(.01, rate*.01)
	}
	if r.finished && covered && r.outputTokens > 0 && finitePositive(r.elapsed) {
		rate := float64(r.outputTokens) / r.elapsed
		if finitePositive(rate) {
			r.tokenRate, r.speedValid = rate, true
			r.speedText = strconv.FormatFloat(rate, 'f', 1, 64) + "/s"
		}
	}
}

func finitePositive(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }
func compare(requested, reported, verdict string) Route {
	if requested == "" || reported == "" {
		return Pending
	}
	if verdict == "conflicting_evidence" {
		return Conflict
	}
	if strings.EqualFold(requested, reported) {
		return Match
	}
	if verdict == "version_variant" {
		return Variant
	}
	if len(reported) > 11 && strings.EqualFold(reported[:len(reported)-11], requested) {
		if _, err := time.Parse("2006-01-02", reported[len(reported)-10:]); err == nil {
			return Variant
		}
	}
	return Mismatch
}

func (e *Engine) evict() {
	index := 0
	for i, id := range e.order {
		s := e.threads[id]
		old := e.threads[e.order[index]]
		if (!s.ended.IsZero() && old.ended.IsZero()) || (s.ended.IsZero() == old.ended.IsZero() && s.changed.Before(old.changed)) {
			index = i
		}
	}
	delete(e.threads, e.order[index])
	e.order = append(e.order[:index], e.order[index+1:]...)
	e.diagnostics.CapacityEvictions++
}

func (e *Engine) Prune(now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, id := range e.order {
		if now.Sub(e.threads[id].changed) > e.opts.StateTTL || e.retiredClient(e.threads[id], now) {
			delete(e.threads, id)
			e.revision++
		} else {
			e.order[n] = id
			n++
		}
	}
	e.order = e.order[:n]
}

func (e *Engine) retiredClient(s *session, now time.Time) bool {
	return e.health.Ready && !e.health.At.IsZero() && now.Sub(e.health.At) <= e.opts.HealthTTL &&
		!e.health.At.After(now.Add(time.Minute)) && len(e.pids) > 0 && s.pid > 0 && !e.pids[s.pid] &&
		now.Sub(s.changed) >= e.opts.NormalHold
}

func (e *Engine) HasTurn(thread, turn string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	s := e.threads[thread]
	return s != nil && s.turn == turn
}
