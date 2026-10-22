package floatui

// SVG is a portable inspection/export backend, not the desktop host.
import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html"
	"image/png"
	"io"
	"strings"
)

func svgColor(c uint32) string { return fmt.Sprintf("#%06x", c&0xffffff) }
func svgPoints(points []Point) string {
	var b strings.Builder
	for _, p := range points {
		fmt.Fprintf(&b, "%.3f,%.3f ", p.X, p.Y)
	}
	return b.String()
}
func WriteSVG(w io.Writer, s Scene, scale float64) error {
	paint := NewPainter().Paint(s)
	m := NewCompositor().Render(s, paint, scale)
	var image bytes.Buffer
	if err := png.Encode(&image, m.Image()); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %.3f %.3f">`, m.Width, m.Height, s.Width+12, s.Height+12)
	fmt.Fprintf(&b, `<image width="%.3f" height="%.3f" href="data:image/png;base64,%s"/>`, s.Width+12, s.Height+12, base64.StdEncoding.EncodeToString(image.Bytes()))
	fmt.Fprintf(&b, `<defs><clipPath id="inside"><rect x="5" y="5" width="%.3f" height="%.3f" rx="14"/></clipPath><clipPath id="water"><polygon points="%s"/></clipPath></defs><g transform="translate(6 6)" clip-path="url(#inside)">`, s.Width-10, s.Height-10, svgPoints(s.Water[:s.WaterCount]))

	for i, l := range paint.Labels {
		fmt.Fprintf(&b, `<defs><clipPath id="t%d"><rect x="%.3f" y="%.3f" width="%.3f" height="%.3f"/></clipPath></defs><g clip-path="url(#t%d)">`, i, l.X, l.Y, l.W, l.H, i)
		if l.WaterClip {
			b.WriteString(`<g clip-path="url(#water)">`)
		}
		x, anchor := l.X+l.Size*.12, "start"
		if l.Right {
			x = l.X + l.W - l.Size*.12
			anchor = "end"
		}
		weight := 400
		if l.Bold {
			weight = 700
		}
		fmt.Fprintf(&b, `<text x="%.3f" y="%.3f" text-anchor="%s" fill="%s" fill-opacity="%.4f" font-family="'Segoe UI', 'Noto Sans', 'Arial', 'Noto Sans CJK SC', sans-serif" font-weight="%d" font-size="%.3f">%s</text>`, x, l.Y+l.Size*1.06, anchor, svgColor(l.Color), float64(l.Color>>24)/255, weight, l.Size, html.EscapeString(l.Text))
		if l.WaterClip {
			b.WriteString(`</g>`)
		}
		b.WriteString(`</g>`)
	}
	b.WriteString(`</g></svg>`)
	_, err := io.WriteString(w, b.String())
	return err
}
