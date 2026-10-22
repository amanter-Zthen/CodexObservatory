package floatui

import "time"

type Color string

const (
	Green  Color = "green"
	Red    Color = "red"
	Yellow Color = "yellow"
)

type Row struct {
	Thread             string    `json:"thread_id"`
	Turn               string    `json:"turn_id"`
	Response           string    `json:"response_id,omitempty"`
	Model              string    `json:"model"`
	Requested          string    `json:"requested_model"`
	Reported           string    `json:"reported_model,omitempty"`
	Route              Route     `json:"route"`
	Color              Color     `json:"color"`
	Activity           Activity  `json:"activity"`
	Speed              string    `json:"speed"`
	TokenRate          float64   `json:"tokens_per_second,omitempty"`
	SpeedValid         bool      `json:"speed_valid"`
	SpeedFresh         bool      `json:"speed_fresh"`
	OutputActivity     float64   `json:"output_activity,omitempty"`
	Anomaly            bool      `json:"turn_anomaly"`
	HealthValid        bool      `json:"health_valid"`
	Started            time.Time `json:"started_at"`
	Ended              time.Time `json:"ended_at,omitempty"`
	Outputting         bool      `json:"outputting"`
	EstimatedTokenRate float64   `json:"estimated_visible_tokens_per_second,omitempty"`
	ActivityScope      string    `json:"activity_scope"`
}

type QuotaView struct {
	Known     bool      `json:"known"`
	Fresh     bool      `json:"fresh"`
	Remaining float64   `json:"remaining_percent"`
	Bucket    string    `json:"bucket,omitempty"`
	At        time.Time `json:"at,omitempty"`
}
type Snapshot struct {
	PendingCompleted int         `json:"pending_completed"`
	Mode             string      `json:"mode"`
	Revision         uint64      `json:"revision"`
	At               time.Time   `json:"at"`
	Rows             []Row       `json:"rows"`
	Quota            QuotaView   `json:"quota"`
	Ready            bool        `json:"ready"`
	Diagnostics      Diagnostics `json:"diagnostics"`
}

// AppendSnapshot uses caller storage. Native frames can reuse a published snapshot;
// this method is also allocation-free with a sufficiently sized rows buffer.
func (e *Engine) AppendSnapshot(dst []Row, now time.Time) Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	ready := e.health.Ready && !e.health.At.IsZero() && now.Sub(e.health.At) <= e.opts.HealthTTL && !e.health.At.After(now.Add(time.Minute))
	for _, id := range e.order {
		s := e.threads[id]
		if now.Sub(s.changed) > e.opts.ActivityTTL || e.retiredClient(s, now) {
			continue
		}
		if !s.ended.IsZero() {
			hold := e.opts.NormalHold
			if s.anomaly {
				hold = e.opts.ErrorHold
			}
			if now.Sub(s.ended) >= hold {
				continue
			}
		}
		r := Row{Thread: s.thread, Turn: s.turn, Requested: s.requested, Model: s.requested,
			Route: Pending, Color: Yellow, Activity: s.activity, Speed: "—/s", Anomaly: s.anomaly, Started: s.started, Ended: s.ended,
			HealthValid: ready && s.pid > 0 && e.pids[s.pid]}
		r.ActivityScope = "response"
		if s.turnTracked {
			r.ActivityScope = "turn"
		}
		if response := s.responses[s.current]; response != nil {
			r.Response, r.Reported, r.Route = response.id, response.reported, response.route
			if response.reported != "" {
				r.Model = response.reported
			}
			if response.speedValid {
				r.Speed, r.TokenRate, r.SpeedValid = response.speedText, response.tokenRate, true
				age := now.Sub(response.rateAt)
				r.SpeedFresh = response.finished && age >= 0 && age < 1100*time.Millisecond
			}
			if s.ended.IsZero() && !response.finished {
				r.OutputActivity = response.characterRate
				r.Outputting = !response.lastOutput.IsZero() && now.Sub(response.lastOutput) < 1500*time.Millisecond
				if r.Outputting {
					r.EstimatedTokenRate = response.estimatedRate
				}
			}
		}
		if r.HealthValid {
			if s.anomaly {
				r.Color = Red
			} else if r.Route == Match {
				r.Color = Green
			}
		}
		dst = append(dst, r)
	}
	q := QuotaView{}
	if quota, ok := e.quotas[e.quotaKey]; ok && !quota.At.After(now.Add(time.Minute)) {
		q = QuotaView{Known: true, Fresh: now.Sub(quota.At) <= e.opts.QuotaTTL, Remaining: 100 - quota.Used, Bucket: quota.Bucket, At: quota.At}
	}
	return Snapshot{Mode: "observed", Revision: e.revision, At: now, Rows: dst, Quota: q, Ready: ready, Diagnostics: e.diagnostics}
}

func (e *Engine) Snapshot(now time.Time) Snapshot { return e.AppendSnapshot([]Row{}, now) }
