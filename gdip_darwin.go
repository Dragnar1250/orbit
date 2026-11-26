//go:build darwin

package main

// The Mac side of Orbit's drawing functions: the same names and arguments as gdip_windows.go,
// drawn by Core Graphics (gdip_darwin.m). Handles are C pointers kept as uintptr, exactly like
// the GDI+ handles on Windows.

/*
#cgo CFLAGS: -fobjc-arc -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Cocoa -framework CoreText -framework ImageIO -framework Accelerate
#include <stdlib.h>
#include "gdip_darwin.h"
*/
import "C"

import (
	"fmt"
	"math"
	"unsafe"
)

const (
	interpBilinear           = 3
	interpHighQualityBicubic = 7
	textHintAntiAlias        = 4
	dashStyleDash            = 1
)

func gdipInit() bool { return true }
func formats()       {}

func og(g uintptr) *C.OG {
	if g == redirFrom && g != 0 { // between softBegin and softEnd: the soft layer
		g = redirTo
	}
	if onScreen[g] { // its last picture is being shown: draw the next one on a free surface
		delete(onScreen, g)
		C.og_surface_next((*C.OG)(unsafe.Pointer(g)))
	}
	return (*C.OG)(unsafe.Pointer(g))
}

// onScreen: surface canvases whose current surface was just handed to the window server
var onScreen = map[uintptr]bool{}

// newSurfaceCanvas: a widget's canvas, drawn straight into what the window server shows
func newSurfaceCanvas(w, h int) *Canvas {
	g := C.og_new_surface(C.int(max(w, 1)), C.int(max(h, 1)))
	if g == nil {
		return newCanvas(w, h)
	}
	return &Canvas{w: max(w, 1), h: max(h, 1), g: uintptr(unsafe.Pointer(g)), bmp: uintptr(unsafe.Pointer(C.og_img_of(g)))}
}
func opath(p uintptr) *C.OPath { return (*C.OPath)(unsafe.Pointer(p)) }
func open_(p uintptr) *C.OPen  { return (*C.OPen)(unsafe.Pointer(p)) }
func oimg(i uintptr) *C.OImg   { return (*C.OImg)(unsafe.Pointer(i)) }

func argb(a, r, g, b uint8) uint32 { return uint32(a)<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(b) }

func withAlpha(c uint32, a float64) uint32 {
	if a < 0 {
		a = 0
	}
	if a > 1 {
		a = 1
	}
	base := float64(c>>24) / 255
	return uint32(base*a*255)<<24 | (c & 0xFFFFFF)
}

/* ---------- Bitmap + Canvas ---------- */

// Bitmap: an off-screen picture (cached layers); bmp is its image handle, g its graphics.
type Bitmap struct {
	bmp  uintptr
	g    uintptr
	w, h int
}

func newBitmap(w, h int) *Bitmap {
	g := C.og_new(C.int(w), C.int(h))
	if g == nil {
		return nil
	}
	return &Bitmap{bmp: uintptr(unsafe.Pointer(C.og_img_of(g))), g: uintptr(unsafe.Pointer(g)), w: max(w, 1), h: max(h, 1)}
}

func (b *Bitmap) Free() {
	if b == nil {
		return
	}
	if b.bmp != 0 {
		C.og_img_free(oimg(b.bmp))
		b.bmp = 0
	}
	if b.g != 0 {
		C.og_free(og(b.g))
		b.g = 0
	}
}

// Canvas: a top-down premultiplied BGRA bitmap (bits) — the same layout as the Windows DIB.
type Canvas struct {
	w, h int
	bits uintptr
	bmp  uintptr
	g    uintptr
}

func newCanvas(w, h int) *Canvas {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	g := C.og_new(C.int(w), C.int(h))
	if g == nil {
		logf("canvas %dx%d failed", w, h)
		return nil
	}
	return &Canvas{w: w, h: h, g: uintptr(unsafe.Pointer(g)), bits: uintptr(C.og_bits(g)), bmp: uintptr(unsafe.Pointer(C.og_img_of(g)))}
}

func (c *Canvas) Free() {
	if c == nil {
		return
	}
	if c.bmp != 0 {
		C.og_img_free(oimg(c.bmp))
		c.bmp = 0
	}
	if c.g != 0 {
		delete(onScreen, c.g)
		C.og_free((*C.OG)(unsafe.Pointer(c.g)))
		c.g = 0
	}
}

/* ---------- state ---------- */

func gClear(g uintptr, c uint32) {
	C.og_clear(og(g), C.uint32_t(c))
	if redirFrom == 0 {
		softClear(g) // a cleared widget starts with an empty soft layer too
	}
}
func gTranslate(g uintptr, dx, dy float32) { C.og_translate(og(g), C.float(dx), C.float(dy)) }
func gRotate(g uintptr, deg float32)       { C.og_rotate(og(g), C.float(deg)) }
func gScale(g uintptr, sx, sy float32)     { C.og_scale(og(g), C.float(sx), C.float(sy)) }
func gReset(g uintptr)                     { C.og_reset(og(g)) }
func setTextHint(g uintptr, hint int)      {}
func setInterp(g uintptr, mode int)        { C.og_interp(og(g), C.int(mode)) }
func setCopyMode(g uintptr, copy bool)     { C.og_copy_mode(og(g), C.int(b2i(copy))) }
func clipPath(g, path uintptr)             { C.og_clip_path(og(g), opath(path)) }
func clipReset(g uintptr)                  { C.og_clip_reset(og(g)) }
func clipExclude(g, path uintptr)          { C.og_clip_exclude(og(g), opath(path)) }
func clipRect(g uintptr, x, y, w, h float32) {
	C.og_clip_rect(og(g), C.float(x), C.float(y), C.float(w), C.float(h))
}

func clipCircle(g uintptr, cx, cy, r float32) uintptr {
	p := newPath()
	addEllipse(p, cx-r, cy-r, 2*r, 2*r)
	clipPath(g, p)
	return p
}

/* ---------- images ---------- */

func gImage(g, img uintptr, x, y, w, h float32) {
	if img == 0 {
		return
	}
	C.og_draw_image(og(g), oimg(img), C.float(x), C.float(y), C.float(w), C.float(h), 0, 0, 0, 0, 0)
}

func gImageAlpha(g, img uintptr, iw, ih int, x, y, w, h float32, alpha float64) {
	if img == 0 || alpha <= 0.004 {
		return
	}
	C.og_draw_image_alpha(og(g), oimg(img), C.float(x), C.float(y), C.float(w), C.float(h), C.float(math.Min(1, alpha)))
}

func gImageSrc(g, img uintptr, x, y, w, h, sx, sy, sw, sh float32) {
	if img == 0 {
		return
	}
	C.og_draw_image(og(g), oimg(img), C.float(x), C.float(y), C.float(w), C.float(h), C.float(sx), C.float(sy), C.float(sw), C.float(sh), 1)
}

func imageFromBytes(b []byte) uintptr {
	if len(b) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(C.og_img_from_bytes(unsafe.Pointer(&b[0]), C.int(len(b)))))
}

func imageFromFile(path string) uintptr {
	cs := C.CString(path)
	defer C.free(unsafe.Pointer(cs))
	return uintptr(unsafe.Pointer(C.og_img_from_file(cs)))
}

func savePNG(img uintptr, path string) bool {
	cs := C.CString(path)
	defer C.free(unsafe.Pointer(cs))
	return C.og_save_png(oimg(img), cs) != 0
}

func imageSize(img uintptr) (int, int) {
	if img == 0 {
		return 0, 0
	}
	var w, h C.int
	C.og_img_size(oimg(img), &w, &h)
	return int(w), int(h)
}

func freeImage(img uintptr) {
	if img != 0 {
		C.og_img_free(oimg(img))
	}
}

// pixel: the colour at (x, y) of a bitmap, as straight (not premultiplied) ARGB like GDI+ gives.
func pixel(bmp uintptr, x, y int) uint32 {
	var w, h C.int
	C.og_img_size(oimg(bmp), &w, &h)
	if x < 0 || y < 0 || x >= int(w) || y >= int(h) {
		return 0
	}
	// bitmaps made by newBitmap: read the shared pixels
	bits := C.og_bits(C.og_img_og(oimg(bmp)))
	if bits == nil {
		return 0
	}
	px := unsafe.Slice((*uint32)(bits), int(w)*int(h))[y*int(w)+x]
	a := px >> 24
	if a == 0 {
		return 0
	}
	un := func(c uint32) uint32 { return min(255, c*255/a) }
	return a<<24 | un((px>>16)&255)<<16 | un((px>>8)&255)<<8 | un(px&255)
}

/* ---------- brushes and pens ---------- */

func trimBrushes() {}

func newPen(c uint32, width float32) uintptr {
	return uintptr(unsafe.Pointer(C.og_pen(C.uint32_t(c), C.float(width))))
}
func freePen(p uintptr) { C.og_pen_free(open_(p)) }
func penDash(p uintptr) { C.og_pen_dash(open_(p)) }

func fillEllipse(g uintptr, c uint32, x, y, w, h float32) {
	C.og_fill_ellipse(og(g), C.uint32_t(c), C.float(x), C.float(y), C.float(w), C.float(h))
}
func fillCircle(g uintptr, c uint32, cx, cy, r float32) { fillEllipse(g, c, cx-r, cy-r, 2*r, 2*r) }

func strokeCircle(g uintptr, c uint32, cx, cy, r, width float32) {
	p := newPen(c, width)
	C.og_stroke_ellipse(og(g), open_(p), C.float(cx-r), C.float(cy-r), C.float(2*r), C.float(2*r))
	freePen(p)
}

func line(g uintptr, c uint32, width, x1, y1, x2, y2 float32) {
	p := newPen(c, width)
	drawLinePen(g, p, x1, y1, x2, y2)
	freePen(p)
}

func drawLinePen(g, pen uintptr, x1, y1, x2, y2 float32) {
	C.og_line(og(g), open_(pen), C.float(x1), C.float(y1), C.float(x2), C.float(y2))
}

func drawArc(g, pen uintptr, x, y, w, h, start, sweep float32) {
	C.og_arc(og(g), open_(pen), C.float(x), C.float(y), C.float(w), C.float(h), C.float(start), C.float(sweep))
}

func fillRect(g uintptr, c uint32, x, y, w, h float32) {
	C.og_fill_rect(og(g), C.uint32_t(c), C.float(x), C.float(y), C.float(w), C.float(h))
}

func fillPie(g uintptr, c uint32, x, y, w, h, start, sweep float32) {
	C.og_fill_pie(og(g), C.uint32_t(c), C.float(x), C.float(y), C.float(w), C.float(h), C.float(start), C.float(sweep))
}

func fillPoly(g uintptr, c uint32, pts []PointF) {
	if len(pts) < 3 {
		return
	}
	xy := make([]C.float, 0, 2*len(pts))
	for _, p := range pts {
		xy = append(xy, C.float(p.X), C.float(p.Y))
	}
	C.og_fill_poly(og(g), C.uint32_t(c), &xy[0], C.int(len(pts)))
}

/* ---------- paths ---------- */

func newPath() uintptr      { return uintptr(unsafe.Pointer(C.og_path())) }
func freePath(p uintptr)    { C.og_path_free(opath(p)) }
func pathWinding(p uintptr) { C.og_path_winding(opath(p)) }
func addPath(p, o uintptr)  { C.og_path_add(opath(p), opath(o)) }
func addEllipse(p uintptr, x, y, w, h float32) {
	C.og_path_ellipse(opath(p), C.float(x), C.float(y), C.float(w), C.float(h))
}

func roundRectPath(x, y, w, h, r float32) uintptr {
	p := newPath()
	C.og_path_round_rect(opath(p), C.float(x), C.float(y), C.float(w), C.float(h), C.float(r))
	return p
}

func pathBounds(path uintptr) RectF {
	var b [4]C.float
	C.og_path_bounds(opath(path), &b[0])
	return RectF{float32(b[0]), float32(b[1]), float32(b[2]), float32(b[3])}
}

func fillPathColor(g, path uintptr, c uint32) { C.og_fill_path(og(g), opath(path), C.uint32_t(c)) }
func drawPathPen(g, pen, path uintptr)        { C.og_stroke_path(og(g), opath(path), open_(pen)) }

// fillPathGlow: the path with a blurred glow of colour glow around it (Mac only)
func fillPathGlow(g, path uintptr, fill, glow uint32, blur float32) {
	C.og_fill_path_glow(og(g), opath(path), C.uint32_t(fill), C.uint32_t(glow), C.float(blur))
}

func strokePath(g, path uintptr, c uint32, width float32) {
	p := newPen(c, width)
	drawPathPen(g, p, path)
	freePen(p)
}

func fillPathGradient(g, path uintptr, top, bottom uint32, y0, y1 float32) {
	if y1 <= y0 {
		y1 = y0 + 1
	}
	C.og_fill_path_vgrad(og(g), opath(path), C.uint32_t(top), C.uint32_t(bottom), C.float(y0), C.float(y1))
}

func fillRectGrad(g uintptr, x, y, w, h, x0, y0, x1, y1 float32, c0, c1 uint32) {
	if w <= 0 || h <= 0 {
		return
	}
	if x0 == x1 && y0 == y1 {
		y1++
	}
	C.og_fill_rect_grad(og(g), C.float(x), C.float(y), C.float(w), C.float(h), C.float(x0), C.float(y0), C.float(x1), C.float(y1), C.uint32_t(c0), C.uint32_t(c1))
}

// softGlow: a disc fading to transparent at radius r.
func softGlow(g uintptr, cx, cy, r float32, center uint32) {
	C.og_radial(og(g), C.float(cx), C.float(cy), C.float(r), C.float(r), C.uint32_t(center), C.uint32_t(center&0x00FFFFFF))
}

// radialFill: an ellipse that fades from center to surround.
func radialFill(g uintptr, cx, cy, rx, ry float32, center, surround uint32) {
	C.og_radial(og(g), C.float(cx), C.float(cy), C.float(rx), C.float(ry), C.uint32_t(center), C.uint32_t(surround))
}

// drawGlowPath fills a path with an optional soft glow + drop shadow (as on Windows). With a soft
// layer the glow is one blurred fill there instead of three outlines of every letter here.
func drawGlowPath(g, path uintptr, fill uint32, glow uint32, glowAmt float32, shadow bool) {
	if (shadow || glowAmt > 0) && softBegin(g) {
		if shadow {
			gTranslate(g, 0, 2)
			fillPathColor(g, path, argb(uint8(float64(fill>>24)*0.45), 0, 0, 0))
			gTranslate(g, 0, -2)
		}
		if glowAmt > 0 { // the same three faint outlines as on Windows (a neon edge round the letters,
			// not a cloud over the whole word), only softened a little by the blur
			for i, wmul := range []float32{1.0, 0.6, 0.3} {
				a := float64(glow>>24) / 255 * []float64{0.10, 0.16, 0.24}[i]
				p := newPen(withAlpha(glow|0xFF000000, a), glowAmt*wmul)
				drawPathPen(g, p, path)
				freePen(p)
			}
		}
		softEnd(g)
		softBlur(g, float32(math.Max(1.5, float64(glowAmt)*0.12)))
		fillPathColor(g, path, fill)
		return
	}
	if shadow {
		gTranslate(g, 0, 2)
		fillPathColor(g, path, argb(uint8(float64(fill>>24)*0.45), 0, 0, 0))
		gTranslate(g, 0, -2)
	}
	if glowAmt > 0 { // a soft halo hugging the letters: Core Graphics' blurred shadow
		a := float64(glow>>24) / 255
		r := float32(math.Min(14, float64(glowAmt)*0.8+1))
		if !glowSprite(g, path, glow, r, a*0.6) {
			fillPathGlow(g, path, fill, withAlpha(glow|0xFF000000, a*0.6), r)
			return
		}
		fillPathColor(g, path, fill)
		return
	}
	fillPathColor(g, path, fill)
}

/* ---------- fonts and text ---------- */

type Font struct {
	f    *C.OFont
	size float32
	key  string // name|size|bold — for the measure cache
}

func newFont(name string, size float32, bold bool) *Font {
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	return &Font{f: C.og_font(cs, C.float(size), C.int(b2i(bold))), size: size, key: fmt.Sprintf("%s|%g|%v", name, size, bold)}
}

// measured: widths already worked out (the lyrics measure every word every frame; Core Text
// lays out a whole line for each)
type measureKey struct{ font, s string }

var measured = map[measureKey][2]float32{}

func (f *Font) Free() {
	if f != nil && f.f != nil {
		C.og_font_free(f.f)
		f.f = nil
	}
}

func cstr(s string) (*C.char, C.int) {
	if s == "" {
		return nil, 0
	}
	b := []byte(s)
	return (*C.char)(C.CBytes(b)), C.int(len(b))
}

func measure(f *Font, s string) (float32, float32) {
	if s == "" {
		return 0, f.size
	}
	k := measureKey{f.key, s}
	if r, ok := measured[k]; ok {
		return r[0], r[1]
	}
	p, n := cstr(s)
	defer C.free(unsafe.Pointer(p))
	var w, h C.float
	C.og_measure(f.f, p, n, &w, &h)
	if len(measured) > 4096 {
		clear(measured)
	}
	measured[k] = [2]float32{float32(w), float32(h)}
	return float32(w), float32(h)
}

func drawText(g uintptr, f *Font, s string, x, y, maxW float32, c uint32) {
	if s == "" {
		return
	}
	p, n := cstr(s)
	defer C.free(unsafe.Pointer(p))
	C.og_text(og(g), f.f, p, n, C.float(x), C.float(y), C.float(maxW), C.uint32_t(c), 0)
}

func drawTextRight(g uintptr, f *Font, s string, xRight, y, maxW float32, c uint32) {
	if s == "" {
		return
	}
	p, n := cstr(s)
	defer C.free(unsafe.Pointer(p))
	C.og_text(og(g), f.f, p, n, C.float(xRight-maxW), C.float(y), C.float(maxW), C.uint32_t(c), 2)
}

func addTextPath(path uintptr, f *Font, s string, x, y float32) {
	if s == "" {
		return
	}
	p, n := cstr(s)
	defer C.free(unsafe.Pointer(p))
	C.og_text_path(opath(path), f.f, p, n, C.float(x), C.float(y))
}

// On the Mac the glows stay a glow, not a cloud (1.24.6): the beat's Glow motion makes them
// brighter more than wider, a strong setting (up to 200 %) widens them only so far, and full
// screen's bloom is gentler — with Retina's sharp, bright text they read as puffy white blobs.
const (
	glowBeatK   = 0.55
	glowBeatAdd = 0.35
	glowMaxAmt  = 1.6
	bloomTone   = 0.6
)

// strongGlowSoft: Strong glow on the Mac — one wide outline and the letters themselves laid on the
// soft layer and blurred: a smooth halo. (Four outlines one on another showed as hard grey rings.)
func strongGlowSoft(g, path uintptr, col uint32, size, amt float32, a float64) bool {
	// Core Graphics' own blurred shadow, in the letters' colour: a smooth halo (four outlines one
	// on another showed as hard grey rings). A wide faint one, then a close brighter one.
	wide := float32(math.Min(22, float64(size*0.5*amt)+2)) // (the blur's cost grows with its radius)
	near := float32(math.Min(12, float64(size*0.18*amt)+1.5))
	if glowSprite(g, path, col, wide, a*0.55*math.Min(1.2, float64(amt))) && glowSprite(g, path, col, near, a*0.75) {
		fillPathColor(g, path, col)
		return true
	}
	fillPathGlow(g, path, col, withAlpha(col|0xFF000000, a*0.55*math.Min(1.2, float64(amt))), wide)
	fillPathGlow(g, path, col, withAlpha(col|0xFF000000, a*0.75), near)
	return true
}

// Glow sprites: the blur of a line's glow is made once — a picture of only the glow, kept with
// the line's text path — and then only laid down each frame at the strength wanted (the beat's
// Glow changes that every frame). Blurring every line every frame was a third of the widgets'
// time on the Mac. Kept for the text paths in the cache, at most ~24 MB.
type glowSpriteKey struct {
	path uintptr
	rgb  uint32
	r2   int32 // the blur radius in half pixels
}

type glowSpriteEntry struct {
	b    *Bitmap
	x, y float32 // where its top-left goes, in the path's space
}

var (
	glowSprites     = map[glowSpriteKey]*glowSpriteEntry{}
	glowSpriteBytes int
)

func flushGlowSprites() {
	for _, e := range glowSprites {
		e.b.Free()
	}
	glowSprites = map[glowSpriteKey]*glowSpriteEntry{}
	glowSpriteBytes = 0
}

// glowSprite lays the glow of path (colour c, blur r) at alpha; false when it can't be kept
// (not a cached text path, or too big) — then the caller blurs it directly.
func glowSprite(g, path uintptr, c uint32, r float32, alpha float64) bool {
	if !textPathSet[path] || r <= 0 {
		return false
	}
	k := glowSpriteKey{path, c & 0xFFFFFF, int32(math.Round(float64(r) * 2))}
	e := glowSprites[k]
	if e == nil {
		b := pathBounds(path)
		m := float32(math.Ceil(float64(r)*2.2)) + 2
		w, h := int(math.Ceil(float64(b.W+2*m))), int(math.Ceil(float64(b.H+2*m)))
		if w <= 0 || h <= 0 || w*h > 1500*400 {
			return false
		}
		if glowSpriteBytes+w*h*4 > 24<<20 {
			flushGlowSprites()
		}
		bm := newBitmap(w, h)
		if bm == nil {
			return false
		}
		gTranslate(bm.g, m-b.X, m-b.Y)
		C.og_shadow_only(og(bm.g), opath(path), C.uint32_t(c), C.float(r))
		gReset(bm.g)
		e = &glowSpriteEntry{bm, b.X - m, b.Y - m}
		glowSprites[k] = e
		glowSpriteBytes += w * h * 4
	}
	gImageAlpha(g, e.b.bmp, e.b.w, e.b.h, e.x, e.y, float32(e.b.w), float32(e.b.h), alpha)
	return true
}
