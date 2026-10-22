package floatui

import "math"

// Appearance animates the shell, typography and path visibility independently.
// Head/Tail are coordinates on the ORIGINAL animated curves, not screen wipes.
// Value semantics make a frame safe to inspect without retaining animator state.
type Appearance struct {
	Shell, Text, Details                     float64
	Head, Tail                               [3]float64
	Energy                                   float64
	Entering, Exiting, Hidden, Transitioning bool
}

func (s Appearance) mask(u float64, band int) float64 {
	track := band % 3
	if band%2 != 0 {
		u = 1 - u
	}
	// Finite soft head and tail: nothing is visible beyond this path segment.
	return smoother((s.Head[track]-u)/.12) * smoother((u-s.Tail[track])/.12)
}

type appearanceController struct {
	initialized          bool
	shell, text, details Tween
	head, tail           [3]Tween
	mode                 uint8 // 0=shown, 1=entering, 2=leaving, 3=hidden
	since, until         float64
}

func (c *appearanceController) init() {
	if c.initialized {
		return
	}
	c.initialized = true
	c.shell, c.text, c.details = settled(1), settled(1), settled(1)
	for i := range c.head {
		c.head[i] = settled(1.16)
		c.tail[i] = settled(-.16)
	}
}
func (c *appearanceController) frame(now float64) Appearance {
	c.init()
	if now >= c.until {
		if c.mode == 1 {
			c.mode = 0
		}
		if c.mode == 2 {
			c.mode = 3
		}
	}
	s := Appearance{Shell: clamp01(c.shell.Value(now)), Text: clamp01(c.text.Value(now)), Details: clamp01(c.details.Value(now)), Entering: c.mode == 1, Exiting: c.mode == 2, Hidden: c.mode == 3}
	for i := range s.Head {
		s.Head[i] = c.head[i].Value(now)
		s.Tail[i] = c.tail[i].Value(now)
	}
	s.Transitioning = s.Entering || s.Exiting
	if s.Transitioning {
		s.Energy = math.Sin(math.Pi * clamp01((now-c.since)/math.Max(.01, c.until-c.since)))
	}
	if s.Hidden {
		s.Shell, s.Text, s.Details = 0, 0, 0
	}
	return s
}

// BeginReveal starts at transparent pixels on first open or after a completed
// exit. Calling Reveal mid-exit preserves all currently displayed values.
func (a *Animator) BeginReveal(now float64) {
	c := &a.appearance
	c.init()
	c.frame(now)
	if c.mode == 1 {
		return
	}
	fresh := c.mode == 0 || c.mode == 3
	if fresh {
		c.shell, c.text, c.details = settled(0), settled(0), settled(0)
		for i := range c.head {
			c.head[i] = settled(-.16)
			c.tail[i] = settled(-.16)
		}
	}
	c.mode, c.since, c.until = 1, now, now+1.72
	c.shell.RetargetAfter(1, now, .18, .98)
	c.text.RetargetAfter(1, now, .48, .65)
	c.details.RetargetAfter(1, now, .73, .63)
	for i := range c.head {
		c.head[i].RetargetAfter(1.16, now, .07*float64(i), 1.36)
		c.tail[i].Retarget(-.16, now, .78)
	}
}
func (a *Animator) BeginExit(now float64) {
	c := &a.appearance
	c.init()
	c.frame(now)
	if c.mode == 2 || c.mode == 3 {
		return
	}
	c.mode, c.since, c.until = 2, now, now+1.52
	c.text.Retarget(0, now, .49)
	c.details.RetargetAfter(0, now, .10, .52)
	c.shell.RetargetAfter(0, now, .28, .91)
	for i := range c.tail {
		c.head[i].Retarget(1.16, now, .62)
		c.tail[i].RetargetAfter(1.16, now, .08*float64(i), 1.20)
	}
}

// ShowImmediate is used for reduced-motion re-opening. A hidden stage must not
// stay hidden just because its decorative transition has been disabled.
func (a *Animator) ShowImmediate() {
	a.appearance = appearanceController{}
	a.appearance.init()
}
func (a *Animator) HideImmediate() {
	a.appearance.init()
	a.appearance.mode = 3
}
