package main

// GDI+ flat API wrappers. GDI+ ships with every Windows version, draws anti-aliased
// text / shapes and rotates images in software — cheap enough for a few small widgets.

import (
	"math"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var (
	gdiplus = syscall.NewLazyDLL("gdiplus.dll")

	gpStartup                                = gdiplus.NewProc("GdiplusStartup")
	gpCreateBitmapFromScan0                  = gdiplus.NewProc("GdipCreateBitmapFromScan0")
	gpGetImageGraphicsContext                = gdiplus.NewProc("GdipGetImageGraphicsContext")
	gpDeleteGraphics                         = gdiplus.NewProc("GdipDeleteGraphics")
	gpDisposeImage                           = gdiplus.NewProc("GdipDisposeImage")
	gpGraphicsClear                          = gdiplus.NewProc("GdipGraphicsClear")
	gpSetSmoothingMode                       = gdiplus.NewProc("GdipSetSmoothingMode")
	gpSetInterpolationMode                   = gdiplus.NewProc("GdipSetInterpolationMode")
	gpSetTextRenderingHint                   = gdiplus.NewProc("GdipSetTextRenderingHint")
	gpSetPixelOffsetMode                     = gdiplus.NewProc("GdipSetPixelOffsetMode")
	gpSetCompositingMode                     = gdiplus.NewProc("GdipSetCompositingMode")
	gpTranslateWorldTransform                = gdiplus.NewProc("GdipTranslateWorldTransform")
	gpRotateWorldTransform                   = gdiplus.NewProc("GdipRotateWorldTransform")
	gpScaleWorldTransform                    = gdiplus.NewProc("GdipScaleWorldTransform")
	gpResetWorldTransform                    = gdiplus.NewProc("GdipResetWorldTransform")
	gpDrawImageRect                          = gdiplus.NewProc("GdipDrawImageRect")
	gpCreateImageAttributes                  = gdiplus.NewProc("GdipCreateImageAttributes")
	gpSetImageAttributesColorMatrix          = gdiplus.NewProc("GdipSetImageAttributesColorMatrix")
	gpDrawImageRectI                         = gdiplus.NewProc("GdipDrawImageRectI")
	gpCreateSolidFill                        = gdiplus.NewProc("GdipCreateSolidFill")
	gpSetSolidFillColor                      = gdiplus.NewProc("GdipSetSolidFillColor")
	gpDeleteBrush                            = gdiplus.NewProc("GdipDeleteBrush")
	gpCreatePen1                             = gdiplus.NewProc("GdipCreatePen1")
	gpDeletePen                              = gdiplus.NewProc("GdipDeletePen")
	gpSetPenLineJoin                         = gdiplus.NewProc("GdipSetPenLineJoin")
	gpSetPenStartCap                         = gdiplus.NewProc("GdipSetPenStartCap")
	gpSetPenEndCap                           = gdiplus.NewProc("GdipSetPenEndCap")
	gpSetPenDashStyle                        = gdiplus.NewProc("GdipSetPenDashStyle")
	gpFillEllipse                            = gdiplus.NewProc("GdipFillEllipse")
	gpDrawEllipse                            = gdiplus.NewProc("GdipDrawEllipse")
	gpDrawLine                               = gdiplus.NewProc("GdipDrawLine")
	gpDrawArc                                = gdiplus.NewProc("GdipDrawArc")
	gpFillRectangle                          = gdiplus.NewProc("GdipFillRectangle")
	gpFillPie                                = gdiplus.NewProc("GdipFillPie")
	gpCreatePath                             = gdiplus.NewProc("GdipCreatePath")
	gpDeletePath                             = gdiplus.NewProc("GdipDeletePath")
	gpResetPath                              = gdiplus.NewProc("GdipResetPath")
	gpAddPathEllipse                         = gdiplus.NewProc("GdipAddPathEllipse")
	gpAddPathArc                             = gdiplus.NewProc("GdipAddPathArc")
	gpClosePathFigure                        = gdiplus.NewProc("GdipClosePathFigure")
	gpAddPathClosedCurve2                    = gdiplus.NewProc("GdipAddPathClosedCurve2")
	gpAddPathLine2                           = gdiplus.NewProc("GdipAddPathLine2")
	gpAddPathString                          = gdiplus.NewProc("GdipAddPathString")
	gpFillPath                               = gdiplus.NewProc("GdipFillPath")
	gpDrawPath                               = gdiplus.NewProc("GdipDrawPath")
	gpGetPathWorldBounds                     = gdiplus.NewProc("GdipGetPathWorldBounds")
	gpSetClipPath                            = gdiplus.NewProc("GdipSetClipPath")
	gpResetClip                              = gdiplus.NewProc("GdipResetClip")
	gpSetClipRect                            = gdiplus.NewProc("GdipSetClipRect")
	gpCreateFontFamilyFromName               = gdiplus.NewProc("GdipCreateFontFamilyFromName")
	gpDeleteFontFamily                       = gdiplus.NewProc("GdipDeleteFontFamily")
	gpCreateFont                             = gdiplus.NewProc("GdipCreateFont")
	gpDeleteFont                             = gdiplus.NewProc("GdipDeleteFont")
	gpStringFormatGetGenericTypographic      = gdiplus.NewProc("GdipStringFormatGetGenericTypographic")
	gpCloneStringFormat                      = gdiplus.NewProc("GdipCloneStringFormat")
	gpSetStringFormatFlags                   = gdiplus.NewProc("GdipSetStringFormatFlags")
	gpSetStringFormatAlign                   = gdiplus.NewProc("GdipSetStringFormatAlign")
	gpSetStringFormatTrimming                = gdiplus.NewProc("GdipSetStringFormatTrimming")
	gpDeleteStringFormat                     = gdiplus.NewProc("GdipDeleteStringFormat")
	gpMeasureString                          = gdiplus.NewProc("GdipMeasureString")
	gpDrawString                             = gdiplus.NewProc("GdipDrawString")
	gpLoadImageFromStream                    = gdiplus.NewProc("GdipLoadImageFromStream")
	gpLoadImageFromFile                      = gdiplus.NewProc("GdipLoadImageFromFile")
	gpSaveImageToFile                        = gdiplus.NewProc("GdipSaveImageToFile")
	gpGetImageWidth                          = gdiplus.NewProc("GdipGetImageWidth")
	gpGetImageHeight                         = gdiplus.NewProc("GdipGetImageHeight")
	gpBitmapGetPixel                         = gdiplus.NewProc("GdipBitmapGetPixel")
	gpCreateHICONFromBitmap                  = gdiplus.NewProc("GdipCreateHICONFromBitmap")
	gpCreatePathGradientFromPath             = gdiplus.NewProc("GdipCreatePathGradientFromPath")
	gpNewInstalledFontCollection             = gdiplus.NewProc("GdipNewInstalledFontCollection")
	gpGetFontCollectionFamilyCount           = gdiplus.NewProc("GdipGetFontCollectionFamilyCount")
	gpGetFontCollectionFamilyList            = gdiplus.NewProc("GdipGetFontCollectionFamilyList")
	gpGetFamilyName                          = gdiplus.NewProc("GdipGetFamilyName")
	gpCreateLineBrush                        = gdiplus.NewProc("GdipCreateLineBrush")
	gpFillPolygon                            = gdiplus.NewProc("GdipFillPolygon")
	gpSetPathGradientCenterColor             = gdiplus.NewProc("GdipSetPathGradientCenterColor")
	gpSetPathGradientSurroundColorsWithCount = gdiplus.NewProc("GdipSetPathGradientSurroundColorsWithCount")
)

const (
	pixelFormat32bppPARGB         = 0xE200B
	smoothingAntiAlias            = 4
	interpBilinear                = 3
	interpHighQualityBicubic      = 7
	interpHighQualityBilinear     = 6
	textHintAntiAlias             = 4
	pixelOffsetHalf               = 4
	unitWorld                     = 0
	unitPixel                     = 2
	lineJoinRound                 = 2
	lineCapRound                  = 2
	dashStyleDash                 = 1
	fontStyleRegular              = 0
	fontStyleBold                 = 1
	strAlignNear                  = 0
	strAlignCenter                = 1
	strAlignFar                   = 2
	strFlagsMeasureTrailingSpaces = 0x800
	strFlagsNoWrap                = 0x1000
	strTrimEllipsisChar           = 3
	combineReplace                = 0
	matrixPrepend                 = 0
)

func fl(v float32) uintptr { return uintptr(math.Float32bits(v)) }

var gdipToken uintptr

func gdipInit() bool {
	in := struct {
		Version                  uint32
		DebugEventCallback       uintptr
		SuppressBackgroundThread int32
		SuppressExternalCodecs   int32
	}{Version: 1}
	r := call(gpStartup, uintptr(unsafe.Pointer(&gdipToken)), uintptr(unsafe.Pointer(&in)), 0)
	return r == 0
}

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

/* ---------- Bitmap + Graphics ---------- */

type Bitmap struct {
	bmp  uintptr
	g    uintptr
	w, h int
	px   []uint32 // a copy of its pixels, once asked for (only for bitmaps finished when made)
}

var (
	gpGetWorldTransform = gdiplus.NewProc("GdipGetWorldTransform")
	gpSetWorldTransform = gdiplus.NewProc("GdipSetWorldTransform")
	gpCreateMatrixE     = gdiplus.NewProc("GdipCreateMatrix")
	gpCreateMatrix2     = gdiplus.NewProc("GdipCreateMatrix2")
	gpGetMatrixElements = gdiplus.NewProc("GdipGetMatrixElements")
	gpDeleteMatrixE     = gdiplus.NewProc("GdipDeleteMatrix")
)

// getTransform / setTransform: the drawing transform, saved and put back (6 numbers).
func getTransform(g uintptr) (m [6]float32) {
	var mx uintptr
	call(gpCreateMatrixE, uintptr(unsafe.Pointer(&mx)))
	if mx == 0 {
		return [6]float32{1, 0, 0, 1, 0, 0}
	}
	call(gpGetWorldTransform, g, mx)
	call(gpGetMatrixElements, mx, uintptr(unsafe.Pointer(&m[0])))
	call(gpDeleteMatrixE, mx)
	return
}

func setTransform(g uintptr, m [6]float32) {
	var mx uintptr
	call(gpCreateMatrix2, fl(m[0]), fl(m[1]), fl(m[2]), fl(m[3]), fl(m[4]), fl(m[5]), uintptr(unsafe.Pointer(&mx)))
	if mx == 0 {
		return
	}
	call(gpSetWorldTransform, g, mx)
	call(gpDeleteMatrixE, mx)
}

var (
	gpBitmapLockBits   = gdiplus.NewProc("GdipBitmapLockBits")
	gpBitmapUnlockBits = gdiplus.NewProc("GdipBitmapUnlockBits")
	gpFlush            = gdiplus.NewProc("GdipFlush")
)

// edit: change its pixels in Go (premultiplied BGRA, w×h, row by row).
func (b *Bitmap) edit(f func(px []uint32, w, h int)) {
	if b == nil || b.bmp == 0 {
		return
	}
	call(gpFlush, b.g, 1)
	type bitmapData struct {
		w, h     uint32
		stride   int32
		format   int32
		scan0    uintptr
		reserved uintptr
	}
	r := [4]int32{0, 0, int32(b.w), int32(b.h)}
	var d bitmapData
	if st := call(gpBitmapLockBits, b.bmp, uintptr(unsafe.Pointer(&r)), 3, pixelFormat32bppPARGB, uintptr(unsafe.Pointer(&d))); st != 0 || d.scan0 == 0 {
		return
	}
	px := make([]uint32, b.w*b.h)
	row := func(y int) []uint32 {
		return unsafe.Slice((*uint32)(unsafe.Pointer(d.scan0+uintptr(int(d.stride)*y))), b.w)
	}
	for y := 0; y < b.h; y++ {
		copy(px[y*b.w:(y+1)*b.w], row(y))
	}
	f(px, b.w, b.h)
	for y := 0; y < b.h; y++ {
		copy(row(y), px[y*b.w:(y+1)*b.w])
	}
	call(gpBitmapUnlockBits, b.bmp, uintptr(unsafe.Pointer(&d)))
	b.px = nil
}

// pixels: its premultiplied BGRA pixels (read once and kept; nil if that fails).
func (b *Bitmap) pixels() []uint32 {
	if b == nil || b.bmp == 0 {
		return nil
	}
	if b.px != nil {
		return b.px
	}
	type bitmapData struct {
		w, h     uint32
		stride   int32
		format   int32
		scan0    uintptr
		reserved uintptr
	}
	r := [4]int32{0, 0, int32(b.w), int32(b.h)}
	var d bitmapData
	if st := call(gpBitmapLockBits, b.bmp, uintptr(unsafe.Pointer(&r)), 1, pixelFormat32bppPARGB, uintptr(unsafe.Pointer(&d))); st != 0 || d.scan0 == 0 {
		return nil
	}
	px := make([]uint32, b.w*b.h)
	for y := 0; y < b.h; y++ {
		copy(px[y*b.w:(y+1)*b.w], unsafe.Slice((*uint32)(unsafe.Pointer(d.scan0+uintptr(int(d.stride)*y))), b.w))
	}
	call(gpBitmapUnlockBits, b.bmp, uintptr(unsafe.Pointer(&d)))
	b.px = px
	return px
}

// newBitmap creates a GDI+ owned premultiplied ARGB bitmap (used for cached layers).
// live: how many of each are alive (the test mode writes them: a number that keeps growing is a leak)
var liveCanvas, liveBitmap, liveImage, liveFont atomic.Int64
var canvasMade, bitmapMade atomic.Int64 // how many were ever made (churn)

func newBitmap(w, h int) *Bitmap {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	b := &Bitmap{w: w, h: h}
	call(gpCreateBitmapFromScan0, uintptr(w), uintptr(h), 0, pixelFormat32bppPARGB, 0, uintptr(unsafe.Pointer(&b.bmp)))
	if b.bmp == 0 {
		return nil
	}
	liveBitmap.Add(1)
	bitmapMade.Add(1)
	call(gpGetImageGraphicsContext, b.bmp, uintptr(unsafe.Pointer(&b.g)))
	setupGraphics(b.g)
	return b
}

func (b *Bitmap) Free() {
	if b == nil {
		return
	}
	b.px = nil
	if b.g != 0 {
		call(gpDeleteGraphics, b.g)
		b.g = 0
	}
	if b.bmp != 0 {
		call(gpDisposeImage, b.bmp)
		liveBitmap.Add(-1)
		b.bmp = 0
	}
}

func setupGraphics(g uintptr) {
	call(gpSetSmoothingMode, g, smoothingAntiAlias)
	call(gpSetInterpolationMode, g, interpBilinear)
	call(gpSetTextRenderingHint, g, textHintAntiAlias)
	call(gpSetPixelOffsetMode, g, pixelOffsetHalf)
}

// Canvas is a top-down 32-bit DIB section shared with GDI+ — exactly what UpdateLayeredWindow wants.
type Canvas struct {
	w, h int
	dc   uintptr
	dib  uintptr
	old  uintptr
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
	c := &Canvas{w: w, h: h}
	c.dc = call(pCreateCompatibleDC, 0)
	bi := BITMAPINFOHEADER{BiSize: uint32(unsafe.Sizeof(BITMAPINFOHEADER{})), BiWidth: int32(w), BiHeight: -int32(h), BiPlanes: 1, BiBitCount: 32}
	// BITMAPINFO = header + 1 RGBQUAD
	var bmi [unsafe.Sizeof(BITMAPINFOHEADER{}) + 4]byte
	*(*BITMAPINFOHEADER)(unsafe.Pointer(&bmi[0])) = bi
	c.dib = call(pCreateDIBSection, c.dc, uintptr(unsafe.Pointer(&bmi[0])), 0, uintptr(unsafe.Pointer(&c.bits)), 0, 0)
	if c.dib == 0 || c.bits == 0 {
		logf("CreateDIBSection failed %dx%d", w, h)
		return nil
	}
	c.old = call(pSelectObject, c.dc, c.dib)
	call(gpCreateBitmapFromScan0, uintptr(w), uintptr(h), uintptr(w*4), pixelFormat32bppPARGB, c.bits, uintptr(unsafe.Pointer(&c.bmp)))
	call(gpGetImageGraphicsContext, c.bmp, uintptr(unsafe.Pointer(&c.g)))
	setupGraphics(c.g)
	liveCanvas.Add(1)
	canvasMade.Add(1)
	return c
}

func (c *Canvas) Free() {
	if c == nil {
		return
	}
	if c.dib != 0 {
		liveCanvas.Add(-1)
	}
	if c.g != 0 {
		call(gpDeleteGraphics, c.g)
	}
	if c.bmp != 0 {
		call(gpDisposeImage, c.bmp)
	}
	if c.dc != 0 {
		call(pSelectObject, c.dc, c.old)
		call(pDeleteDC, c.dc)
	}
	if c.dib != 0 {
		call(pDeleteObject, c.dib)
	}
}

/* ---------- Drawing helpers (g = any GDI+ Graphics) ---------- */

func gClear(g uintptr, c uint32) { call(gpGraphicsClear, g, uintptr(c)) }

func gTranslate(g uintptr, dx, dy float32) {
	call(gpTranslateWorldTransform, g, fl(dx), fl(dy), matrixPrepend)
}
func gRotate(g uintptr, deg float32)   { call(gpRotateWorldTransform, g, fl(deg), matrixPrepend) }
func gScale(g uintptr, sx, sy float32) { call(gpScaleWorldTransform, g, fl(sx), fl(sy), matrixPrepend) }
func gReset(g uintptr)                 { call(gpResetWorldTransform, g) }

func gImage(g, img uintptr, x, y, w, h float32) {
	call(gpDrawImageRect, g, img, fl(x), fl(y), fl(w), fl(h))
}

var brushCache = map[uint32]uintptr{}

func brush(c uint32) uintptr {
	if b, ok := brushCache[c]; ok {
		return b
	}
	var b uintptr
	call(gpCreateSolidFill, uintptr(c), uintptr(unsafe.Pointer(&b)))
	brushCache[c] = b
	return b
}

func trimBrushes() {
	if len(brushCache) > 384 {
		for k, v := range brushCache {
			call(gpDeleteBrush, v)
			delete(brushCache, k)
		}
	}
}

func newPen(c uint32, width float32) uintptr {
	var p uintptr
	call(gpCreatePen1, uintptr(c), fl(width), unitWorld, uintptr(unsafe.Pointer(&p)))
	call(gpSetPenLineJoin, p, lineJoinRound)
	call(gpSetPenStartCap, p, lineCapRound)
	call(gpSetPenEndCap, p, lineCapRound)
	return p
}
func freePen(p uintptr) { call(gpDeletePen, p) }

func fillEllipse(g uintptr, c uint32, x, y, w, h float32) {
	call(gpFillEllipse, g, brush(c), fl(x), fl(y), fl(w), fl(h))
}
func fillCircle(g uintptr, c uint32, cx, cy, r float32) { fillEllipse(g, c, cx-r, cy-r, 2*r, 2*r) }

func strokeCircle(g uintptr, c uint32, cx, cy, r, width float32) {
	p := newPen(c, width)
	call(gpDrawEllipse, g, p, fl(cx-r), fl(cy-r), fl(2*r), fl(2*r))
	freePen(p)
}

func line(g uintptr, c uint32, width, x1, y1, x2, y2 float32) {
	p := newPen(c, width)
	call(gpDrawLine, g, p, fl(x1), fl(y1), fl(x2), fl(y2))
	freePen(p)
}

func fillRect(g uintptr, c uint32, x, y, w, h float32) {
	call(gpFillRectangle, g, brush(c), fl(x), fl(y), fl(w), fl(h))
}

func fillPie(g uintptr, c uint32, x, y, w, h, start, sweep float32) {
	call(gpFillPie, g, brush(c), fl(x), fl(y), fl(w), fl(h), fl(start), fl(sweep))
}

func newPath() uintptr {
	var p uintptr
	call(gpCreatePath, 0, uintptr(unsafe.Pointer(&p)))
	return p
}
func freePath(p uintptr) { call(gpDeletePath, p) }

func roundRectPath(x, y, w, h, r float32) uintptr {
	p := newPath()
	d := 2 * r
	if d > w {
		d = w
	}
	if d > h {
		d = h
	}
	call(gpAddPathArc, p, fl(x), fl(y), fl(d), fl(d), fl(180), fl(90))
	call(gpAddPathArc, p, fl(x+w-d), fl(y), fl(d), fl(d), fl(270), fl(90))
	call(gpAddPathArc, p, fl(x+w-d), fl(y+h-d), fl(d), fl(d), fl(0), fl(90))
	call(gpAddPathArc, p, fl(x), fl(y+h-d), fl(d), fl(d), fl(90), fl(90))
	call(gpClosePathFigure, p)
	return p
}

func clipPath(g, path uintptr) { call(gpSetClipPath, g, path, combineReplace) }
func clipReset(g uintptr)      { call(gpResetClip, g) }
func clipRect(g uintptr, x, y, w, h float32) {
	call(gpSetClipRect, g, fl(x), fl(y), fl(w), fl(h), combineReplace)
}

func clipCircle(g uintptr, cx, cy, r float32) uintptr {
	p := newPath()
	call(gpAddPathEllipse, p, fl(cx-r), fl(cy-r), fl(2*r), fl(2*r))
	clipPath(g, p)
	return p
}

// softGlow draws a radial gradient disc that fades to transparent at radius r.
func softGlow(g uintptr, cx, cy, r float32, center uint32) {
	p := newPath()
	call(gpAddPathEllipse, p, fl(cx-r), fl(cy-r), fl(2*r), fl(2*r))
	var br uintptr
	call(gpCreatePathGradientFromPath, p, uintptr(unsafe.Pointer(&br)))
	if br != 0 {
		call(gpSetPathGradientCenterColor, br, uintptr(center))
		surround := center & 0x00FFFFFF
		n := int32(1)
		call(gpSetPathGradientSurroundColorsWithCount, br, uintptr(unsafe.Pointer(&surround)), uintptr(unsafe.Pointer(&n)))
		call(gpFillPath, g, br, p)
		call(gpDeleteBrush, br)
	}
	freePath(p)
}

/* ---------- Fonts + text ---------- */

type Font struct {
	family uintptr
	font   uintptr
	size   float32
	style  int
}

var familyCache = map[string]uintptr{}

func fontFamily(name string) uintptr {
	if f, ok := familyCache[name]; ok {
		return f
	}
	var f uintptr
	call(gpCreateFontFamilyFromName, uintptr(unsafe.Pointer(u16(name))), 0, uintptr(unsafe.Pointer(&f)))
	if f == 0 && name != "Segoe UI" {
		logf("font %q not installed, using Segoe UI", name)
		f = fontFamily("Segoe UI")
	}
	familyCache[name] = f
	return f
}

func newFont(name string, size float32, bold bool) *Font {
	st := fontStyleRegular
	if bold {
		st = fontStyleBold
	}
	f := &Font{family: fontFamily(name), size: size, style: st}
	call(gpCreateFont, f.family, fl(size), uintptr(st), unitPixel, uintptr(unsafe.Pointer(&f.font)))
	if f.font != 0 {
		liveFont.Add(1)
	}
	return f
}

func (f *Font) Free() {
	if f != nil && f.font != 0 {
		call(gpDeleteFont, f.font)
		liveFont.Add(-1)
		f.font = 0
	}
}

var typoFormat uintptr      // generic typographic + measure trailing spaces
var listFormat uintptr      // no wrap + ellipsis
var listFormatRight uintptr // same, right-aligned

func formats() {
	var base uintptr
	call(gpStringFormatGetGenericTypographic, uintptr(unsafe.Pointer(&base)))
	call(gpCloneStringFormat, base, uintptr(unsafe.Pointer(&typoFormat)))
	call(gpSetStringFormatFlags, typoFormat, strFlagsMeasureTrailingSpaces)
	call(gpCloneStringFormat, base, uintptr(unsafe.Pointer(&listFormat)))
	call(gpSetStringFormatFlags, listFormat, strFlagsNoWrap)
	call(gpSetStringFormatTrimming, listFormat, strTrimEllipsisChar)
	call(gpCloneStringFormat, listFormat, uintptr(unsafe.Pointer(&listFormatRight)))
	call(gpSetStringFormatAlign, listFormatRight, strAlignFar)
}

// drawTextRight draws a single line that ends at xRight.
func drawTextRight(g uintptr, f *Font, s string, xRight, y, maxW float32, c uint32) {
	if s == "" {
		return
	}
	w, _ := syscall.UTF16FromString(s)
	r := RectF{xRight - maxW, y, maxW, f.size * 1.6}
	call(gpDrawString, g, uintptr(unsafe.Pointer(&w[0])), uintptr(len(w)-1), f.font, uintptr(unsafe.Pointer(&r)), listFormatRight, brush(c))
}

// fillPathGradient fills a path with a vertical gradient from top to bottom colour.
func fillPathGradient(g, path uintptr, top, bottom uint32, y0, y1 float32) {
	if y1 <= y0 {
		y1 = y0 + 1
	}
	p1, p2 := PointF{0, y0}, PointF{0, y1}
	var br uintptr
	call(gpCreateLineBrush, uintptr(unsafe.Pointer(&p1)), uintptr(unsafe.Pointer(&p2)), uintptr(top), uintptr(bottom), 3, uintptr(unsafe.Pointer(&br)))
	if br == 0 {
		call(gpFillPath, g, brush(top), path)
		return
	}
	call(gpFillPath, g, br, path)
	call(gpDeleteBrush, br)
}

// fillRectGrad fills a rectangle with a linear gradient from (x0,y0)=c0 to (x1,y1)=c1.
func fillRectGrad(g uintptr, x, y, w, h, x0, y0, x1, y1 float32, c0, c1 uint32) {
	if w <= 0 || h <= 0 {
		return
	}
	if x0 == x1 && y0 == y1 {
		y1 += 1
	}
	p1, p2 := PointF{x0, y0}, PointF{x1, y1}
	var br uintptr
	call(gpCreateLineBrush, uintptr(unsafe.Pointer(&p1)), uintptr(unsafe.Pointer(&p2)), uintptr(c0), uintptr(c1), 3, uintptr(unsafe.Pointer(&br)))
	if br == 0 {
		return
	}
	call(gpFillRectangle, g, br, fl(x), fl(y), fl(w), fl(h))
	call(gpDeleteBrush, br)
}

// fillPoly fills a closed polygon.
func fillPoly(g uintptr, c uint32, pts []PointF) {
	if len(pts) < 3 {
		return
	}
	call(gpFillPolygon, g, brush(c), uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)), 0)
}

// clipExclude removes a path from the current clip (crescent moon).
func clipExclude(g, path uintptr) { call(gpSetClipPath, g, path, 4) }

func strokePath(g, path uintptr, c uint32, width float32) {
	p := newPen(c, width)
	call(gpDrawPath, g, p, path)
	freePen(p)
}

func fillPathColor(g, path uintptr, c uint32) { call(gpFillPath, g, brush(c), path) }

var measureBmp *Bitmap

// measure returns the width/height of s in font f (single line).
func measure(f *Font, s string) (float32, float32) {
	if measureBmp == nil {
		measureBmp = newBitmap(8, 8)
	}
	if s == "" {
		return 0, f.size
	}
	w, _ := syscall.UTF16FromString(s)
	layout := RectF{0, 0, 100000, 100000}
	var out RectF
	call(gpMeasureString, measureBmp.g, uintptr(unsafe.Pointer(&w[0])), uintptr(len(w)-1), f.font, uintptr(unsafe.Pointer(&layout)), typoFormat, uintptr(unsafe.Pointer(&out)), 0, 0)
	return out.W, out.H
}

// drawText draws a single line with GdipDrawString (used for small UI text).
func drawText(g uintptr, f *Font, s string, x, y, maxW float32, c uint32) {
	if s == "" {
		return
	}
	w, _ := syscall.UTF16FromString(s)
	r := RectF{x, y, maxW, f.size * 1.6}
	call(gpDrawString, g, uintptr(unsafe.Pointer(&w[0])), uintptr(len(w)-1), f.font, uintptr(unsafe.Pointer(&r)), listFormat, brush(c))
}

// textPath builds an outline path of s at (x, y) — used for big lyrics so we can add a glow.
func addTextPath(path uintptr, f *Font, s string, x, y float32) {
	if s == "" {
		return
	}
	w, _ := syscall.UTF16FromString(s)
	r := RectF{x, y, 100000, 100000}
	call(gpAddPathString, path, uintptr(unsafe.Pointer(&w[0])), uintptr(len(w)-1), f.family, uintptr(f.style), fl(f.size), uintptr(unsafe.Pointer(&r)), typoFormat)
}

// textPaths: each word's outline, made once at (0, 0) and filled wherever it is needed (moved
// by the drawing transform). Building an outline from the font is the slow part of drawing big
// words, and the lyrics draw the same words again every frame.
type textPathKey struct {
	fam   uintptr
	size  float32
	style int
	s     string
}

type textPathEntry struct {
	path     uintptr
	w, h     float32
	measured bool
}

var textPaths = map[textPathKey]*textPathEntry{}

func cachedTextPath(f *Font, s string) *textPathEntry {
	k := textPathKey{f.family, f.size, f.style, s}
	if e, ok := textPaths[k]; ok {
		return e
	}
	if len(textPaths) >= 700 { // plenty for a few lines in every style; start again past that
		flushTextPaths()
	}
	e := &textPathEntry{path: newPath()}
	addTextPath(e.path, f, s, 0, 0)
	textPaths[k] = e
	return e
}

func flushTextPaths() {
	for _, e := range textPaths {
		freePath(e.path)
	}
	textPaths = map[textPathKey]*textPathEntry{}
}

func (e *textPathEntry) size(f *Font, s string) (float32, float32) {
	if !e.measured {
		e.w, e.h = measure(f, s)
		e.measured = true
	}
	return e.w, e.h
}

// drawGlowPath fills a path with an optional soft glow + drop shadow.
func drawGlowPath(g, path uintptr, fill uint32, glow uint32, glowAmt float32, shadow bool) {
	if shadow {
		gTranslate(g, 0, 2)
		call(gpFillPath, g, brush(argb(uint8(float64(fill>>24)*0.45), 0, 0, 0)), path)
		gTranslate(g, 0, -2)
	}
	if glowAmt > 0 {
		for i, wmul := range []float32{1.0, 0.6, 0.3} {
			a := float64(glow>>24) / 255 * []float64{0.10, 0.16, 0.24}[i]
			p := newPen(withAlpha(glow|0xFF000000, a), glowAmt*wmul)
			call(gpDrawPath, g, p, path)
			freePen(p)
		}
	}
	call(gpFillPath, g, brush(fill), path)
}

func pathBounds(path uintptr) RectF {
	var r RectF
	call(gpGetPathWorldBounds, path, uintptr(unsafe.Pointer(&r)), 0, 0)
	return r
}

/* ---------- Images ---------- */

func imageFromBytes(b []byte) uintptr {
	if len(b) == 0 {
		return 0
	}
	stream := call(pSHCreateMemStream, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	if stream == 0 {
		return 0
	}
	defer comRelease(stream)
	var img uintptr
	if call(gpLoadImageFromStream, stream, uintptr(unsafe.Pointer(&img))) != 0 {
		return 0
	}
	liveImage.Add(1)
	return img
}

func imageFromFile(path string) uintptr {
	var img uintptr
	call(gpLoadImageFromFile, uintptr(unsafe.Pointer(u16(path))), uintptr(unsafe.Pointer(&img)))
	if img != 0 {
		liveImage.Add(1)
	}
	return img
}

var pngEncoder = GUID{0x557CF406, 0x1A04, 0x11D3, [8]byte{0x9A, 0x73, 0x00, 0x00, 0xF8, 0x1E, 0xF3, 0x2E}}

func savePNG(img uintptr, path string) bool {
	return call(gpSaveImageToFile, img, uintptr(unsafe.Pointer(u16(path))), uintptr(unsafe.Pointer(&pngEncoder)), 0) == 0
}

func imageSize(img uintptr) (int, int) {
	var w, h uint32
	call(gpGetImageWidth, img, uintptr(unsafe.Pointer(&w)))
	call(gpGetImageHeight, img, uintptr(unsafe.Pointer(&h)))
	return int(w), int(h)
}

func freeImage(img uintptr) {
	if img != 0 {
		call(gpDisposeImage, img)
		liveImage.Add(-1)
	}
}

func pixel(bmp uintptr, x, y int) uint32 {
	var c uint32
	call(gpBitmapGetPixel, bmp, uintptr(x), uintptr(y), uintptr(unsafe.Pointer(&c)))
	return c
}

func hiconFromBitmap(b *Bitmap) uintptr {
	var h uintptr
	call(gpCreateHICONFromBitmap, b.bmp, uintptr(unsafe.Pointer(&h)))
	return h
}

// closedCurvePath: a smooth closed curve through pts (tension 0..1).
func closedCurvePath(pts []PointF, tension float32) uintptr {
	p := newPath()
	if len(pts) > 2 {
		call(gpAddPathClosedCurve2, p, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)), fl(tension))
	}
	return p
}

// polylinePath: an open line through pts.
func polylinePath(pts []PointF) uintptr {
	p := newPath()
	if len(pts) > 1 {
		call(gpAddPathLine2, p, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)))
	}
	return p
}

// imgAttr: one reusable ImageAttributes for drawing images see-through (UI thread only).
var imgAttr uintptr

// gImageAlpha draws img (iw × ih source pixels) into (x, y, w, h) at opacity a (0..1).
func gImageAlpha(g, img uintptr, iw, ih int, x, y, w, h float32, a float64) {
	if a >= 0.999 {
		gImage(g, img, x, y, w, h)
		return
	}
	if a <= 0.004 {
		return
	}
	if imgAttr == 0 {
		call(gpCreateImageAttributes, uintptr(unsafe.Pointer(&imgAttr)))
		if imgAttr == 0 {
			gImage(g, img, x, y, w, h)
			return
		}
	}
	var m [25]float32
	m[0], m[6], m[12], m[18], m[24] = 1, 1, 1, float32(a), 1
	call(gpSetImageAttributesColorMatrix, imgAttr, 0, 1, uintptr(unsafe.Pointer(&m[0])), 0, 0)
	const unitPixel = 2
	call(gpDrawImageRectRect, g, img, fl(x), fl(y), fl(w), fl(h), fl(0), fl(0), fl(float32(iw)), fl(float32(ih)), unitPixel, imgAttr, 0, 0)
}

var (
	gpSetImageAttributesWrapMode = gdiplus.NewProc("GdipSetImageAttributesWrapMode")
	gpGetInterpolationMode       = gdiplus.NewProc("GdipGetInterpolationMode")
	gpCreateTextureE             = gdiplus.NewProc("GdipCreateTexture")
	gpTranslateTextureTransform  = gdiplus.NewProc("GdipTranslateTextureTransform")
	wrapAttr                     uintptr
)

// gImageHQ draws a picture with the best filter GDI+ has, and without the half-see-through
// fringe its edges get otherwise (the edge pixels are mirrored instead of blended with nothing).
// For pictures drawn once into a cached layer — covers, labels — not for every frame.
func gImageHQ(g, img uintptr, x, y, w, h float32) {
	iw, ih := imageSize(img)
	if iw <= 0 || ih <= 0 {
		return
	}
	if wrapAttr == 0 {
		call(gpCreateImageAttributes, uintptr(unsafe.Pointer(&wrapAttr)))
		if wrapAttr != 0 {
			call(gpSetImageAttributesWrapMode, wrapAttr, 3, 0, 0) // WrapModeTileFlipXY
		}
	}
	var mode int32
	call(gpGetInterpolationMode, g, uintptr(unsafe.Pointer(&mode)))
	call(gpSetInterpolationMode, g, interpHighQualityBicubic)
	if wrapAttr != 0 {
		call(gpDrawImageRectRect, g, img, fl(x), fl(y), fl(w), fl(h), fl(0), fl(0), fl(float32(iw)), fl(float32(ih)), 2, wrapAttr, 0, 0)
	} else {
		gImage(g, img, x, y, w, h)
	}
	call(gpSetInterpolationMode, g, uintptr(mode))
}

// fillPathCover fills path with the cover (cropped to a square s wide at x, y) — with smooth
// edges. A clip path has hard, stair-stepped edges; this draws the cover at its final size once,
// then fills the shape with it, so round corners and round labels come out clean.
func fillPathCover(g, img uintptr, x, y, s float32, path uintptr) {
	n := int(math.Ceil(float64(s))) + 2
	if img == 0 || n < 2 || n > 4096 {
		return
	}
	b := newBitmap(n, n)
	if b == nil {
		return
	}
	defer b.Free()
	gClear(b.g, 0)
	drawCoverSquare(b.g, img, 1, 1, s)
	var tb uintptr
	call(gpCreateTextureE, b.bmp, 0, uintptr(unsafe.Pointer(&tb)))
	if tb == 0 {
		return
	}
	call(gpTranslateTextureTransform, tb, fl(x-1), fl(y-1), 0)
	call(gpFillPath, g, tb, path)
	call(gpDeleteBrush, tb)
}
