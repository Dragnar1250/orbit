//go:build darwin

package main

// The drawing helpers the 1.20+ views use, on the Mac (gdip_windows.go / gdiphelp_windows.go have
// the Windows ones). Canvases and bitmaps here are plain top-down premultiplied BGRA memory
// (the same layout as the Windows DIB), so the Go pixel work of cinema.go runs on them as is.

/*
#include <stdlib.h>
#include "gdip_darwin.h"
*/
import "C"

import (
	"fmt"
	"math"
	"unsafe"
)

const pixelOffsetHalf = 2

func gFlush(g uintptr)                   {} // Core Graphics draws at once
func gdiFlush()                          {}
func setPixelOffset(g uintptr, mode int) {}
func closeFigure(p uintptr)              { C.og_path_close(opath(p)) }
func drawEllipsePen(g, pen uintptr, x, y, w, h float32) {
	C.og_stroke_ellipse(og(g), open_(pen), C.float(x), C.float(y), C.float(w), C.float(h))
}

// pix: a canvas' pixels as a Go slice (its memory itself).
func pix(c *Canvas) []uint32 {
	if c == nil || c.g == 0 {
		return nil
	}
	b := C.og_bits(og(c.g))
	if b == nil {
		return nil
	}
	return unsafe.Slice((*uint32)(b), c.w*c.h)
}

// pixels: a bitmap's premultiplied BGRA pixels (its memory itself).
func (b *Bitmap) pixels() []uint32 {
	if b == nil || b.g == 0 {
		return nil
	}
	p := C.og_bits(og(b.g))
	if p == nil {
		return nil
	}
	return unsafe.Slice((*uint32)(p), b.w*b.h)
}

// edit: change its pixels in Go.
func (b *Bitmap) edit(f func(px []uint32, w, h int)) {
	if px := b.pixels(); px != nil {
		f(px, b.w, b.h)
	}
}

// clearCanvas empties the widget's picture with a plain memory clear.
func (w *Widget) clearCanvas() {
	if w.canvas == nil {
		return
	}
	if px := pix(w.canvas); px != nil {
		clear(px)
		return
	}
	gClear(w.canvas.g, 0)
}

func getTransform(g uintptr) (m [6]float32) {
	var f [6]C.float
	C.og_get_transform(og(g), &f[0])
	for i := range m {
		m[i] = float32(f[i])
	}
	return
}

func setTransform(g uintptr, m [6]float32) {
	var f [6]C.float
	for i := range m {
		f[i] = C.float(m[i])
	}
	C.og_set_transform(og(g), &f[0])
}

func pathLines(pts []PointF, closed bool) uintptr {
	p := newPath()
	if len(pts) < 1 {
		return p
	}
	xy := make([]C.float, 2*len(pts))
	for i, q := range pts {
		xy[2*i], xy[2*i+1] = C.float(q.X), C.float(q.Y)
	}
	C.og_path_lines(opath(p), &xy[0], C.int(len(pts)), C.int(b2i(closed)))
	return p
}

// polylinePath: an open line through pts.
func polylinePath(pts []PointF) uintptr {
	if len(pts) < 2 {
		return newPath()
	}
	return pathLines(pts, false)
}

// closedCurvePath: a smooth closed curve through pts (GDI+'s cardinal spline, tension 0..1),
// drawn as many short lines.
func closedCurvePath(pts []PointF, tension float32) uintptr {
	n := len(pts)
	if n < 3 {
		return newPath()
	}
	t := float64(tension) // GDI+: tangents are (p[i+1] - p[i-1]) * tension / 2 ... scaled like Catmull-Rom at 0.5
	var out []PointF
	const steps = 10
	for i := 0; i < n; i++ {
		p0, p1, p2, p3 := pts[(i-1+n)%n], pts[i], pts[(i+1)%n], pts[(i+2)%n]
		m1x, m1y := t*float64(p2.X-p0.X), t*float64(p2.Y-p0.Y)
		m2x, m2y := t*float64(p3.X-p1.X), t*float64(p3.Y-p1.Y)
		for s := 0; s < steps; s++ {
			u := float64(s) / steps
			h00, h10, h01, h11 := 2*u*u*u-3*u*u+1, u*u*u-2*u*u+u, -2*u*u*u+3*u*u, u*u*u-u*u
			out = append(out, PointF{
				float32(h00*float64(p1.X) + h10*m1x + h01*float64(p2.X) + h11*m2x),
				float32(h00*float64(p1.Y) + h10*m1y + h01*float64(p2.Y) + h11*m2y),
			})
		}
	}
	return pathLines(out, true)
}

// gImageHQ: a picture drawn with the best filter (covers and labels, drawn once).
func gImageHQ(g, img uintptr, x, y, w, h float32) {
	setInterp(g, interpHighQualityBicubic)
	gImage(g, img, x, y, w, h)
}

// fillPathCover fills path with the cover (cropped to a square s wide at x, y). Core Graphics'
// clipping is anti-aliased, so the cover is simply drawn inside the path.
func fillPathCover(g, img uintptr, x, y, s float32, path uintptr) {
	if img == 0 || s < 1 {
		return
	}
	clipPath(g, path)
	setInterp(g, interpHighQualityBicubic)
	drawCoverSquare(g, img, x, y, s)
	clipReset(g)
}

// fontID: the font, its size and style, as a key
func fontID(f *Font) string { return fmt.Sprint(f.key, "|", f.size) }

/* ---------- text outlines, kept (as on Windows) ---------- */

type textPathKey struct {
	font string
	s    string
}

type textPathEntry struct {
	path     uintptr
	w, h     float32
	measured bool
}

var textPaths = map[textPathKey]*textPathEntry{}

func cachedTextPath(f *Font, s string) *textPathEntry {
	k := textPathKey{fontID(f), s}
	if e, ok := textPaths[k]; ok {
		return e
	}
	if len(textPaths) >= 700 {
		flushTextPaths()
	}
	e := &textPathEntry{path: newPath()}
	addTextPath(e.path, f, s, 0, 0)
	textPaths[k] = e
	textPathSet[e.path] = true
	return e
}

func flushTextPaths() {
	for _, e := range textPaths {
		freePath(e.path)
	}
	textPaths = map[textPathKey]*textPathEntry{}
	textPathSet = map[uintptr]bool{}
	flushGlowSprites() // they belong to those paths
}

// textPathSet: the paths kept in textPaths — only those get glow sprites (a path made and freed
// at once could come back at the same address as another text)
var textPathSet = map[uintptr]bool{}

func (e *textPathEntry) size(f *Font, s string) (float32, float32) {
	if !e.measured {
		e.w, e.h = measure(f, s)
		e.measured = true
	}
	return e.w, e.h
}

var _ = math.Pi
