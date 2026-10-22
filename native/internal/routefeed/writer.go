package routefeed

import (
	"codex-observatory/internal/routetrace"
	"context"
	"sync"
	"sync/atomic"
	"time"
)

type WriterStats struct {
	Saved             uint64 `json:"saved"`
	WriteErrors       uint64 `json:"write_errors"`
	CriticalOverruns  uint64 `json:"critical_overruns"`
	ProgressCoalesced uint64 `json:"progress_coalesced"`
	Pending           int    `json:"pending"`
}
type Sink struct {
	Hub                                *Hub
	critical                           chan routetrace.Record
	mu                                 sync.Mutex
	progress                           map[string]routetrace.Record
	wake, stop, done                   chan struct{}
	closeOnce                          sync.Once
	saved, errors, overruns, coalesced atomic.Uint64
	save                               func(routetrace.Record) error
}

func NewSink(save func(routetrace.Record) error, capacity int) (*Sink, error) {
	hub, err := NewHub(2048)
	if err != nil {
		return nil, err
	}
	if capacity < 8 {
		capacity = 8
	}
	s := &Sink{Hub: hub, critical: make(chan routetrace.Record, capacity), progress: make(map[string]routetrace.Record),
		wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}), save: save}
	go s.run()
	return s, nil
}
func (s *Sink) Publish(rows []routetrace.Record) {
	s.Hub.Publish(rows)
	for _, r := range rows {
		if r.ID == "" || r.Scope != "original_desktop_request" || r.Thread == "" || r.Turn == "" {
			continue
		}
		if r.Stage == "response.output_text.delta" {
			key := r.Thread + "\x00" + r.Turn + "\x00" + r.Response
			s.mu.Lock()
			_, existed := s.progress[key]
			if existed || len(s.progress) < 256 {
				s.progress[key] = r
				if existed {
					s.coalesced.Add(1)
				}
			} else {
				s.coalesced.Add(1)
			}
			s.mu.Unlock()
		} else {
			select {
			case s.critical <- r:
			default:
				s.overruns.Add(1)
			}
		}
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Sink) next() (routetrace.Record, bool) {
	select {
	case r := <-s.critical:
		return r, true
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, r := range s.progress {
		delete(s.progress, key)
		return r, true
	}
	return routetrace.Record{}, false
}
func (s *Sink) run() {
	defer close(s.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		for {
			select {
			case r := <-s.critical:
				s.write(r)
			default:
				goto waiting
			}
		}
	waiting:
		select {
		case <-s.wake:
		case <-ticker.C:
			s.flushProgress()
		case <-s.stop:
			for {
				r, ok := s.next()
				if !ok {
					return
				}
				if s.save(r) != nil {
					s.errors.Add(1)
				} else {
					s.saved.Add(1)
				}
			}
		}
	}
}
func (s *Sink) write(r routetrace.Record) {
	if s.save(r) != nil {
		s.errors.Add(1)
	} else {
		s.saved.Add(1)
	}
}
func (s *Sink) flushProgress() {
	s.mu.Lock()
	batch := s.progress
	s.progress = make(map[string]routetrace.Record)
	s.mu.Unlock()
	for _, r := range batch {
		s.write(r)
	}
}
func (s *Sink) Stats() WriterStats {
	s.mu.Lock()
	pending := len(s.progress) + len(s.critical)
	s.mu.Unlock()
	return WriterStats{Saved: s.saved.Load(), WriteErrors: s.errors.Load(), CriticalOverruns: s.overruns.Load(), ProgressCoalesced: s.coalesced.Load(), Pending: pending}
}
func (s *Sink) Close(ctx context.Context) error {
	s.closeOnce.Do(func() { close(s.stop) })
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
