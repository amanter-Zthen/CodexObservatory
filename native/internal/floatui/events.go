// Package floatui maintains display state independently of files and native drawing.
package floatui

import "time"

type Kind uint8

const (
	TurnStarted Kind = iota + 1
	TurnFinished
	ToolStarted
	RequestSent
	ResponseCreated
	ResponseMetadata
	OutputProgress
	ResponseFinished
	ResponseFailed
)

// At is source time. Ingest time is supplied separately, so replay cannot look live.
type Event struct {
	ID, Thread, Turn, Response string
	At                         time.Time
	Kind                       Kind
	PID                        int
	Requested, Reported        string
	Verdict                    string
	Trusted                    bool
	OutputTokens               int64
	ElapsedSeconds, TokenRate  float64
	CharacterRate              float64
	EstimatedTokenRate         float64
	Cancelled                  bool
	// ResponseScoped requests have no associated task lifecycle. Their own
	// terminal response ends the temporary display, without finishing a chat.
	ResponseScoped bool
}

type Health struct {
	At    time.Time
	Ready bool
	PIDs  []int
}

type QuotaEvent struct {
	Bucket, Source string
	At             time.Time
	Used           float64
	WindowMinutes  int64
}

type Options struct {
	NormalHold, ErrorHold, HealthTTL, QuotaTTL time.Duration
	ActivityTTL, StateTTL                      time.Duration
	MaxThreads, MaxResponses, DedupCapacity    int
	PreferredQuota                             string
}

func DefaultOptions() Options {
	return Options{NormalHold: 8 * time.Second, ErrorHold: 30 * time.Second,
		HealthTTL: 10 * time.Second, QuotaTTL: 15 * time.Minute,
		ActivityTTL: 2 * time.Hour, StateTTL: 24 * time.Hour,
		MaxThreads: 256, MaxResponses: 32, DedupCapacity: 4096}
}
