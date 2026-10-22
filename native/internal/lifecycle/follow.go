// Package lifecycle defines companion behavior for the whole Desktop app.
package lifecycle

import "time"

type Action string

const (
	None  Action = ""
	Start Action = "start"
	Stop  Action = "stop"
)

type Follower struct {
	Active       bool
	MissingSince time.Time
	Grace        time.Duration
}

func (f *Follower) Update(present, known bool, at time.Time) Action {
	if !known {
		f.MissingSince = time.Time{}
		return None
	}
	if present {
		f.MissingSince = time.Time{}
		if !f.Active {
			f.Active = true
			return Start
		}
		return None
	}
	if !f.Active {
		return None
	}
	if f.MissingSince.IsZero() {
		f.MissingSince = at
		return None
	}
	if at.Sub(f.MissingSince) >= f.Grace {
		f.Active = false
		f.MissingSince = time.Time{}
		return Stop
	}
	return None
}
