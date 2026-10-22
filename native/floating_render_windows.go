//go:build windows

package main

import (
	"codex-observatory/internal/floatui"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"syscall"
	"unsafe"
)

type gpStartup struct {
	Version                       uint32
	Callback                      uintptr
	SuppressThread, SuppressCodec int32
}
type bitmapInfo struct {
	Size                   uint32
	Width, Height          int32
	Planes, Bits           uint16
	Compression, ImageSize uint32
	XPels, YPels           int32
	Used, Important        uint32
}
type gpRect struct{ X, Y, Width, Height float32 }
type glyphKey struct {
	text string
	bold bool
}
type glyphEntry struct {
	mask floatui.GlyphMask
	used uint64
}

const glyphCacheLimit = 4 * 1024 * 1024
const glyphScratchWidth = 1024
const glyphScratchHeight = 88

type floatRenderer struct {
	painter                                                                       *floatui.Painter
	compositor                                                                    *floatui.Compositor
	material                                                                      floatui.Material
	procs                                                                         map[string]*syscall.LazyProc
	gdi                                                                           *syscall.LazyDLL
	token, dc, dib, oldDIB                                                        uintptr
	bits                                                                          unsafe.Pointer
	width, height                                                                 int
	scale                                                                         float64
	maskDC, maskDIB, maskOld, maskBitmap, maskGraphics, maskBrush, family, format uintptr
	maskFonts                                                                     [2]uintptr
	maskBits                                                                      unsafe.Pointer
	glyphs                                                                        map[glyphKey]*glyphEntry
	glyphBytes                                                                    int
	glyphClock                                                                    uint64
	GlyphBuilds                                                                   uint64
}

func real(v float64) uintptr { return uintptr(math.Float32bits(float32(v))) }

//go:uintptrescapes
func (r *floatRenderer) call(name string, args ...uintptr) uintptr {
	value, _, _ := r.procs[name].Call(args...)
	return value
}
func (r *floatRenderer) makeDIB(w, h int) (dc, dib, old uintptr, bits unsafe.Pointer, err error) {
	dc, _, _ = r.gdi.NewProc("CreateCompatibleDC").Call(0)
	info := bitmapInfo{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, Bits: 32}
	dib, _, _ = r.gdi.NewProc("CreateDIBSection").Call(dc, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if dc == 0 || dib == 0 {
		err = fmt.Errorf("native bitmap allocation failed")
		return
	}
	old, _, _ = r.gdi.NewProc("SelectObject").Call(dc, dib)
	return
}
func newFloatRenderer(scale, maxHeight float64) (r *floatRenderer, err error) {
	scale = math.Max(.75, math.Min(4, scale))
	maxHeight = math.Min(floatui.MaxPanelHeight, math.Max(76, maxHeight))
	r = &floatRenderer{procs: make(map[string]*syscall.LazyProc), gdi: syscall.NewLazyDLL("gdi32.dll"), scale: scale, painter: floatui.NewPainter(), compositor: floatui.NewCompositor(), glyphs: make(map[glyphKey]*glyphEntry)}
	dll := syscall.NewLazyDLL("gdiplus.dll")
	for _, name := range []string{"GdiplusStartup", "GdiplusShutdown", "GdipCreateBitmapFromScan0", "GdipGetImageGraphicsContext", "GdipDisposeImage", "GdipDeleteGraphics", "GdipSetTextRenderingHint", "GdipCreateSolidFill", "GdipDeleteBrush", "GdipCreateFontFamilyFromName", "GdipDeleteFontFamily", "GdipCreateFont", "GdipDeleteFont", "GdipCreateStringFormat", "GdipSetStringFormatTrimming", "GdipDeleteStringFormat", "GdipDrawString", "GdipMeasureString", "GdipGraphicsClear", "GdipFlush"} {
		proc := dll.NewProc(name)
		if err = proc.Find(); err != nil {
			return nil, err
		}
		r.procs[name] = proc
	}
	input := gpStartup{Version: 1}
	if code := r.call("GdiplusStartup", uintptr(unsafe.Pointer(&r.token)), uintptr(unsafe.Pointer(&input)), 0); code != 0 {
		return nil, fmt.Errorf("GDI+ startup %d", code)
	}
	defer func() {
		if err != nil {
			r.close()
		}
	}()
	r.width = int(math.Ceil((floatui.PanelWidth + 12) * scale))
	r.height = int(math.Ceil((maxHeight + 12) * scale))
	r.dc, r.dib, r.oldDIB, r.bits, err = r.makeDIB(r.width, r.height)
	if err != nil {
		return
	}
	r.maskDC, r.maskDIB, r.maskOld, r.maskBits, err = r.makeDIB(glyphScratchWidth, glyphScratchHeight)
	if err != nil {
		return
	}
	if code := r.call("GdipCreateBitmapFromScan0", glyphScratchWidth, glyphScratchHeight, glyphScratchWidth*4, 0xe200b, uintptr(r.maskBits), uintptr(unsafe.Pointer(&r.maskBitmap))); code != 0 {
		err = fmt.Errorf("glyph bitmap %d", code)
		return
	}
	if code := r.call("GdipGetImageGraphicsContext", r.maskBitmap, uintptr(unsafe.Pointer(&r.maskGraphics))); code != 0 {
		err = fmt.Errorf("glyph graphics %d", code)
		return
	}
	r.call("GdipSetTextRenderingHint", r.maskGraphics, 4)
	if code := r.call("GdipCreateSolidFill", 0xffffffff, uintptr(unsafe.Pointer(&r.maskBrush))); code != 0 {
		err = fmt.Errorf("glyph brush %d", code)
		return
	}
	if code := r.call("GdipCreateFontFamilyFromName", uintptr(unsafe.Pointer(u16("Segoe UI"))), 0, uintptr(unsafe.Pointer(&r.family))); code != 0 {
		err = fmt.Errorf("font family %d", code)
		return
	}
	for i := range r.maskFonts {
		if code := r.call("GdipCreateFont", r.family, real(floatui.GlyphReferenceSize), uintptr(i), 2, uintptr(unsafe.Pointer(&r.maskFonts[i]))); code != 0 {
			err = fmt.Errorf("glyph font %d", code)
			return
		}
	}
	if code := r.call("GdipCreateStringFormat", 0x1000, 0, uintptr(unsafe.Pointer(&r.format))); code != 0 {
		err = fmt.Errorf("glyph format %d", code)
		return
	}
	r.call("GdipSetStringFormatTrimming", r.format, 3)
	return
}
func (r *floatRenderer) close() {
	for _, font := range r.maskFonts {
		if font != 0 {
			r.call("GdipDeleteFont", font)
		}
	}
	for _, item := range []struct {
		name string
		h    uintptr
	}{{"GdipDeleteStringFormat", r.format}, {"GdipDeleteFontFamily", r.family}, {"GdipDeleteBrush", r.maskBrush}, {"GdipDeleteGraphics", r.maskGraphics}, {"GdipDisposeImage", r.maskBitmap}} {
		if item.h != 0 {
			r.call(item.name, item.h)
		}
	}
	for _, v := range [][3]uintptr{{r.maskDC, r.maskDIB, r.maskOld}, {r.dc, r.dib, r.oldDIB}} {
		if v[0] != 0 {
			if v[2] != 0 {
				r.gdi.NewProc("SelectObject").Call(v[0], v[2])
			}
			if v[1] != 0 {
				r.gdi.NewProc("DeleteObject").Call(v[1])
			}
			r.gdi.NewProc("DeleteDC").Call(v[0])
		}
	}
	if r.token != 0 {
		r.call("GdiplusShutdown", r.token)
	}
	clear(r.glyphs)
}
func (r *floatRenderer) glyph(l floatui.PaintLabel) (floatui.GlyphMask, bool) {
	r.glyphClock++
	key := glyphKey{l.Text, l.Bold}
	if g := r.glyphs[key]; g != nil {
		g.used = r.glyphClock
		return g.mask, true
	}
	text, err := syscall.UTF16FromString(l.Text)
	if err != nil {
		return floatui.GlyphMask{}, false
	}
	font := r.maskFonts[0]
	if l.Bold {
		font = r.maskFonts[1]
	}
	area := gpRect{X: float32(floatui.GlyphPadding), Y: float32(floatui.GlyphPadding), Width: glyphScratchWidth - 2*float32(floatui.GlyphPadding), Height: glyphScratchHeight - 2*float32(floatui.GlyphPadding)}
	var bounds gpRect
	if r.call("GdipMeasureString", r.maskGraphics, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)-1), font, uintptr(unsafe.Pointer(&area)), r.format, uintptr(unsafe.Pointer(&bounds)), 0, 0) != 0 {
		return floatui.GlyphMask{}, false
	}
	r.call("GdipGraphicsClear", r.maskGraphics, 0)
	if r.call("GdipDrawString", r.maskGraphics, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)-1), font, uintptr(unsafe.Pointer(&area)), r.format, r.maskBrush) != 0 {
		return floatui.GlyphMask{}, false
	}
	r.call("GdipFlush", r.maskGraphics, 1)
	w := min(glyphScratchWidth, max(1, int(math.Ceil(float64(bounds.Width)))+int(2*floatui.GlyphPadding)))
	h := min(glyphScratchHeight, max(1, int(math.Ceil(float64(bounds.Height)))+int(2*floatui.GlyphPadding)))
	alpha := make([]byte, w*h)
	pixels := unsafe.Slice((*byte)(r.maskBits), glyphScratchWidth*glyphScratchHeight*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			alpha[y*w+x] = pixels[(y*glyphScratchWidth+x)*4+3]
		}
	}
	mask := floatui.MakeGlyphMask(w, h, float64(bounds.Width), alpha)
	size := mask.Bytes()
	for len(r.glyphs) > 0 && (len(r.glyphs) >= 128 || r.glyphBytes+size > glyphCacheLimit) {
		var oldest glyphKey
		age := ^uint64(0)
		for k, g := range r.glyphs {
			if g.used < age {
				oldest, age = k, g.used
			}
		}
		e := r.glyphs[oldest]
		r.glyphBytes -= e.mask.Bytes()
		delete(r.glyphs, oldest)
	}
	r.glyphs[key] = &glyphEntry{mask: mask, used: r.glyphClock}
	r.glyphBytes += size
	r.GlyphBuilds++
	return mask, true
}
func (r *floatRenderer) draw(scene floatui.Scene) {
	painting := r.painter.Paint(scene)
	r.material = r.compositor.Render(scene, painting, r.scale)
	for _, label := range painting.Labels {
		if glyph, ok := r.glyph(label); ok {
			r.compositor.DrawGlyph(label, glyph)
		}
	}
	pixels := unsafe.Slice((*byte)(r.bits), r.width*r.height*4)
	rows := min(r.material.Height, r.height)
	for y := 0; y < rows; y++ {
		copy(pixels[y*r.width*4:(y+1)*r.width*4], r.material.BGRA[y*r.material.Width*4:(y+1)*r.material.Width*4])
	}
}
func (r *floatRenderer) present(hwnd uintptr, scene floatui.Scene) bool {
	size := point{int32(math.Ceil((scene.Width + 12) * r.scale)), int32(math.Ceil((scene.Height + 12) * r.scale))}
	zero := point{}
	blend := [4]byte{0, 0, 255, 1}
	result, _, _ := user.NewProc("UpdateLayeredWindow").Call(hwnd, 0, 0, uintptr(unsafe.Pointer(&size)), r.dc, uintptr(unsafe.Pointer(&zero)), 0, uintptr(unsafe.Pointer(&blend)), 2)
	return result != 0
}
func (r *floatRenderer) savePNG(path string, scene floatui.Scene) error {
	r.draw(scene)
	out := image.NewRGBA(image.Rect(0, 0, r.material.Width, r.material.Height))
	for i := 0; i < len(out.Pix); i += 4 {
		out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = r.material.BGRA[i+2], r.material.BGRA[i+1], r.material.BGRA[i], r.material.BGRA[i+3]
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, out)
}
