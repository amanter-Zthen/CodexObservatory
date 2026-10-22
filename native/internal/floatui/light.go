package floatui

// Selective bloom, adapted as an ORIGINAL small CPU implementation. The design
// references are three.js UnrealBloomPass (multiple blur scales) and pmndrs
// SelectiveBloomEffect (only selected emitters). No upstream shader is copied.
// An integer radiance grid (255 = unit intensity) preserves values above 1
// until compression.
// This is local LDR desktop compositing, not physical light transport or an HDR
// output surface. Geometry/text remain sharp; the whole white panel never blooms.
import "math"

type lightPixel struct{ r, g, b uint32 }
type lightBuffer struct {
	w, h, step              int
	raw, near, far, scratch []lightPixel
	builds                  uint64
	pw, ph                  int
	packed                  []uint32
	row0, row1              []uint32
	xIndex, yIndex          []int
	xWeight, yWeight        []uint32
}

const lightPadding = 8 // six blur taps of support plus bilinear/sampling guards

func reuseLight(v []lightPixel, n int) []lightPixel {
	if cap(v) < n {
		return make([]lightPixel, n, max(n, cap(v)*2))
	}
	v = v[:n]
	clear(v)
	return v
}
func reuse32(v []uint32, n int) []uint32 {
	if cap(v) < n {
		return make([]uint32, n, max(n, cap(v)*2))
	}
	return v[:n]
}
func reuseInt(v []int, n int) []int {
	if cap(v) < n {
		return make([]int, n, max(n, cap(v)*2))
	}
	return v[:n]
}
func (l *lightBuffer) prepare(w, h int, scale float64, q Quality) {
	step := max(2, int(math.Round(3*scale)))
	if q >= Economy {
		step = max(3, int(math.Round(4*scale)))
	}
	lw, lh := (w+step-1)/step+2*lightPadding, (h+step-1)/step+2*lightPadding
	if l.w != lw || l.h != lh || l.step != step || l.pw != w || l.ph != h {
		l.w, l.h, l.step, l.pw, l.ph = lw, lh, step, w, h
		n := lw * lh
		l.raw = reuseLight(l.raw, n)
		l.near = reuseLight(l.near, n)
		l.far = reuseLight(l.far, n)
		l.scratch = reuseLight(l.scratch, n)
		l.packed = reuse32(l.packed, n)
		l.row0 = reuse32(l.row0, w)
		l.row1 = reuse32(l.row1, w)
		l.xIndex = reuseInt(l.xIndex, w)
		l.yIndex = reuseInt(l.yIndex, h)
		l.xWeight = reuse32(l.xWeight, w)
		l.yWeight = reuse32(l.yWeight, h)
		for x := 0; x < w; x++ {
			g := float64(lightPadding) + (float64(x)+.5)/float64(step) - .5
			i := min(lw-1, int(g))
			l.xIndex[x] = i
			l.xWeight[x] = uint32((g - float64(i)) * 256)
		}
		for y := 0; y < h; y++ {
			g := float64(lightPadding) + (float64(y)+.5)/float64(step) - .5
			i := min(lh-1, int(g))
			l.yIndex[y] = i
			l.yWeight[y] = uint32((g - float64(i)) * 256)
		}
		l.builds++
	} else {
		clear(l.raw)
	}
}
func (l *lightBuffer) add(x, y int, r, g, b uint32) {
	if x < 0 || y < 0 || x >= l.w || y >= l.h {
		return
	}
	p := &l.raw[y*l.w+x]
	p.r = min(uint32(65535), p.r+r)
	p.g = min(uint32(65535), p.g+g)
	p.b = min(uint32(65535), p.b+b)
}
func (l *lightBuffer) spot(x, y, radius float64, color uint32, energy float64) {
	if energy <= .001 || l.step == 0 {
		return
	}
	cx, cy := x/float64(l.step)+lightPadding, y/float64(l.step)+lightPadding
	r := max(.85, radius/float64(l.step))
	inv := 1 / (r * r)
	vr, vg, vb := float64(byte(color>>16))*energy, float64(byte(color>>8))*energy, float64(byte(color))*energy
	for y0 := max(0, int(cy-r-1)); y0 < min(l.h, int(math.Ceil(cy+r+1))); y0++ {
		for x0 := max(0, int(cx-r-1)); x0 < min(l.w, int(math.Ceil(cx+r+1))); x0++ {
			dx, dy := (float64(x0) + .5 - cx), (float64(y0) + .5 - cy)
			v := 1 - (dx*dx+dy*dy)*inv
			if v <= 0 {
				continue
			}
			v = v * v
			l.add(x0, y0, uint32(vr*v+.5), uint32(vg*v+.5), uint32(vb*v+.5))
		}
	}
}

// Separable five-tap binomial blur. Fixed work, no dynamic kernel or allocation.
func blurLight(src, tmp, dst []lightPixel, w, h, stride int) {
	weights := [5]uint32{1, 4, 6, 4, 1}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var p lightPixel
			for k := -2; k <= 2; k++ {
				xx := x + k*stride
				if xx < 0 || xx >= w {
					continue
				}
				v := src[y*w+xx]
				a := weights[k+2]
				p.r += v.r * a
				p.g += v.g * a
				p.b += v.b * a
			}
			tmp[y*w+x] = lightPixel{(p.r + 8) / 16, (p.g + 8) / 16, (p.b + 8) / 16}
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var p lightPixel
			for k := -2; k <= 2; k++ {
				yy := y + k*stride
				if yy < 0 || yy >= h {
					continue
				}
				v := tmp[yy*w+x]
				a := weights[k+2]
				p.r += v.r * a
				p.g += v.g * a
				p.b += v.b * a
			}
			dst[y*w+x] = lightPixel{(p.r + 8) / 16, (p.g + 8) / 16, (p.b + 8) / 16}
		}
	}
}
func (c *Compositor) emitBand(b Band, width float64, k int) {
	if b.Opacity < .001 {
		return
	}
	l := &c.light
	for x := 0; x < l.w; x++ {
		u := ((float64(x-lightPadding)+.5)*float64(l.step)/c.scale - 6) / c.scene.Width
		if u < 0 || u > 1 {
			continue
		}
		_, y, w, v := bandAt(b, u)
		if v < .003 {
			continue
		}
		pulse := heroPulse(u, b.Phase, k)
		intensity := v * (.34 + 3.4*pulse)
		if k >= 3 {
			intensity *= .39
		}
		cy := ((y-w*width*.22+6)*c.scale)/float64(l.step) + lightPadding
		r := max(.95, w*width*.38*c.scale/float64(l.step))
		for yy := max(0, int(cy-r-1)); yy < min(l.h, int(math.Ceil(cy+r+1))); yy++ {
			d := math.Abs(float64(yy)+.5-cy) / r
			if d >= 1 {
				continue
			}
			a := (1 - d*d)
			a = a * a * intensity
			l.add(x, yy, uint32(a*255), uint32(a*251), uint32(a*239))
		}
	}
}

// Two independent packed 16-bit lanes interpolate RGBA without float work.
// All values are premultiplied and each pair's weights sum to exactly 256.
func lerpPacked(a, b, w uint32) uint32 {
	i := uint32(256) - w
	rb := (((a&0x00ff00ff)*i + (b&0x00ff00ff)*w) >> 8) & 0x00ff00ff
	ag := (((a>>8)&0x00ff00ff)*i + ((b>>8)&0x00ff00ff)*w) & 0xff00ff00
	return rb | ag
}
func (l *lightBuffer) expandRow(row []uint32, y int) {
	start := y * l.w
	for x, i := range l.xIndex {
		row[x] = lerpPacked(l.packed[start+i], l.packed[start+min(l.w-1, i+1)], l.xWeight[x])
	}
}
func (c *Compositor) applyBloom() {
	l := &c.light
	if len(l.raw) == 0 {
		return
	}
	blurLight(l.raw, l.scratch, l.near, l.w, l.h, 1)
	wide := c.scene.Quality <= Balanced
	if wide {
		blurLight(l.near, l.scratch, l.far, l.w, l.h, 2)
	}
	// Compress/combine once on the small grid, NOT twice per final pixel.
	// Horizontal rows are expanded once and reused for multiple output rows.
	for i, p := range l.near {
		if wide {
			f := l.far[i]
			p.r = (p.r*3 + f.r*2) / 4
			p.g = (p.g*3 + f.g*2) / 4
			p.b = (p.b*3 + f.b*2) / 4
		}
		peak := max(p.r, p.g, p.b)
		if peak < 2 {
			l.packed[i] = 0
			continue
		}
		a := min(uint32(242), peak*255/(peak+105))
		l.packed[i] = a<<24 | (p.r*a/peak)<<16 | (p.g*a/peak)<<8 | (p.b * a / peak)
	}
	last := -2
	dst := c.surface.BGRA
	for y, yi := range l.yIndex {
		if yi != last {
			if yi == last+1 {
				l.row0, l.row1 = l.row1, l.row0
			} else {
				l.expandRow(l.row0, yi)
			}
			l.expandRow(l.row1, min(l.h-1, yi+1))
			last = yi
		}
		wy := l.yWeight[y]
		row := y * c.surface.Width
		for x, lo := range l.row0 {
			col := lerpPacked(lo, l.row1[x], wy)
			a := col >> 24
			if a == 0 {
				continue
			}
			pi := row + x
			i := pi * 4
			if c.clip[pi] == 255 {
				inv := uint32(255) - a
				dst[i] = byte(uint32(byte(col)) + (uint32(dst[i])*inv+127)/255)
				dst[i+1] = byte(uint32(byte(col>>8)) + (uint32(dst[i+1])*inv+127)/255)
				dst[i+2] = byte(uint32(byte(col>>16)) + (uint32(dst[i+2])*inv+127)/255)
				if dst[i+3] != 255 {
					dst[i+3] = byte(a + (uint32(dst[i+3])*inv+127)/255)
				}
			} else {
				c.blend(x, y, col, 255)
			}
		}
	}
}

// LightStats are diagnostics, not timing claims or an OS memory measurement.
type LightStats struct {
	Width, Height, Step, ActiveCells, Bytes int
	Peak                                    float64
	Builds                                  uint64
}

func (c *Compositor) LightStats() LightStats {
	l := &c.light
	s := LightStats{Width: l.w, Height: l.h, Step: l.step, Builds: l.builds, Bytes: (cap(l.raw)+cap(l.near)+cap(l.far)+cap(l.scratch))*12 + (cap(l.packed)+cap(l.row0)+cap(l.row1)+cap(l.xWeight)+cap(l.yWeight))*4 + (cap(l.xIndex)+cap(l.yIndex))*8}
	for _, v := range l.raw {
		m := max(v.r, v.g, v.b)
		if m > 0 {
			s.ActiveCells++
		}
		s.Peak = max(s.Peak, float64(m)/255)
	}
	return s
}

// anti-aliased segment with bounded geometry. Tiny orbital lines are cheaper
// as segments than scanning an entire disc with sqrt/atan2 for every pixel.
func (c *Compositor) stroke(xa, ya, xb, yb, width float64, color uint32) {
	xa = (xa + 6) * c.scale
	ya = (ya + 6) * c.scale
	xb = (xb + 6) * c.scale
	yb = (yb + 6) * c.scale
	dx, dy := xb-xa, yb-ya
	d2 := dx*dx + dy*dy
	if d2 < 1e-10 {
		return
	}
	r := max(.4, width*c.scale*.5)
	col := premul(color)
	for y := max(0, int(min(ya, yb)-r-1)); y < min(c.surface.Height, int(math.Ceil(max(ya, yb)+r+1))); y++ {
		for x := max(0, int(min(xa, xb)-r-1)); x < min(c.surface.Width, int(math.Ceil(max(xa, xb)+r+1))); x++ {
			u := clamp01(((float64(x)+.5-xa)*dx + (float64(y)+.5-ya)*dy) / d2)
			vx, vy := float64(x)+.5-xa-u*dx, float64(y)+.5-ya-u*dy
			dist2 := vx*vx + vy*vy
			if dist2 >= (r+.5)*(r+.5) {
				continue
			}
			d := math.Sqrt(dist2)
			a := clamp01(r + .5 - d)
			if a > 0 {
				c.blend(x, y, col, uint32(a*255))
			}
		}
	}
}
func (c *Compositor) rings(rings []LightRing) {
	for _, r := range rings {
		if r.Opacity < .003 || r.Radius < 1 {
			continue
		}
		sn, cs := math.Sincos(r.Angle)
		at := func(angle, radius float64) (float64, float64) {
			s, v := math.Sincos(angle)
			x, y := v*radius, s*radius*r.Aspect
			return r.X + x*cs - y*sn, r.Y + x*sn + y*cs
		}
		phase := math.Mod(r.Phase, math.Pi*2)
		if phase < 0 {
			phase += math.Pi * 2
		}
		segments := 128
		if c.scene.Quality >= Economy {
			segments = 72
		}
		for i := 0; i < segments; i++ {
			u := float64(i) * 2 * math.Pi / float64(segments)
			un := float64(i+1) * 2 * math.Pi / float64(segments)
			x, y := at(u, r.Radius)
			xx, yy := at(un, r.Radius)
			d := math.Atan2(math.Sin(u-phase), math.Cos(u-phase))
			high := math.Exp(-d * d / .16)
			// A slightly shaded counter-edge provides contrast on the milk field.
			c.stroke(x, y+.85, xx, yy+.85, .58, Opacity(0xffada698, r.Opacity*.32))
			c.stroke(x, y, xx, yy, .72, Opacity(0xfffff8e9, r.Opacity*(.70+.30*high)))
			if c.LightEnabled && (high > .015 || i%4 == 0) {
				c.light.spot((x+6)*c.scale, (y+6)*c.scale, 2.5*c.scale, 0xffffedce, r.Opacity*(.25+3.3*high))
			}
			if i%4 == 0 {
				length := 1.5
				if i%16 == 0 {
					length = 3.5
				}
				x0, y0 := at(u, r.Radius+3)
				x1, y1 := at(u, r.Radius+3+length)
				c.stroke(x0, y0, x1, y1, .68, Opacity(0xfffdf9ef, r.Opacity*.88))
			}
			// Interrupted inner arc: an instrument ring, not a uniform bullseye.
			if i%32 < 19 && i%2 == 0 && c.scene.Quality <= Balanced {
				x0, y0 := at(u, r.Radius-4)
				x1, y1 := at(un+.009, r.Radius-4)
				c.stroke(x0, y0, x1, y1, .54, Opacity(0xffece1d1, r.Opacity*.59))
			}
		}
		x, y := at(phase, r.Radius)
		c.sprite(Sprite{X: x, Y: y, Radius: 6.1, Aspect: 1.2, Angle: r.Angle, Opacity: r.Opacity, Color: 0xfffff6e3, Kind: 4, Emission: 3.7})
	}
}
