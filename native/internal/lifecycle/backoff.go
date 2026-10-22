// Package lifecycle defines companion behavior for the whole Desktop app.
package lifecycle

import "time"

// Child supervisor backoff. Exported because the launcher's watch loop lives in
// package main and must not touch unexported state of this package.
//
// Next() produces: 1s, 2s, 4s, 8s, 16s, 32s, 60s, 60s, ...
const (
	MinBackoff    = 1 * time.Second
	MaxBackoff    = 60 * time.Second
	StablePeriod  = 60 * time.Second
	DegradedAfter = 5
)

// Backoff produces increasing retry delays. The zero value is ready to use.
type Backoff struct {
	attempts int
}

// Next returns the delay for the coming retry and advances internal state.
func (b *Backoff) Next() time.Duration {
	d := MinBackoff << uint(b.attempts)
	if d <= 0 || d > MaxBackoff {
		d = MaxBackoff
	}
	b.attempts++
	return d
}

// Reset clears the backoff after a child has proven stable.
func (b *Backoff) Reset() { b.attempts = 0 }

// Attempts reports how many consecutive retries have been scheduled.
func (b *Backoff) Attempts() int { return b.attempts }

// Degraded reports whether the supervisor has crossed DegradedAfter attempts.
// Informational only: Next() keeps returning delays.
func (b *Backoff) Degraded() bool { return b.attempts >= DegradedAfter }
