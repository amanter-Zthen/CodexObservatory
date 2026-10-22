package floatui

import "math"

// Paths, optics and labels are shared by the desktop renderer and offline proof.
// Glyph rasterisation alone is platform-specific. All buffers have fixed caps.
type PaintShape struct {
	Points     [66]Point
	Count      int
	Color      uint32
	Ellipse    bool
	X, Y, W, H float64
}
type PaintLabel struct {
	Text                      string
	X, Y, W, H, Size, Opacity float64
	Color                     uint32
	Bold, Right, WaterClip    bool
	// Sheen moves through the glyph mask, never across a rectangle behind it.
	Sheen, Glow float64
	Light       uint32
}
type LightRing struct {
	X, Y, Radius, Aspect, Angle, Phase, Opacity float64
}

type Sprite struct {
	Emission float64 // bounded radiance, independent from body opacity

	X, Y, Radius, Aspect, Angle, Opacity float64
	Color                                uint32
	Kind                                 uint8 // 0=soft light, 1=pearl, 2=faceted shard, 3=streak, 4=star
}
type Painting struct {
	Rings []LightRing

	Shapes  []PaintShape
	Labels  []PaintLabel
	Bands   []Band
	Sprites []Sprite
}
type Painter struct {
	result                        Painting
	limit                         int
	viewportWidth, viewportHeight float64
}

func NewPainter() *Painter {
	return &Painter{result: Painting{Rings: make([]LightRing, 0, 2), Shapes: make([]PaintShape, 0, 96), Labels: make([]PaintLabel, 0, 128), Sprites: make([]Sprite, 0, MaxParticles)}}
}
func Opacity(c uint32, o float64) uint32 {
	return uint32(math.Round(float64(c>>24)*clamp01(o)))<<24 | c&0xffffff
}
func RouteInk(c Color) uint32 {
	switch c {
	case Green:
		return 0xff235b4d
	case Red:
		return 0xff97393f
	default:
		return 0xff734d1e
	}
}
func (p *Painter) ellipse(x, y, w, h float64, c uint32) {
	if len(p.result.Shapes) >= 96 || c>>24 == 0 {
		return
	}
	p.result.Shapes = append(p.result.Shapes, PaintShape{Ellipse: true, X: x, Y: y, W: w, H: h, Color: c})
}
func (p *Painter) sprite(s Sprite) {
	if p.viewportWidth > 0 && p.viewportHeight > 0 && !spriteIntersects(s, p.viewportWidth, p.viewportHeight, 24) {
		return
	}
	if len(p.result.Sprites) < p.limit && s.Opacity > .001 {
		p.result.Sprites = append(p.result.Sprites, s)
	}
}
func (p *Painter) label(text string, x, y, w, h, size float64, c uint32, bold, right, wet bool) {
	if text != "" && c>>24 != 0 && len(p.result.Labels) < 128 {
		p.result.Labels = append(p.result.Labels, PaintLabel{Text: text, X: x, Y: y, W: w, H: h, Size: size, Opacity: 1, Color: c, Bold: bold, Right: right, WaterClip: wet, Sheen: -1})
	}
}
func bandAt(b Band, u float64) (x, y, w, visibility float64) {
	seg := u * 32
	lo := max(0, min(31, int(seg)))
	hi := lo + 1
	v := seg - float64(lo)
	a, c := b.Points[lo], b.Points[65-lo]
	d, e := b.Points[hi], b.Points[65-hi]
	x = a.X + (d.X-a.X)*v
	y = (a.Y + c.Y + (d.Y+e.Y-a.Y-c.Y)*v) * .5
	w = (c.Y - a.Y + ((e.Y-d.Y)-(c.Y-a.Y))*v) * .5
	w = math.Max(.02, w)
	visibility = b.Opacity * (b.Visibility[lo] + (b.Visibility[hi]-b.Visibility[lo])*clamp01(v))
	return
}
func particleAt(s Scene, b Band, u float64, k int) (x, y, w, visibility float64) {
	x, y, w, visibility = bandAt(b, u)
	if u < 0 || u > 1 {
		visibility = b.Opacity
		if s.Stage.Entering || s.Stage.Exiting {
			visibility *= s.Stage.mask(u, k)
		}
	}
	return
}
func (p *Painter) Paint(s Scene) Painting {
	p.viewportWidth, p.viewportHeight = s.Width, s.Height
	p.result.Shapes = p.result.Shapes[:0]
	p.result.Labels = p.result.Labels[:0]
	p.result.Sprites = p.result.Sprites[:0]
	p.result.Rings = p.result.Rings[:0]
	p.result.Bands = s.Bands
	p.limit = s.Quality.ParticleLimit()
	if s.Stage.Hidden {
		p.result.Bands = nil
		return p.result
	}
	t := s.FlowTime
	if s.PendingCompleted > 0 {
		p.ellipse(10.5, s.Height*.5-3, 6, 6, Opacity(0xff4284b2, s.Stage.Text))
		p.ellipse(12, s.Height*.5-2, 1.7, 1.7, Opacity(0xffd9f3ff, s.Stage.Text*.9))
	}
	// Orbital instruments: a fixed pair, not one per session. They remain
	// subordinate to text and use the same light buffer as the hero ribbons.
	ringFade := max(s.Stage.Shell*.82, s.Stage.Energy*.70)
	p.result.Rings = append(p.result.Rings, LightRing{X: s.Width * .20, Y: s.Height * .56, Radius: min(62, s.Height*.39), Aspect: .88, Angle: -.19, Phase: t * .17, Opacity: ringFade * .77})
	if s.Quality <= Balanced {
		p.result.Rings = append(p.result.Rings, LightRing{X: s.Width * .75, Y: s.Height * .48, Radius: min(92, s.Height*.55), Aspect: .91, Angle: .17, Phase: -t*.12 + 1.9, Opacity: ringFade * .74})
	}
	// Status lights get first claim on the particle budget. Reading the state
	// must survive a low-quality tier even if decorative dust is dropped.
	for i, r := range s.Rows {
		ink := RouteInk(r.Row.Color)
		cy := r.Y + r.Font*.78
		op := r.Opacity * s.Stage.Text
		breath := .5 - .5*math.Cos(t*1.38+float64(i)*.73)
		glow, sheen := .22, -1.0
		light := uint32(0xff64c7a4)
		switch r.Row.Color {
		case Green:
			if r.Row.Activity == Running {
				glow = .68 + .25*breath
				sheen = math.Mod(t*.19+float64(i)*.28, 1)*1.6 - .3
			}
			p.ellipse(r.X-11, cy-2, 3.5, 3.5, Opacity(ink, op*.88))
		case Red:
			glow = .88 + .22*(.5-.5*math.Cos(t*1.85+float64(i)*.31))
			light = 0xffed8c7c
			mark := PaintShape{Count: 4, Color: Opacity(ink, op)}
			mark.Points[0] = Point{r.X - 9, cy - 3}
			mark.Points[1] = Point{r.X - 6, cy}
			mark.Points[2] = Point{r.X - 9, cy + 3}
			mark.Points[3] = Point{r.X - 12, cy}
			if len(p.result.Shapes) < 96 {
				p.result.Shapes = append(p.result.Shapes, mark)
			}
			sheen = math.Mod(t*.16+float64(i)*.17, 1)*1.7 - .35
		default:
			glow = .36 + .59*breath
			light = 0xffe8bd72
			p.ellipse(r.X-11.5, cy-2.5, 4.5, 4.5, Opacity(ink, op*(.62+.25*breath)))
			p.ellipse(r.X-10.5, cy-1.5, 2.5, 2.5, Opacity(0xfffaf9f4, op))
		}
		if s.Quality == Still {
			glow = .22
			sheen = -1
		}
		p.sprite(Sprite{X: r.X - 9, Y: cy, Radius: 9.0, Aspect: 1, Opacity: op * glow * .64, Color: light, Kind: 0, Emission: .8})
		p.sprite(Sprite{X: r.X - 9, Y: cy, Radius: 2.0, Aspect: 1, Opacity: op * min(1, glow), Color: 0xfffffaf0, Kind: 4, Emission: 1.7})
		if r.PreviousModel != "" && r.ModelMix < 1 {
			p.label(r.PreviousModel, r.X, r.Y-2*r.ModelMix, 176, 28, r.Font, Opacity(ink, op*(1-r.ModelMix)), true, false, false)
		}
		first := len(p.result.Labels)
		p.label(r.Row.Model, r.X, r.Y+2*(1-r.ModelMix), 176, 28, r.Font, Opacity(ink, op*r.ModelMix), true, false, false)
		if len(p.result.Labels) > first {
			l := &p.result.Labels[first]
			l.Sheen = sheen
			l.Glow = glow
			l.Light = light
		}
		if r.RateOpacity > .005 && r.Speed != "0.0/s" {
			at := len(p.result.Labels)
			p.label(r.Speed, s.Width-86, r.Y+2+1.7*(1-r.RateOpacity), 61, 26, r.Font*.79, Opacity(ink, op*r.RateOpacity), true, true, false)
			if len(p.result.Labels) > at {
				l := &p.result.Labels[at]
				l.Glow = glow * .68
				l.Light = light
				l.Sheen = sheen
			}
		}
	}
	// Hero caustics travel with the current path. Their narrow heads and long
	// trailing envelopes are shared with the ribbon's radiance channel.
	for k := 0; k < min(3, len(s.Bands)); k++ {
		b := s.Bands[k]
		u := heroHead(b.Phase, k)
		x, y, w, v := particleAt(s, b, u, k)
		fade := v
		p.sprite(Sprite{X: x, Y: y - w*4, Radius: 3.7, Aspect: 2.1, Angle: -.12, Opacity: fade * .94, Color: 0xfffffcf0, Kind: 4, Emission: 4.5})
		p.sprite(Sprite{X: x, Y: y, Radius: 9.5, Aspect: 1.65, Opacity: fade * .62, Color: 0xffffebc6, Kind: 0, Emission: 1.0})
		// A short tapered wake of light seeds, moving behind the same caustic.
		for j := 1; j <= 3; j++ {
			u0 := u - float64(j)*.035
			if k%2 != 0 {
				u0 = u + float64(j)*.035
			}
			x0, y0, w0, v0 := particleAt(s, b, u0, k)
			p.sprite(Sprite{X: x0, Y: y0 - w0*4, Radius: 1.08 - float64(j)*.12, Aspect: 1, Opacity: v0 * (.76 - float64(j)*.14), Color: 0xfffff9e8, Kind: 1, Emission: 1.3})
		}

	}
	// A fixed number of analytic lifetimes: each rigid facet brightens, splits
	// into three smaller pieces, then turns into a few light seeds. No child
	// emitter is created and nothing accumulates between frames.
	groups := 8
	if s.Quality == Balanced {
		groups = 4
	}
	if s.Quality >= Economy {
		groups = 2
	}
	for j := 0; j < groups && len(s.Bands) > 0; j++ {
		k := j % min(3, len(s.Bands))
		b := s.Bands[k]
		age := math.Mod(t*(.205+.011*float64(j))+float64(j)*.173, 1)
		u := particleTravel(float64(j)*.219 + t*.048 + .09)
		if k%2 != 0 {
			u = 1 - u
		}
		x, y, _, v := particleAt(s, b, u, k)
		fade := v
		p.crystal(x, y, age, t, float64(j), fade, 1)
	}
	// Assembly/retreat clusters follow the finite head/tail front, not a
	// screen wipe. Finite tails keep their own phase all the way to empty.
	if s.Stage.Energy > .015 {
		for k := 0; k < min(3, len(s.Bands)); k++ {
			b := s.Bands[k]
			u := s.Stage.Head[k] - .065
			if s.Stage.Exiting {
				u = s.Stage.Tail[k] + .10
			}
			if k%2 != 0 {
				u = 1 - u
			}
			x, y, _, v := particleAt(s, b, u, k)
			age := math.Mod(t*.8+float64(k)*.28, 1)
			p.crystal(x, y, age, t, float64(k)+9, max(v*.55, s.Stage.Energy*.78), 1.28)
		}
	}
	if s.Quality <= Balanced {
		for j := 0; j < 6; j++ {
			u := math.Mod(float64(j)*.157+t*.009, 1)
			pulse := .15 + .75*math.Pow(.5+.5*math.Sin(t*3.1+float64(j)*2.3), 6)
			p.sprite(Sprite{X: u * s.Width, Y: s.Height*(.20+.61*math.Mod(float64(j)*.317, 1)) + 3*math.Sin(t*.4+float64(j)), Radius: 1.1 + .28*float64(j%3), Aspect: 1, Opacity: s.Stage.Details * pulse * math.Sin(math.Pi*u), Color: 0xfffff5dd, Kind: 4, Emission: 1.7})
		}
	}
	qx, qw, qsize, qy := s.QuotaX, 78.0, s.QuotaFont, s.QuotaY
	if s.TotalRows == 0 {
		qw = 86
	}
	ink := uint32(0xff5d5e59)
	if !s.Quota.Fresh {
		ink = 0xff74736e
	}
	quotaText := func(text string, y, mix float64) {
		p.label(text, qx, y, qw, 43, qsize, Opacity(ink, mix*s.Stage.Text), true, false, false)
		if s.WaterCount > 0 && s.Quota.Fresh {
			p.label(text, qx, y, qw, 43, qsize, Opacity(0xff515e5b, mix*s.Stage.Text*.68), true, false, true)
		}
	}
	if s.QuotaMix < 1 {
		quotaText(s.PreviousQuotaText, qy-2*s.QuotaMix, 1-s.QuotaMix)
	}
	quotaText(s.QuotaText, qy+2*(1-s.QuotaMix), s.QuotaMix)
	caption := "周剩余"
	if !s.Quota.Known {
		caption = "额度未获取"
	} else if !s.Quota.Fresh {
		caption = "周剩余 · 旧"
	}
	p.label(caption, qx+2, qy+37, 84, 19, 9.5, Opacity(0xff757570, s.Stage.Details), false, false, false)
	if s.Synthetic {
		p.label("合成预览", s.Width-74, s.Height-19, 56, 14, 8, Opacity(0xff85857e, s.Stage.Details), false, true, false)
	}
	if s.TotalRows > s.VisibleRows {
		p.label(s.ViewportText, s.Width-135, s.Height-19, 55, 14, 8.5, Opacity(0xff757570, s.Stage.Details), false, true, false)
	}
	return p.result
}

// heroHead and the directional envelope also drive emissive ribbon shading.
// Sharing them prevents a disconnected dot from sliding over a static stripe.
func heroHead(phase float64, k int) float64 {
	// Allow the .43-long luminous wake and its blur to finish crossing the
	// outgoing edge before this source wraps to its next pass.
	period := 1 + particleTravelMargin + .52
	position := phase*.177 + .31*float64(k)
	u := position - math.Floor(position/period)*period - particleTravelMargin
	if k%2 != 0 {
		return 1 - u
	}
	return u
}
func heroPulse(u, phase float64, k int) float64 {
	head := heroHead(phase, k)
	d := head - u
	if k%2 != 0 {
		d = -d
	}
	// A compact luminous head, long asymmetric wake; no wrap discontinuity.
	if d < -.042 || d > .43 {
		return 0
	}
	if d < 0 {
		return smoother(1 + d/.042)
	}
	v := 1 - d/.43
	return v * v * v
}
func (p *Painter) crystal(x, y, age, t, seed, fade, size float64) {
	if fade < .005 {
		return
	}
	rise := smoother(age / .10)
	split := smoother((age - .38) / .29)
	body := rise * (1 - smoother((age-.46)/.21))
	flash := math.Exp(-math.Pow((age-.49)/.075, 2))
	p.sprite(Sprite{X: x, Y: y - 3*math.Sin(seed+t*.34), Radius: (3.10 + .35*math.Sin(seed)) * size, Aspect: .85, Angle: seed*.71 + t*.23, Opacity: fade * body, Color: 0xfffff9ed, Kind: 2, Emission: .28 + flash*3.5})
	for q := 0; q < 3; q++ {
		angle := seed*1.72 + float64(q)*2.2
		sn, cs := math.Sincos(angle)
		dist := (2 + split*(8+float64(q)*3)) * size
		x1, y1 := x+cs*dist+split*8, y+sn*dist-split*6
		fadeChild := fade * split * (1 - smoother((age-.80)/.20))
		twinkle := .32 + .68*math.Pow(.5+.5*math.Sin(t*4.7+seed+float64(q)*1.7), 5)
		radius := (1.55 - .70*smoother((age-.61)/.30)) * size
		kind := uint8(2)
		if age > .69 {
			kind = 4
		}
		p.sprite(Sprite{X: x1, Y: y1, Radius: radius, Aspect: .8, Angle: angle + t*.39, Opacity: fadeChild * (.60 + .40*twinkle), Color: 0xfffff8e6, Kind: kind, Emission: .65 + twinkle*2.6})
	}
	if flash > .035 {
		p.sprite(Sprite{X: x, Y: y, Radius: 5.6 * size, Aspect: 1.15, Angle: .20, Opacity: fade * flash, Color: 0xffffefd0, Kind: 4, Emission: 5.0})
	}
}
