package floatui

import "math"

const GlyphReferenceSize = 32.0
const GlyphPadding = 12.0

// GlyphMask contains no font file. A platform rasterises it once, then the
// compositor resamples this mask at continuous floating-point sizes/positions.
// Glow is a small cached blur, not a per-frame blur of the whole window.
type GlyphMask struct {
	Width, Height     int
	Advance           float64
	Alpha, Glow, Aura []byte
}

func (g GlyphMask) Bytes() int { return len(g.Alpha) + len(g.Glow) + len(g.Aura) }

// Both narrow optical edge and broad aura are cached with the glyph. Padding
// belongs to the mask, not to the layout: the baseline/advance never changes.
func MakeGlyphMask(width, height int, advance float64, alpha []byte) GlyphMask {
	if width <= 0 || height <= 0 || len(alpha) != width*height {
		return GlyphMask{}
	}
	g := GlyphMask{Width: width, Height: height, Advance: advance, Alpha: alpha, Glow: make([]byte, len(alpha)), Aura: make([]byte, len(alpha))}
	tmp := make([]byte, len(alpha))
	blurMask(alpha, tmp, g.Glow, width, height, 1)
	blurMask(alpha, tmp, g.Aura, width, height, 5)
	blurMask(g.Aura, tmp, g.Aura, width, height, 5)
	return g
}

// Rolling separable box filters are sufficient for the cached soft aura. They
// run only on a mask cache miss, not in the per-frame animation loop.
func blurMask(src, tmp, dst []byte, w, h, r int) {
	div := r*2 + 1
	for y := 0; y < h; y++ {
		sum := 0
		for x := 0; x <= r && x < w; x++ {
			sum += int(src[y*w+x])
		}
		for x := 0; x < w; x++ {
			tmp[y*w+x] = byte((sum + div/2) / div)
			if x-r >= 0 {
				sum -= int(src[y*w+x-r])
			}
			if x+r+1 < w {
				sum += int(src[y*w+x+r+1])
			}
		}
	}
	for x := 0; x < w; x++ {
		sum := 0
		for y := 0; y <= r && y < h; y++ {
			sum += int(tmp[y*w+x])
		}
		for y := 0; y < h; y++ {
			dst[y*w+x] = byte((sum + div/2) / div)
			if y-r >= 0 {
				sum -= int(tmp[(y-r)*w+x])
			}
			if y+r+1 < h {
				sum += int(tmp[(y+r+1)*w+x])
			}
		}
	}
}
func maskSample(pixels []byte, w, h int, x, y float64) uint32 {
	x -= .5
	y -= .5
	ix, iy := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(ix), y-float64(iy)
	var a, b, c, d float64
	if iy >= 0 && iy < h {
		if ix >= 0 && ix < w {
			a = float64(pixels[iy*w+ix])
		}
		if ix+1 >= 0 && ix+1 < w {
			b = float64(pixels[iy*w+ix+1])
		}
	}
	if iy+1 >= 0 && iy+1 < h {
		if ix >= 0 && ix < w {
			c = float64(pixels[(iy+1)*w+ix])
		}
		if ix+1 >= 0 && ix+1 < w {
			d = float64(pixels[(iy+1)*w+ix+1])
		}
	}
	return uint32(math.Round((a*(1-fx)+b*fx)*(1-fy) + (c*(1-fx)+d*fx)*fy))
}
func (c *Compositor) DrawGlyph(l PaintLabel, g GlyphMask) {
	if l.Color>>24 == 0 || g.Width <= 0 || g.Height <= 0 || len(g.Alpha) != g.Width*g.Height || l.Size <= 0 || c.scene.Stage.Hidden {
		return
	}
	zoom := l.Size / GlyphReferenceSize * c.scale
	logicalX := l.X
	if l.Right {
		logicalX = l.X + l.W - g.Advance*l.Size/GlyphReferenceSize
	}
	ox := (logicalX+6)*c.scale - GlyphPadding*zoom
	oy := (l.Y+6)*c.scale - GlyphPadding*zoom
	// Do not crop the glow at the old glyph rectangle. Only the readable ink
	// respects the text rectangle; the precomputed aura owns its margin.
	x0 := max(0, int(math.Floor(ox)))
	y0 := max(0, int(math.Floor(oy)))
	x1 := min(c.surface.Width, int(math.Ceil(ox+float64(g.Width)*zoom)), int(math.Ceil((l.X+l.W+6)*c.scale+GlyphPadding*zoom)))
	y1 := min(c.surface.Height, int(math.Ceil(oy+float64(g.Height)*zoom)))
	base := l.Color
	opacity := uint32(clamp01(l.Opacity) * 255)
	coreX0, coreX1 := (l.X+6)*c.scale, (l.X+l.W+6)*c.scale
	coreY0, coreY1 := (l.Y+6)*c.scale, (l.Y+l.H+6)*c.scale
	for x := x0; x < x1; x++ {
		sx := (float64(x) + .5 - ox) / zoom
		py0 := y0
		if l.WaterClip {
			if c.scene.WaterCount == 0 {
				continue
			}
			u := clamp01(((float64(x)+.5)/c.scale-6)/c.scene.Width) * 32
			i := min(31, int(u))
			f := u - float64(i)
			waterY := c.scene.Water[i].Y + (c.scene.Water[i+1].Y-c.scene.Water[i].Y)*f
			py0 = max(py0, int(math.Ceil((waterY+6)*c.scale)))
		}
		sheen := 0.0
		if c.LightEnabled && l.Sheen >= 0 && l.Sheen <= 1.3 {
			u := (sx - GlyphPadding) / max(g.Advance, 1)
			sheen = clamp01(1 - math.Abs(u-l.Sheen)/.105)
			sheen *= sheen
		}
		col := base
		if sheen > 0 {
			strength := sheen * .16
			rr := float64(byte(base>>16)) + (float64(byte(l.Light>>16))-float64(byte(base>>16)))*strength
			gg := float64(byte(base>>8)) + (float64(byte(l.Light>>8))-float64(byte(base>>8)))*strength
			bb := float64(byte(base)) + (float64(byte(l.Light))-float64(byte(base)))*strength
			col = base&0xff000000 | uint32(rr)<<16 | uint32(gg)<<8 | uint32(bb)
		}
		col = premul(col)
		strength := l.Glow * float64(base>>24) / 255
		aura := premul(Opacity(l.Light, strength*(.72+.25*sheen)))
		near := premul(Opacity(0xfffffaf0, strength*(.31+.43*sheen)))
		for y := py0; y < y1; y++ {
			sy := (float64(y) + .5 - oy) / zoom
			if c.LightEnabled && l.Glow > .01 && len(g.Aura) == len(g.Alpha) {
				a := maskSample(g.Aura, g.Width, g.Height, sx, sy)
				c.blend(x, y, aura, a*opacity/255)
				a = maskSample(g.Glow, g.Width, g.Height, sx, sy)
				c.blend(x, y, near, a*opacity/255)
			}
			if float64(x)+.5 < coreX0 || float64(x)+.5 >= coreX1 || float64(y)+.5 < coreY0 || float64(y)+.5 >= coreY1 {
				continue
			}
			a := maskSample(g.Alpha, g.Width, g.Height, sx, sy)
			c.blend(x, y, col, a*opacity/255)
			// A small moving rim spark is confined to the glyph contour. It never
			// bleaches an entire word or turns into a glowing rectangle.
			if c.LightEnabled && sheen > .005 && l.Glow > .01 {
				up := maskSample(g.Alpha, g.Width, g.Height, sx, sy-1.25)
				if a > up {
					edge := uint32(float64(a-up) * sheen * .63)
					c.blend(x, y, premul(Opacity(0xfffff7e4, strength)), edge*opacity/255)
				}
			}
		}
	}
}
