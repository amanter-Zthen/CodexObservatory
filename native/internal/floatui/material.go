package floatui

// The nacre shell is our own procedural artwork. No PV pixels, 3-D assets,
// font files or external shaders are used. The native host needs no web engine.
// This buffer is rebuilt only
// when its pixel size changes, never during steady-state animation.
import (
	"image"
	"math"
	"sync"
)

const FrameMargin = 6.0
const FrameRadius = 19.0

type Material struct {
	Width, Height int
	BGRA          []byte
}
type pigment struct{ r, g, b float64 }

func blendPigment(a, b pigment, t float64) pigment {
	return pigment{a.r + (b.r-a.r)*t, a.g + (b.g-a.g)*t, a.b + (b.b-a.b)*t}
}
func clamp01(x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	return x
}
func patch(x, y, cx, cy, rx, ry float64) float64 {
	dx, dy := (x-cx)/rx, (y-cy)/ry
	v := math.Max(0, 1-dx*dx-dy*dy)
	return v * v
}

// A fixed normalised pigment field decouples expensive colour construction
// from frame dimensions. The rim is still evaluated in physical DIP units.
var pigmentOnce sync.Once
var pigmentField [257][401]uint32

func bodyPigment(ux, uy float64) pigment {
	// Luminance headroom is intentional: glowing white cannot read on clipped
	// white. Milk glass under a broad neutral studio light; warmth lives in reflected
	// light, not in yellow/green blocks of pigment.
	col := pigment{229, 227, 222}
	col = blendPigment(col, pigment{195, 187, 174}, .48*patch(ux, uy, .16, .94, .64, .86))
	col = blendPigment(col, pigment{196, 202, 205}, .40*patch(ux, uy, .91, .90, .56, .76))
	col = blendPigment(col, pigment{250, 249, 245}, .52*patch(ux, uy, .48, .24, .80, .70))
	streak := clamp01(1 - math.Abs(uy-(.86-.54*ux))/.25)
	return blendPigment(col, pigment{248, 245, 237}, .21*streak*streak)
}
func roundDistance(x, y, w, h, r float64) float64 {
	cx, cy := math.Min(x, w-x), math.Min(y, h-y)
	if cx >= r || cy >= r {
		return -math.Min(cx, cy)
	}
	return math.Hypot(r-cx, r-cy) - r
}
func MaterialFor(width, height, scale float64) Material {
	return MaterialInto(Material{}, width, height, scale)
}

// MaterialInto reuses its backing store during resize; callers own that store.
func MaterialInto(m Material, width, height, scale float64) Material {
	pigmentOnce.Do(func() {
		for y := range pigmentField {
			for x := range pigmentField[y] {
				c := bodyPigment(float64(x)/400, float64(y)/256)
				// Fixed fine dithering avoids broad 8-bit gradient contour bands.
				hash := uint32(x)*374761393 + uint32(y)*668265263
				hash = (hash ^ (hash >> 13)) * 1274126177
				n := (float64(hash&255)/255 - .5) * .85
				pigmentField[y][x] = uint32(c.b+n+.5) | uint32(c.g+n+.5)<<8 | uint32(c.r+n+.5)<<16 | 0xff000000
			}
		}
	})
	pw, ph := int(math.Ceil((width+FrameMargin*2)*scale)), int(math.Ceil((height+FrameMargin*2)*scale))
	size := pw * ph * 4
	if cap(m.BGRA) < size {
		m.BGRA = make([]byte, size, max(size, 2*cap(m.BGRA)))
	} else {
		m.BGRA = m.BGRA[:size]
	}
	m.Width, m.Height = pw, ph
	for py := 0; py < ph; py++ {
		for px := 0; px < pw; px++ {
			x, y := (float64(px)+.5)/scale-FrameMargin, (float64(py)+.5)/scale-FrameMargin
			d := roundDistance(x, y, width, height, FrameRadius)
			ux, uy := x/width, y/height
			if d < -1.65 {
				// Opaque body: cached field lookup, no transcendental colour work.
				v := pigmentField[int(uy*256)][int(ux*400)]
				i := (py*pw + px) * 4
				m.BGRA[i], m.BGRA[i+1], m.BGRA[i+2], m.BGRA[i+3] = byte(v), byte(v>>8), byte(v>>16), 255
				continue
			}
			col := pigment{106, 103, 98}
			coverage := 0.0
			if d > 0 {
				sd := roundDistance(x-.1, y-1.2, width, height, FrameRadius)
				v := clamp01(1 - math.Max(sd, 0)/4.2)
				coverage = .115 * v * v * v
			} else {
				coverage = clamp01(-d*scale + .5)
				depth := -d
				col = bodyPigment(clamp01(ux), clamp01(uy))
				light := .5 + .5*math.Cos((ux*.32+uy)*math.Pi)
				// One thin translucent lip, not a stack of metallic bevels.
				if depth < .55 {
					col = blendPigment(pigment{202, 200, 194}, pigment{237, 236, 231}, light)
				} else if depth < 1.1 {
					col = blendPigment(col, pigment{255, 255, 254}, .78)
				} else if depth < 1.65 {
					col = blendPigment(col, pigment{232, 231, 225}, .16*(1-(depth-1.1)/.55))
				}
			}
			i := (py*pw + px) * 4
			a := math.Round(255 * coverage)
			m.BGRA[i] = byte(math.Round(math.Max(0, math.Min(255, col.b)) * a / 255))
			m.BGRA[i+1] = byte(math.Round(math.Max(0, math.Min(255, col.g)) * a / 255))
			m.BGRA[i+2] = byte(math.Round(math.Max(0, math.Min(255, col.r)) * a / 255))
			m.BGRA[i+3] = byte(a)
		}
	}
	return m
}
func (m Material) Image() *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, m.Width, m.Height))
	for i := 0; i < len(m.BGRA); i += 4 {
		out.Pix[i] = m.BGRA[i+2]
		out.Pix[i+1] = m.BGRA[i+1]
		out.Pix[i+2] = m.BGRA[i]
		out.Pix[i+3] = m.BGRA[i+3]
	}
	return out
}
