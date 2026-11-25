package main

import "fmt"

// Named helpers for the GDI+ calls the shared views make (the Mac has the same names in
// gdip_darwin.go).

var (
	gpSetPathFillMode = gdiplus.NewProc("GdipSetPathFillMode")
	gpAddPathPath     = gdiplus.NewProc("GdipAddPathPath")
)

func setInterp(g uintptr, mode int)      { call(gpSetInterpolationMode, g, uintptr(mode)) }
func setTextHint(g uintptr, hint int)    { call(gpSetTextRenderingHint, g, uintptr(hint)) }
func setPixelOffset(g uintptr, mode int) { call(gpSetPixelOffsetMode, g, uintptr(mode)) }
func gFlush(g uintptr)                   { call(gpFlush, g, 1) }
func gdiFlush()                          { call(pGdiFlush) }
func closeFigure(p uintptr)              { call(gpClosePathFigure, p) }
func pathWinding(p uintptr)              { call(gpSetPathFillMode, p, 1) }
func addPath(p, o uintptr)               { call(gpAddPathPath, p, o, 0) }
func penDash(p uintptr)                  { call(gpSetPenDashStyle, p, dashStyleDash) }
func drawPathPen(g, pen, path uintptr)   { call(gpDrawPath, g, pen, path) }
func setCopyMode(g uintptr, copy bool) {
	call(gpSetCompositingMode, g, uintptr(b2i(copy)))
}
func addEllipse(p uintptr, x, y, w, h float32) {
	call(gpAddPathEllipse, p, fl(x), fl(y), fl(w), fl(h))
}
func drawArc(g, pen uintptr, x, y, w, h, start, sweep float32) {
	call(gpDrawArc, g, pen, fl(x), fl(y), fl(w), fl(h), fl(start), fl(sweep))
}
func drawEllipsePen(g, pen uintptr, x, y, w, h float32) {
	call(gpDrawEllipse, g, pen, fl(x), fl(y), fl(w), fl(h))
}

// gImageSrc draws part (sx, sy, sw, sh) of img into (x, y, w, h).
func gImageSrc(g, img uintptr, x, y, w, h, sx, sy, sw, sh float32) {
	call(gpDrawImageRectRect, g, img, fl(x), fl(y), fl(w), fl(h), fl(sx), fl(sy), fl(sw), fl(sh), 2, 0, 0, 0)
}

// fontID: the font, its size and style, as a key
func fontID(f *Font) string { return fmt.Sprint(f.family, "|", f.size, "|", f.style) }
