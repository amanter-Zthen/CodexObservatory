package floatui

// Small, bounded CPU compositor. This is deliberate for a 388-DIP instrument:
// no browser, scene graph, 3-D simulation, or video decoder. Selective radiance
// is accumulated on a small reusable grid, blurred at two radii and composited
// before readable glyphs. Pre-baked optics remain live along deformed paths.
// Desktop output is premultiplied LDR; this is not an HDR swapchain.
import (
	"math"
	"sort"
	"sync"
)

type Compositor struct {
	light        lightBuffer
	LightEnabled bool // diagnostic bypass; normal/native construction enables it

	material                  Material
	surface                   Material
	clip                      []byte
	waterTops                 []int
	cachedHeight, cachedWidth int
	cachedScale               float64
	scale                     float64
	scene                     Scene
	MaterialBuilds            uint64
	Frames                    uint64
}

func NewCompositor() *Compositor { return &Compositor{LightEnabled: true} }

var opticsOnce sync.Once
var ribbonAtlas [64][256]uint32
var veilAtlas [256]uint32
var spriteAtlas [5][32 * 32]uint32

const AtlasBytes = 64*256*4 + 256*4 + 5*32*32*4

func argb(r, g, b, a float64) uint32 {
	av := uint32(math.Round(clamp01(a) * 255))
	return av<<24 | uint32(math.Round(r*float64(av)/255))<<16 | uint32(math.Round(g*float64(av)/255))<<8 | uint32(math.Round(b*float64(av)/255))
}
func warmOptics() {
	opticsOnce.Do(func() {
		for l := 0; l < 64; l++ {
			light := float64(l) / 63
			for j := 0; j < 256; j++ {
				d := float64(j)/255*2 - 1
				// Neutral transmitted body, soft underside, narrow reflected
				// crest. These are a material cross-section, not rainbow stops.
				shadow := math.Exp(-math.Pow((d-.30)/.26, 2)) * .43 * (1 - .28*light)
				crest := math.Exp(-math.Pow((d+.22)/.105, 2)) * (.62 + .34*light)
				inner := math.Exp(-math.Pow((d+.05)/.32, 2)) * (.40 + .24*light)
				total := shadow + crest + inner
				a := clamp01(total)
				c := .0
				if a > 0 {
					c = (shadow*162 + (crest+inner)*255) / total
				}
				ribbonAtlas[l][j] = argb(c, c*.996, c*.979, a)
				if l == 0 {
					av := math.Exp(-d*d*5) * .15
					veilAtlas[j] = argb(174, 177, 175, av)
				}
			}
		}
		for kind := 0; kind < 5; kind++ {
			for y := 0; y < 32; y++ {
				for x := 0; x < 32; x++ {
					ux, uy := (float64(x)+.5)/16-1, (float64(y)+.5)/16-1
					d := ux*ux + uy*uy
					a, brightness := 0.0, 1.0
					switch kind {
					case 0:
						a = math.Exp(-d*5) * smoother((1-math.Sqrt(d))/.16)
					case 1:
						a = clamp01((1 - math.Sqrt(d)) / .23)
						brightness = .80 + .20*clamp01(1-math.Hypot(ux+.22, uy+.25))
					case 2:
						// Three shaded faces and a bevel, rather than a flat dot.
						edge := math.Max(math.Abs(ux)*.83+math.Abs(uy)*.44, math.Abs(uy)*.88+math.Abs(ux)*.34)
						a = clamp01((.81 - edge) / .085)
						brightness = .95
						if uy > -.23*ux {
							brightness = .66
						}
						if ux > .19 && uy > -.38 {
							brightness = .49
						}
						if edge > .65 && uy < .15 {
							brightness = 1
						}
					case 3:
						a = math.Exp(-uy*uy*18) * smoother((1-math.Abs(ux))/.25)
					case 4:
						a = clamp01(math.Exp(-d*60)+.70*math.Exp(-math.Abs(ux)*42-math.Abs(uy)*3)+.70*math.Exp(-math.Abs(uy)*42-math.Abs(ux)*3)) * smoother((1-math.Sqrt(d))/.18)
					}
					spriteAtlas[kind][y*32+x] = argb(255*brightness, 255*brightness, 255*brightness, a)
				}
			}
		}
	})
}
func (c *Compositor) prepare(s Scene, scale float64) {
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		scale = 1
	}
	scale = math.Min(scale, 4)
	c.scale = scale
	c.scene = s
	pw, ph := int(math.Ceil((s.Width+12)*scale)), int(math.Ceil((s.Height+12)*scale))
	if c.cachedHeight != ph || c.cachedWidth != pw || c.cachedScale != scale {
		c.material = MaterialInto(c.material, s.Width, s.Height, scale)
		c.cachedHeight, c.cachedWidth, c.cachedScale = ph, pw, scale
		c.MaterialBuilds++
		size := pw * ph
		if cap(c.clip) < size {
			c.clip = make([]byte, size, max(size, cap(c.clip)*2))
		} else {
			c.clip = c.clip[:size]
		}
		// Rounded inside clip is precomputed, not re-evaluated per effect.
		for y := 0; y < ph; y++ {
			for x := 0; x < pw; x++ {
				dx, dy := (float64(x)+.5)/scale-6-1.5, (float64(y)+.5)/scale-6-1.5
				d := roundDistance(dx, dy, s.Width-3, s.Height-3, FrameRadius-1.5)
				c.clip[y*pw+x] = byte(math.Round(clamp01(.5-d*scale) * 255))
			}
		}
	}
	size := pw * ph * 4
	if cap(c.surface.BGRA) < size {
		c.surface.BGRA = make([]byte, size, max(size, cap(c.surface.BGRA)*2))
	} else {
		c.surface.BGRA = c.surface.BGRA[:size]
	}
	c.surface.Width, c.surface.Height = pw, ph
	if cap(c.waterTops) < pw {
		c.waterTops = make([]int, pw)
	} else {
		c.waterTops = c.waterTops[:pw]
	}
}
func (c *Compositor) Render(s Scene, p Painting, scale float64) Material {
	warmOptics()
	c.prepare(s, scale)
	c.Frames++
	if s.Stage.Hidden {
		clear(c.surface.BGRA)
		clear(c.light.raw)
		clear(c.light.near)
		clear(c.light.far)
		clear(c.light.scratch)
		clear(c.light.packed)
		return c.surface
	}
	if s.Stage.Shell >= .99999 {
		copy(c.surface.BGRA, c.material.BGRA)
	} else {
		a := uint32(math.Round(clamp01(s.Stage.Shell) * 255))
		for i, v := range c.material.BGRA {
			c.surface.BGRA[i] = byte((uint32(v)*a + 127) / 255)
		}
	}
	c.light.prepare(c.surface.Width, c.surface.Height, c.scale, s.Quality)
	c.water(s)
	c.rings(p.Rings)
	// Wide, nearly transparent strata give depth without a flat green sea.
	if s.Quality <= Balanced {
		for k := 0; k < min(2, len(p.Bands)); k++ {
			c.band(p.Bands[k], 33, .83*s.Stage.Shell, true)
		}
	}
	for k, b := range p.Bands {
		width := 23.0
		if k >= 3 {
			width = 9.0
		}
		c.band(b, width, 1, false)
		if c.LightEnabled {
			c.emitBand(b, width, k)
		}
	}
	for _, sp := range p.Sprites {
		c.sprite(sp)
	}
	for _, sh := range p.Shapes {
		c.shape(sh)
	}
	if c.LightEnabled {
		c.applyBloom()
	}
	return c.surface
}

// blend takes a PREMULTIPLIED ARGB source. Coverage is multiplied once; RGB
// never exceeds alpha, so particles cannot leave black halos on the desktop.
func (c *Compositor) blend(x, y int, color uint32, coverage uint32) {
	if x < 0 || y < 0 || x >= c.surface.Width || y >= c.surface.Height || coverage == 0 {
		return
	}
	pi := y*c.surface.Width + x
	coverage = (coverage*uint32(c.clip[pi]) + 127) / 255
	a := (uint32(color>>24)*coverage + 127) / 255
	if a == 0 {
		return
	}
	i := pi * 4
	dst := c.surface.BGRA
	inv := 255 - a
	dst[i] = byte((uint32(byte(color))*coverage+127)/255 + (uint32(dst[i])*inv+127)/255)
	dst[i+1] = byte((uint32(byte(color>>8))*coverage+127)/255 + (uint32(dst[i+1])*inv+127)/255)
	dst[i+2] = byte((uint32(byte(color>>16))*coverage+127)/255 + (uint32(dst[i+2])*inv+127)/255)
	dst[i+3] = byte(a + (uint32(dst[i+3])*inv+127)/255)
}
func premul(color uint32) uint32 {
	a := color >> 24
	return a<<24 | ((uint32(byte(color>>16))*a+127)/255)<<16 | ((uint32(byte(color>>8))*a+127)/255)<<8 | (uint32(byte(color))*a+127)/255
}
func (c *Compositor) band(b Band, width, opacity float64, veil bool) {
	if b.Opacity <= .001 || b.Count != 66 {
		return
	}
	scale := c.scale
	for seg := 0; seg < 32; seg++ {
		left, right := b.Points[seg].X, b.Points[seg+1].X
		xa := max(0, int(math.Ceil((left+6)*scale-.5)))
		xb := min(c.surface.Width, int(math.Ceil((right+6)*scale-.5)))
		if right <= left {
			continue
		}
		cy0 := (b.Points[seg].Y + b.Points[65-seg].Y) * .5
		cy1 := (b.Points[seg+1].Y + b.Points[64-seg].Y) * .5
		rw0 := (b.Points[65-seg].Y - b.Points[seg].Y) * .5 * width
		rw1 := (b.Points[64-seg].Y - b.Points[seg+1].Y) * .5 * width
		for x := xa; x < xb; x++ {
			u := ((float64(x)+.5)/scale - 6 - left) / (right - left)
			alpha := clamp01((b.Visibility[seg] + (b.Visibility[seg+1]-b.Visibility[seg])*u) * b.Opacity * opacity)
			if alpha < .001 {
				continue
			}
			cov := uint32(math.Round(alpha * 255))
			cy := (cy0 + (cy1-cy0)*u + 6) * scale
			radius := math.Max(.8, (rw0+(rw1-rw0)*u+.48)*scale)
			light := int(clamp01(b.Light[seg]+(b.Light[seg+1]-b.Light[seg])*u) * 63)
			ya := max(0, int(math.Floor(cy-radius)))
			yb := min(c.surface.Height, int(math.Ceil(cy+radius)))
			for y := ya; y < yb; y++ {
				v := int(((float64(y)+.5-cy)/radius + 1) * 127.5)
				if v < 0 || v > 255 {
					continue
				}
				col := ribbonAtlas[light][v]
				if veil {
					col = veilAtlas[v]
				}
				c.blend(x, y, col, cov)
			}
		}
	}
}
func (c *Compositor) water(s Scene) {
	if s.WaterCount == 0 || s.Stage.Shell < .001 {
		return
	}
	scale := c.scale
	floor := (s.Height + 6) * scale
	op := s.Stage.Shell
	if !s.Quota.Fresh {
		op *= .46
	}
	cov := uint32(op * 255)
	// The quota body is a low-frequency row gradient. Evaluate its geometry
	// per column, then shade CONTIGUOUS scanlines. No per-pixel sine, division,
	// or floating point clamp, and no column-strided framebuffer traversal.
	// The animated boundary is retained exactly; its tiny depth variation does
	// not need a separate gradient for every x coordinate.
	for i := range c.waterTops {
		c.waterTops[i] = int(floor * 256)
	}
	minSurface, sumSurface, columns := floor, 0.0, 0
	for seg := 0; seg < 32; seg++ {
		a, b := s.Water[seg], s.Water[seg+1]
		xa := max(0, int(math.Ceil((a.X+6)*scale-.5)))
		xb := min(c.surface.Width, int(math.Ceil((b.X+6)*scale-.5)))
		for x := xa; x < xb; x++ {
			u := ((float64(x)+.5)/scale - 6 - a.X) / (b.X - a.X)
			y := (a.Y + (b.Y-a.Y)*u + 6) * scale
			c.waterTops[x] = int(y * 256)
			minSurface = min(minSurface, y)
			sumSurface += y
			columns++
		}
	}
	if columns == 0 || minSurface >= floor-.01 {
		return
	}
	avgSurface := sumSurface / float64(columns)
	denom := max(1.0, floor-avgSurface)
	firstY := max(0, int(math.Floor(minSurface)))
	lastY := min(c.surface.Height, int(math.Ceil(floor)))
	dst := c.surface.BGRA
	for y := firstY; y < lastY; y++ {
		depth := clamp01((float64(y) + .5 - avgSurface) / denom)
		av := uint32((.023 + .043*depth) * 255)
		color := av<<24 | (188*av/255)<<16 | (196*av/255)<<8 | (198 * av / 255)
		// Pre-multiply shell/freshness ONCE per row. Opaque interior can bypass
		// the general rounded/transparent compositor; edge pixels use it intact.
		aa := (av*cov + 127) / 255
		inv := 255 - aa
		sb := (uint32(byte(color))*cov + 127) / 255
		sg := (uint32(byte(color>>8))*cov + 127) / 255
		sr := (uint32(byte(color>>16))*cov + 127) / 255
		endY := (y + 1) * 256
		for x, top := range c.waterTops {
			cover := endY - top
			if cover <= 0 {
				continue
			}
			pi := y*c.surface.Width + x
			i := pi * 4
			if cover >= 256 && c.clip[pi] == 255 && dst[i+3] == 255 {
				dst[i] = byte(sb + (uint32(dst[i])*inv+127)/255)
				dst[i+1] = byte(sg + (uint32(dst[i+1])*inv+127)/255)
				dst[i+2] = byte(sr + (uint32(dst[i+2])*inv+127)/255)
			} else {
				coverage := cov
				if cover < 256 {
					coverage = (coverage*uint32(cover) + 128) / 256
				}
				c.blend(x, y, color, coverage)
			}
		}
	}
	// Two phase-offset ripples under the free surface use the same baked optics.
	for k := 0; k < 2; k++ {
		if k == 1 && s.Quality >= Economy {
			break
		}
		b := Band{Count: 66, Opacity: .32 * op}
		for j := 0; j <= 32; j++ {
			u := float64(j) / 32
			y := s.Water[j].Y + float64(k)*5 + (1.2+float64(k))*math.Sin(u*7.1+s.FlowTime*.34+float64(k)*1.8)
			if y > s.Height+1 {
				y = s.Height + 1
			}
			w := .5 + .12*math.Sin(u*5.4-s.FlowTime*.21)
			b.Points[j] = Point{s.Water[j].X, y - w}
			b.Points[65-j] = Point{s.Water[j].X, y + w}
			b.Light[j] = .35 + .35*math.Sin(u*4.1-s.FlowTime*.67)*math.Sin(u*4.1-s.FlowTime*.67)
			b.Visibility[j] = 1
		}
		c.band(b, 3.2, 1, false)
	}
}
func (c *Compositor) sprite(s Sprite) {
	if !spriteIntersects(s, c.scene.Width, c.scene.Height, float64(8*max(1, c.light.step))/c.scale) {
		return
	}
	radius := s.Radius * c.scale
	if radius <= 0 || s.Opacity <= 0 {
		return
	}
	aspect := s.Aspect
	if aspect <= 0 {
		aspect = 1
	}
	rx, ry := radius*aspect, radius
	cx, cy := (s.X+6)*c.scale, (s.Y+6)*c.scale
	sn, cs := math.Sincos(s.Angle)
	extent := math.Max(rx, ry) * 1.42
	x0, x1 := max(0, int(cx-extent)), min(c.surface.Width, int(math.Ceil(cx+extent)))
	y0, y1 := max(0, int(cy-extent)), min(c.surface.Height, int(math.Ceil(cy+extent)))
	cov := uint32(clamp01(s.Opacity) * 255)
	base := s.Color
	if c.LightEnabled && s.Emission > 0 {
		c.light.spot(cx, cy, max(2.2*c.scale, radius*1.05), s.Color, s.Emission*s.Opacity)
	}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			dx, dy := float64(x)+.5-cx, float64(y)+.5-cy
			u, v := (dx*cs+dy*sn)/rx, (-dx*sn+dy*cs)/ry
			tx, ty := int((u+1)*16), int((v+1)*16)
			if u < -1 || v < -1 || tx < 0 || tx >= 32 || ty < 0 || ty >= 32 {
				continue
			}
			sample := spriteAtlas[s.Kind%5][ty*32+tx]
			a := sample >> 24
			if a == 0 {
				continue
			}
			col := a<<24 | ((uint32(byte(sample>>16))*uint32(byte(base>>16))+127)/255)<<16 | ((uint32(byte(sample>>8))*uint32(byte(base>>8))+127)/255)<<8 | (uint32(byte(sample))*uint32(byte(base))+127)/255
			c.blend(x, y, col, cov)
		}
	}
}
func (c *Compositor) shape(s PaintShape) {
	color := premul(s.Color)
	scale := c.scale
	if s.Ellipse {
		rx, ry := s.W*.5*scale, s.H*.5*scale
		if rx <= 0 || ry <= 0 {
			return
		}
		cx, cy := (s.X+s.W*.5+6)*scale, (s.Y+s.H*.5+6)*scale
		for y := max(0, int(cy-ry-1)); y < min(c.surface.Height, int(math.Ceil(cy+ry+1))); y++ {
			for x := max(0, int(cx-rx-1)); x < min(c.surface.Width, int(math.Ceil(cx+rx+1))); x++ {
				d := math.Hypot((float64(x)+.5-cx)/rx, (float64(y)+.5-cy)/ry)
				cov := uint32(clamp01((1-d)*math.Min(rx, ry)+.5) * 255)
				c.blend(x, y, color, cov)
			}
		}
		return
	}
	if s.Count < 3 || s.Count > 66 {
		return
	}
	ymin, ymax := math.Inf(1), math.Inf(-1)
	for _, p := range s.Points[:s.Count] {
		ymin = math.Min(ymin, p.Y)
		ymax = math.Max(ymax, p.Y)
	}
	var hits [66]float64
	for y := max(0, int((ymin+6)*scale)); y < min(c.surface.Height, int(math.Ceil((ymax+6)*scale))); y++ {
		fy := (float64(y)+.5)/scale - 6
		n := 0
		for i := 0; i < s.Count; i++ {
			a, b := s.Points[i], s.Points[(i+1)%s.Count]
			if (a.Y <= fy && b.Y > fy) || (b.Y <= fy && a.Y > fy) {
				hits[n] = (a.X + (fy-a.Y)*(b.X-a.X)/(b.Y-a.Y) + 6) * scale
				n++
			}
		}
		sort.Float64s(hits[:n])
		for i := 0; i+1 < n; i += 2 {
			for x := max(0, int(hits[i])); x < min(c.surface.Width, int(math.Ceil(hits[i+1]))); x++ {
				cov := uint32(clamp01(math.Min(float64(x+1), hits[i+1])-math.Max(float64(x), hits[i])) * 255)
				c.blend(x, y, color, cov)
			}
		}
	}
}

// CopyRGBA is for the portable proof only. Native Windows takes BGRA directly.
func (c *Compositor) CopyRGBA(dst []byte) []byte {
	if cap(dst) < len(c.surface.BGRA) {
		dst = make([]byte, len(c.surface.BGRA))
	} else {
		dst = dst[:len(c.surface.BGRA)]
	}
	for i := 0; i < len(dst); i += 4 {
		a := uint32(c.surface.BGRA[i+3])
		dst[i+3] = byte(a)
		if a == 0 {
			dst[i], dst[i+1], dst[i+2] = 0, 0, 0
		} else {
			dst[i] = byte((uint32(c.surface.BGRA[i+2])*255 + a/2) / a)
			dst[i+1] = byte((uint32(c.surface.BGRA[i+1])*255 + a/2) / a)
			dst[i+2] = byte((uint32(c.surface.BGRA[i])*255 + a/2) / a)
		}
	}
	return dst
}
