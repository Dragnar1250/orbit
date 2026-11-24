// The Mac drawing layer: the same operations Orbit uses from GDI+ on Windows, over Core
// Graphics and Core Text. Every canvas is a premultiplied BGRA bitmap laid out top-down, like
// the Windows DIBs, so the Go pixel code works unchanged on both.
#import <Cocoa/Cocoa.h>
#import <CoreText/CoreText.h>
#import <ImageIO/ImageIO.h>
#import <Accelerate/Accelerate.h>
#import <IOSurface/IOSurface.h>
#include "gdip_darwin.h"

struct OG {
	CGContextRef ctx;
	int w, h;
	void *bits;
	// a widget's canvas draws straight into the IOSurfaces the window server shows (no copy):
	// one is on screen, the next one is drawn
	IOSurfaceRef surf[4];
	CGContextRef sctx[4];
	int nsurf, cur, locked;
	size_t stride;
	CGAffineTransform T;     // the "world transform" (GDI+ terms)
	CFMutableArrayRef clips; // CGPaths in base space; an even-odd one is stored in eoClips
	CFMutableArrayRef eoClips;
	int interp, copyMode;
};

struct OPath { CGMutablePathRef p; int winding; };
struct OPen { CGFloat r, g, b, a, width; int dash; };
struct OImg { CGImageRef img; OG *og; };
struct OFont { CTFontRef ct; float size; float ascent, descent, leading; };

static CGColorSpaceRef rgb(void) {
	static CGColorSpaceRef cs;
	if (!cs) cs = CGColorSpaceCreateDeviceRGB();
	return cs;
}

static void setFill(CGContextRef c, uint32_t argb) {
	CGContextSetRGBFillColor(c, ((argb >> 16) & 255) / 255.0, ((argb >> 8) & 255) / 255.0, (argb & 255) / 255.0, (argb >> 24) / 255.0);
}

static CGColorRef color(uint32_t argb) {
	CGFloat c[4] = {((argb >> 16) & 255) / 255.0, ((argb >> 8) & 255) / 255.0, (argb & 255) / 255.0, (argb >> 24) / 255.0};
	return CGColorCreate(rgb(), c);
}

// og_apply rebuilds the context state from scratch: base (flipped) → clips → transform.
static void og_apply(OG *g) {
	CGContextRestoreGState(g->ctx);
	CGContextSaveGState(g->ctx);
	CFIndex n = CFArrayGetCount(g->clips);
	for (CFIndex i = 0; i < n; i++) {
		CGContextAddPath(g->ctx, (CGPathRef)CFArrayGetValueAtIndex(g->clips, i));
		CGContextClip(g->ctx);
	}
	n = CFArrayGetCount(g->eoClips);
	for (CFIndex i = 0; i < n; i++) {
		CGContextAddPath(g->ctx, (CGPathRef)CFArrayGetValueAtIndex(g->eoClips, i));
		CGContextEOClip(g->ctx);
	}
	CGContextConcatCTM(g->ctx, g->T);
	CGContextSetInterpolationQuality(g->ctx, g->interp >= 7 ? kCGInterpolationHigh : kCGInterpolationMedium);
	CGContextSetBlendMode(g->ctx, g->copyMode ? kCGBlendModeCopy : kCGBlendModeNormal);
	CGContextSetShouldAntialias(g->ctx, true);
	CGContextSetLineJoin(g->ctx, kCGLineJoinRound);
	CGContextSetLineCap(g->ctx, kCGLineCapRound);
	CGContextSetTextMatrix(g->ctx, CGAffineTransformMakeScale(1, -1)); // text upright in the flipped space
}

// what is alive (the perf log shows it: a leak would show as numbers that only grow)
long liveOG, liveOGBytes, liveSurfBytes, liveImg, liveImgBytes;
void og_live(long *out) { out[0] = liveOG; out[1] = liveOGBytes; out[2] = liveSurfBytes; out[3] = liveImg; out[4] = liveImgBytes; }

OG *og_new(int w, int h) {
	if (w < 1) w = 1;
	if (h < 1) h = 1;
	OG *g = calloc(1, sizeof(OG));
	g->w = w;
	g->h = h;
	g->bits = calloc((size_t)w * h, 4);
	if (!g->bits) { free(g); return NULL; }
	g->ctx = CGBitmapContextCreate(g->bits, w, h, 8, (size_t)w * 4, rgb(), kCGImageAlphaPremultipliedFirst | kCGBitmapByteOrder32Little);
	if (!g->ctx) { free(g->bits); free(g); return NULL; }
	// top-left origin, y down — like GDI+
	CGContextTranslateCTM(g->ctx, 0, h);
	CGContextScaleCTM(g->ctx, 1, -1);
	CGContextSaveGState(g->ctx);
	g->T = CGAffineTransformIdentity;
	g->clips = CFArrayCreateMutable(NULL, 0, &kCFTypeArrayCallBacks);
	g->eoClips = CFArrayCreateMutable(NULL, 0, &kCFTypeArrayCallBacks);
	g->interp = 3;
	og_apply(g);
	liveOG++;
	liveOGBytes += (long)w * h * 4;
	return g;
}

static IOSurfaceRef newSurf(int w, int h) {
	size_t row = IOSurfaceAlignProperty(kIOSurfaceBytesPerRow, (size_t)w * 4);
	NSDictionary *p = @{
		(id)kIOSurfaceWidth: @(w), (id)kIOSurfaceHeight: @(h), (id)kIOSurfaceBytesPerElement: @4,
		(id)kIOSurfaceBytesPerRow: @(row), (id)kIOSurfacePixelFormat: @((unsigned)'BGRA'),
	};
	return IOSurfaceCreate((__bridge CFDictionaryRef)p);
}

// bind surface i as the one drawn on (made when first needed), locked for the CPU
static int og_bind(OG *g, int i) {
	if (!g->surf[i]) {
		g->surf[i] = newSurf(g->w, g->h);
		if (!g->surf[i]) return 0;
		IOSurfaceLock(g->surf[i], 0, NULL);
		g->sctx[i] = CGBitmapContextCreate(IOSurfaceGetBaseAddress(g->surf[i]), g->w, g->h, 8, IOSurfaceGetBytesPerRow(g->surf[i]), rgb(), kCGImageAlphaPremultipliedFirst | kCGBitmapByteOrder32Little);
		IOSurfaceUnlock(g->surf[i], 0, NULL);
		if (!g->sctx[i]) { CFRelease(g->surf[i]); g->surf[i] = NULL; return 0; }
		liveSurfBytes += (long)IOSurfaceGetAllocSize(g->surf[i]);
		CGContextTranslateCTM(g->sctx[i], 0, g->h);
		CGContextScaleCTM(g->sctx[i], 1, -1);
		CGContextSaveGState(g->sctx[i]);
		if (i >= g->nsurf) g->nsurf = i + 1;
	}
	IOSurfaceLock(g->surf[i], kIOSurfaceLockAvoidSync, NULL); // (only surfaces nobody reads are drawn on)
	g->locked = 1;
	g->cur = i;
	g->ctx = g->sctx[i];
	g->bits = IOSurfaceGetBaseAddress(g->surf[i]);
	g->stride = IOSurfaceGetBytesPerRow(g->surf[i]);
	og_apply(g);
	return 1;
}

OG *og_new_surface(int w, int h) {
	if (w < 1) w = 1;
	if (h < 1) h = 1;
	OG *g = calloc(1, sizeof(OG));
	g->w = w;
	g->h = h;
	g->T = CGAffineTransformIdentity;
	g->clips = CFArrayCreateMutable(NULL, 0, &kCFTypeArrayCallBacks);
	g->eoClips = CFArrayCreateMutable(NULL, 0, &kCFTypeArrayCallBacks);
	g->interp = 3;
	if (!og_bind(g, 0)) {
		CFRelease(g->clips);
		CFRelease(g->eoClips);
		free(g);
		return NULL;
	}
	return g;
}

// og_surface_show: the surface just drawn, to put on screen (NULL: not a surface canvas)
void *og_surface_show(OG *g) {
	if (!g || !g->nsurf) return NULL;
	if (g->locked) { IOSurfaceUnlock(g->surf[g->cur], 0, NULL); g->locked = 0; }
	return g->surf[g->cur];
}

// og_surface_next: drawing starts again after a frame was shown — on a surface the window server
// isn't reading (its old picture: every widget clears before it draws)
void og_surface_next(OG *g) {
	if (!g || !g->nsurf) return;
	if (g->locked) { IOSurfaceUnlock(g->surf[g->cur], 0, NULL); g->locked = 0; }
	for (int k = 1; k < 4; k++) { // a free one that exists (usually two are enough)
		int i = (g->cur + k) % 4;
		if (g->surf[i] && !IOSurfaceIsInUse(g->surf[i]) && og_bind(g, i)) return;
	}
	for (int i = 0; i < 4; i++) // else a new one
		if (!g->surf[i] && og_bind(g, i)) return;
	og_bind(g, (g->cur + 1) % 4); // all four busy: the oldest
}

void og_free(OG *g) {
	if (!g) return;
	if (g->nsurf) {
		if (g->locked) IOSurfaceUnlock(g->surf[g->cur], 0, NULL);
		for (int i = 0; i < 4; i++) {
			if (g->sctx[i]) { CGContextRestoreGState(g->sctx[i]); CGContextRelease(g->sctx[i]); }
			if (g->surf[i]) { liveSurfBytes -= (long)IOSurfaceGetAllocSize(g->surf[i]); CFRelease(g->surf[i]); }
		}
		CFRelease(g->clips);
		CFRelease(g->eoClips);
		free(g);
		return;
	}
	CGContextRestoreGState(g->ctx);
	CGContextRelease(g->ctx);
	CFRelease(g->clips);
	CFRelease(g->eoClips);
	free(g->bits);
	liveOG--;
	liveOGBytes -= (long)g->w * g->h * 4;
	free(g);
}

void *og_bits(OG *g) { return g ? g->bits : NULL; }
int og_width(OG *g) { return g ? g->w : 0; }
int og_height(OG *g) { return g ? g->h : 0; }

/* ---------- state ---------- */

void og_translate(OG *g, float dx, float dy) { g->T = CGAffineTransformTranslate(g->T, dx, dy); CGContextTranslateCTM(g->ctx, dx, dy); }
void og_rotate(OG *g, float deg) { CGFloat r = deg * M_PI / 180; g->T = CGAffineTransformRotate(g->T, r); CGContextRotateCTM(g->ctx, r); }
void og_scale(OG *g, float sx, float sy) { g->T = CGAffineTransformScale(g->T, sx, sy); CGContextScaleCTM(g->ctx, sx, sy); }
void og_reset(OG *g) { g->T = CGAffineTransformIdentity; og_apply(g); }
void og_interp(OG *g, int mode) { g->interp = mode; CGContextSetInterpolationQuality(g->ctx, mode >= 7 ? kCGInterpolationHigh : kCGInterpolationMedium); }
void og_copy_mode(OG *g, int on) { g->copyMode = on; CGContextSetBlendMode(g->ctx, on ? kCGBlendModeCopy : kCGBlendModeNormal); }

static CGPathRef inBase(OG *g, CGPathRef p) { return CGPathCreateCopyByTransformingPath(p, &g->T); }

void og_clip_path(OG *g, OPath *p) {
	CFArrayRemoveAllValues(g->clips);
	CFArrayRemoveAllValues(g->eoClips);
	CGPathRef b = inBase(g, p->p);
	CFArrayAppendValue(p->winding ? g->clips : g->eoClips, b);
	CGPathRelease(b);
	og_apply(g);
}

void og_clip_rect(OG *g, float x, float y, float w, float h) {
	CFArrayRemoveAllValues(g->clips);
	CFArrayRemoveAllValues(g->eoClips);
	CGPathRef r = CGPathCreateWithRect(CGRectMake(x, y, w, h), &g->T);
	CFArrayAppendValue(g->clips, r);
	CGPathRelease(r);
	og_apply(g);
}

// clipExclude: the current clip minus the path (a big rectangle with the path cut out, even-odd)
void og_clip_exclude(OG *g, OPath *p) {
	CGMutablePathRef m = CGPathCreateMutable();
	CGPathAddRect(m, NULL, CGRectMake(-100000, -100000, 200000, 200000));
	CGPathRef b = inBase(g, p->p);
	CGPathAddPath(m, NULL, b);
	CGPathRelease(b);
	CFArrayAppendValue(g->eoClips, m);
	CGPathRelease(m);
	og_apply(g);
}

void og_clip_reset(OG *g) {
	CFArrayRemoveAllValues(g->clips);
	CFArrayRemoveAllValues(g->eoClips);
	og_apply(g);
}

// og_soft_begin: get canvas s (k times the size of g) ready to take g's drawing — g's transform
// and clip, scaled by k. Used for the soft layer under a widget: glows and light that look the
// same drawn at a quarter of the pixels and stretched by the GPU.
void og_soft_begin(OG *s, OG *g, float k) {
	CGAffineTransform K = CGAffineTransformMakeScale(k, k);
	s->T = CGAffineTransformConcat(g->T, K);
	CFArrayRemoveAllValues(s->clips);
	CFArrayRemoveAllValues(s->eoClips);
	for (int pass = 0; pass < 2; pass++) {
		CFArrayRef from = pass ? g->eoClips : g->clips;
		for (CFIndex i = 0; i < CFArrayGetCount(from); i++) {
			CGPathRef c = CGPathCreateCopyByTransformingPath((CGPathRef)CFArrayGetValueAtIndex(from, i), &K);
			CFArrayAppendValue(pass ? s->eoClips : s->clips, c);
			CGPathRelease(c);
		}
	}
	s->copyMode = 0;
	s->interp = 3;
	og_apply(s);
}

// og_blur: blur the whole canvas (three box blurs ≈ a gaussian of that radius, in pixels)
void og_blur(OG *g, float radius) {
	if (radius < 0.75f) return;
	uint32_t k = (uint32_t)lroundf(radius * 1.16f) | 1; // 3 box passes of this width ≈ gaussian sigma radius/1.6
	if (k < 3) k = 3;
	vImage_Buffer a = {g->bits, (vImagePixelCount)g->h, (vImagePixelCount)g->w, (size_t)g->w * 4};
	void *tmp = malloc((size_t)g->w * g->h * 4);
	if (!tmp) return;
	vImage_Buffer b = {tmp, (vImagePixelCount)g->h, (vImagePixelCount)g->w, (size_t)g->w * 4};
	vImageBoxConvolve_ARGB8888(&a, &b, NULL, 0, 0, k, k, NULL, kvImageEdgeExtend);
	vImageBoxConvolve_ARGB8888(&b, &a, NULL, 0, 0, k, k, NULL, kvImageEdgeExtend);
	vImageBoxConvolve_ARGB8888(&a, &b, NULL, 0, 0, k, k, NULL, kvImageEdgeExtend);
	memcpy(g->bits, tmp, (size_t)g->w * g->h * 4);
	free(tmp);
}

void og_clear(OG *g, uint32_t argb) {
	if (argb == 0 && CFArrayGetCount(g->clips) == 0 && CFArrayGetCount(g->eoClips) == 0) { // all of it: plain memory
		size_t row = g->stride ? g->stride : (size_t)g->w * 4;
		memset(g->bits, 0, row * g->h);
		return;
	}
	CGContextSaveGState(g->ctx);
	CGContextSetBlendMode(g->ctx, kCGBlendModeCopy);
	setFill(g->ctx, argb);
	CGContextFillRect(g->ctx, CGContextGetClipBoundingBox(g->ctx));
	CGContextRestoreGState(g->ctx);
}

/* ---------- shapes ---------- */

void og_fill_ellipse(OG *g, uint32_t c, float x, float y, float w, float h) { setFill(g->ctx, c); CGContextFillEllipseInRect(g->ctx, CGRectMake(x, y, w, h)); }
void og_fill_rect(OG *g, uint32_t c, float x, float y, float w, float h) { setFill(g->ctx, c); CGContextFillRect(g->ctx, CGRectMake(x, y, w, h)); }

static void addArc(CGMutablePathRef p, float x, float y, float w, float h, float start, float sweep, int move) {
	if (w <= 0 || h <= 0) return;
	CGAffineTransform m = CGAffineTransformMake(w / 2, 0, 0, h / 2, x + w / 2, y + h / 2); // unit circle → the ellipse
	CGFloat a0 = start * M_PI / 180, a1 = (start + sweep) * M_PI / 180;
	if (move) CGPathMoveToPoint(p, &m, cos(a0), sin(a0));
	CGPathAddArc(p, &m, 0, 0, 1, a0, a1, sweep < 0); // y is down: growing angles turn clockwise, like GDI+
}

void og_fill_pie(OG *g, uint32_t c, float x, float y, float w, float h, float start, float sweep) {
	CGMutablePathRef p = CGPathCreateMutable();
	CGPathMoveToPoint(p, NULL, x + w / 2, y + h / 2);
	addArc(p, x, y, w, h, start, sweep, 0);
	CGPathCloseSubpath(p);
	setFill(g->ctx, c);
	CGContextAddPath(g->ctx, p);
	CGContextFillPath(g->ctx);
	CGPathRelease(p);
}

void og_fill_poly(OG *g, uint32_t c, const float *xy, int n) {
	if (n < 3) return;
	CGContextBeginPath(g->ctx);
	CGContextMoveToPoint(g->ctx, xy[0], xy[1]);
	for (int i = 1; i < n; i++) CGContextAddLineToPoint(g->ctx, xy[2 * i], xy[2 * i + 1]);
	CGContextClosePath(g->ctx);
	setFill(g->ctx, c);
	CGContextEOFillPath(g->ctx); // GDI+ FillPolygon default: alternate
}

/* ---------- pens ---------- */

OPen *og_pen(uint32_t c, float width) {
	OPen *p = calloc(1, sizeof(OPen));
	p->r = ((c >> 16) & 255) / 255.0;
	p->g = ((c >> 8) & 255) / 255.0;
	p->b = (c & 255) / 255.0;
	p->a = (c >> 24) / 255.0;
	p->width = width;
	return p;
}
void og_pen_free(OPen *p) { free(p); }
void og_pen_dash(OPen *p) { p->dash = 1; }

static void usePen(OG *g, OPen *p) {
	CGContextSetRGBStrokeColor(g->ctx, p->r, p->g, p->b, p->a);
	CGContextSetLineWidth(g->ctx, p->width);
	if (p->dash) {
		CGFloat d[2] = {p->width * 3, p->width}; // GDI+ DashStyleDash: 3 on, 1 off (in pen widths)
		CGContextSetLineDash(g->ctx, 0, d, 2);
	} else {
		CGContextSetLineDash(g->ctx, 0, NULL, 0);
	}
}

void og_line(OG *g, OPen *p, float x1, float y1, float x2, float y2) {
	usePen(g, p);
	CGContextBeginPath(g->ctx);
	CGContextMoveToPoint(g->ctx, x1, y1);
	CGContextAddLineToPoint(g->ctx, x2, y2);
	CGContextStrokePath(g->ctx);
}

void og_stroke_ellipse(OG *g, OPen *p, float x, float y, float w, float h) { usePen(g, p); CGContextStrokeEllipseInRect(g->ctx, CGRectMake(x, y, w, h)); }

void og_arc(OG *g, OPen *p, float x, float y, float w, float h, float start, float sweep) {
	CGMutablePathRef m = CGPathCreateMutable();
	addArc(m, x, y, w, h, start, sweep, 1);
	usePen(g, p);
	CGContextAddPath(g->ctx, m);
	CGContextStrokePath(g->ctx);
	CGPathRelease(m);
}

/* ---------- paths ---------- */

OPath *og_path(void) { OPath *p = calloc(1, sizeof(OPath)); p->p = CGPathCreateMutable(); return p; }
void og_path_free(OPath *p) { if (p) { CGPathRelease(p->p); free(p); } }
void og_path_winding(OPath *p) { p->winding = 1; }
void og_path_ellipse(OPath *p, float x, float y, float w, float h) { CGPathAddEllipseInRect(p->p, NULL, CGRectMake(x, y, w, h)); }
void og_path_add(OPath *p, OPath *o) { CGPathAddPath(p->p, NULL, o->p); }

void og_path_round_rect(OPath *p, float x, float y, float w, float h, float r) {
	float d = 2 * r;
	if (d > w) d = w;
	if (d > h) d = h;
	if (w <= 0 || h <= 0) return;
	if (d <= 0) { CGPathAddRect(p->p, NULL, CGRectMake(x, y, w, h)); return; }
	// the same four arcs GDI+ gets
	addArc(p->p, x, y, d, d, 180, 90, 1);
	addArc(p->p, x + w - d, y, d, d, 270, 90, 0);
	addArc(p->p, x + w - d, y + h - d, d, d, 0, 90, 0);
	addArc(p->p, x, y + h - d, d, d, 90, 90, 0);
	CGPathCloseSubpath(p->p);
}

void og_path_bounds(OPath *p, float *out) {
	CGRect r = CGPathIsEmpty(p->p) ? CGRectZero : CGPathGetPathBoundingBox(p->p);
	out[0] = r.origin.x; out[1] = r.origin.y; out[2] = r.size.width; out[3] = r.size.height;
}

void og_fill_path(OG *g, OPath *p, uint32_t c) {
	setFill(g->ctx, c);
	CGContextAddPath(g->ctx, p->p);
	if (p->winding) CGContextFillPath(g->ctx); else CGContextEOFillPath(g->ctx);
}

// og_fill_path_glow: the path filled with a soft glow around it (Core Graphics' own blurred
// shadow — one pass, instead of stroking the outlines of every letter several times)
void og_fill_path_glow(OG *g, OPath *p, uint32_t fill, uint32_t glow, float blur) {
	CGContextSaveGState(g->ctx);
	CGColorRef gc = color(glow);
	CGContextSetShadowWithColor(g->ctx, CGSizeZero, blur, gc);
	CGColorRelease(gc);
	og_fill_path(g, p, fill);
	CGContextRestoreGState(g->ctx);
}

void og_stroke_path(OG *g, OPath *p, OPen *pen) {
	usePen(g, pen);
	CGContextAddPath(g->ctx, p->p);
	CGContextStrokePath(g->ctx);
}

/* ---------- gradients ---------- */

static CGGradientRef grad2(uint32_t c0, uint32_t c1) {
	CGColorRef a = color(c0), b = color(c1);
	CFArrayRef cols = CFArrayCreate(NULL, (const void *[]){a, b}, 2, &kCFTypeArrayCallBacks);
	CGGradientRef gr = CGGradientCreateWithColors(rgb(), cols, NULL);
	CFRelease(cols);
	CGColorRelease(a);
	CGColorRelease(b);
	return gr;
}

void og_fill_path_vgrad(OG *g, OPath *p, uint32_t top, uint32_t bottom, float y0, float y1) {
	CGContextSaveGState(g->ctx);
	CGContextAddPath(g->ctx, p->p);
	if (p->winding) CGContextClip(g->ctx); else CGContextEOClip(g->ctx);
	CGGradientRef gr = grad2(top, bottom);
	CGContextDrawLinearGradient(g->ctx, gr, CGPointMake(0, y0), CGPointMake(0, y1), kCGGradientDrawsBeforeStartLocation | kCGGradientDrawsAfterEndLocation);
	CGGradientRelease(gr);
	CGContextRestoreGState(g->ctx);
}

void og_fill_rect_grad(OG *g, float x, float y, float w, float h, float x0, float y0, float x1, float y1, uint32_t c0, uint32_t c1) {
	CGContextSaveGState(g->ctx);
	CGContextClipToRect(g->ctx, CGRectMake(x, y, w, h));
	CGGradientRef gr = grad2(c0, c1);
	CGContextDrawLinearGradient(g->ctx, gr, CGPointMake(x0, y0), CGPointMake(x1, y1), kCGGradientDrawsBeforeStartLocation | kCGGradientDrawsAfterEndLocation);
	CGGradientRelease(gr);
	CGContextRestoreGState(g->ctx);
}

// radial: an ellipse fading from the centre colour to the surround colour at its edge
void og_radial(OG *g, float cx, float cy, float rx, float ry, uint32_t center, uint32_t surround) {
	if (rx <= 0 || ry <= 0) return;
	CGContextSaveGState(g->ctx);
	CGContextTranslateCTM(g->ctx, cx, cy);
	CGContextScaleCTM(g->ctx, rx, ry);
	CGGradientRef gr = grad2(center, surround);
	CGContextDrawRadialGradient(g->ctx, gr, CGPointZero, 0, CGPointZero, 1, 0);
	CGGradientRelease(gr);
	CGContextRestoreGState(g->ctx);
}

/* ---------- images ---------- */

// flatten: decode a picture once into the canvases' own pixel format (device RGB, premultiplied
// BGRA). A JPEG left as it is gets decoded and colour-converted again every time it is drawn —
// album art is drawn every frame. Very large pictures are kept at 2048 px (more is never shown).
static CGImageRef flatten(CGImageRef img) { // takes img, returns +1
	size_t w = CGImageGetWidth(img), h = CGImageGetHeight(img);
	if (w == 0 || h == 0) return img;
	double k = fmin(1.0, 2048.0 / (double)(w > h ? w : h));
	size_t W = (size_t)fmax(1, round(w * k)), H = (size_t)fmax(1, round(h * k));
	CGContextRef c = CGBitmapContextCreate(NULL, W, H, 8, W * 4, rgb(), kCGImageAlphaPremultipliedFirst | kCGBitmapByteOrder32Little);
	if (!c) return img;
	CGContextSetInterpolationQuality(c, kCGInterpolationHigh);
	CGContextDrawImage(c, CGRectMake(0, 0, W, H), img);
	CGImageRef out = CGBitmapContextCreateImage(c);
	CGContextRelease(c);
	if (!out) return img;
	CGImageRelease(img);
	return out;
}

OImg *og_img_from_bytes(const void *b, int n) {
	CFDataRef d = CFDataCreate(NULL, b, n);
	CGImageSourceRef src = CGImageSourceCreateWithData(d, NULL);
	CFRelease(d);
	if (!src) return NULL;
	CGImageRef img = CGImageSourceCreateImageAtIndex(src, 0, NULL);
	CFRelease(src);
	if (!img) return NULL;
	OImg *o = calloc(1, sizeof(OImg));
	o->img = flatten(img);
	liveImg++;
	liveImgBytes += (long)CGImageGetBytesPerRow(o->img) * CGImageGetHeight(o->img);
	return o;
}

OImg *og_img_from_file(const char *path) {
	CFURLRef u = CFURLCreateFromFileSystemRepresentation(NULL, (const UInt8 *)path, strlen(path), false);
	CGImageSourceRef src = CGImageSourceCreateWithURL(u, NULL);
	CFRelease(u);
	if (!src) return NULL;
	CGImageRef img = CGImageSourceCreateImageAtIndex(src, 0, NULL);
	CFRelease(src);
	if (!img) return NULL;
	OImg *o = calloc(1, sizeof(OImg));
	o->img = flatten(img);
	liveImg++;
	liveImgBytes += (long)CGImageGetBytesPerRow(o->img) * CGImageGetHeight(o->img);
	return o;
}

// a canvas used as an image (GDI+ bitmaps are images too)
OImg *og_img_of(OG *g) { OImg *o = calloc(1, sizeof(OImg)); o->og = g; return o; }
OG *og_img_og(OImg *o) { return o ? o->og : NULL; }

void og_img_free(OImg *o) {
	if (!o) return;
	if (o->img) { liveImg--; liveImgBytes -= (long)CGImageGetBytesPerRow(o->img) * CGImageGetHeight(o->img); CGImageRelease(o->img); }
	free(o);
}

static CGImageRef imageNow(OImg *o) { // +1
	if (o->og) return CGBitmapContextCreateImage(o->og->ctx);
	return CGImageRetain(o->img);
}

void og_img_size(OImg *o, int *w, int *h) {
	if (o->og) { *w = o->og->w; *h = o->og->h; return; }
	*w = (int)CGImageGetWidth(o->img);
	*h = (int)CGImageGetHeight(o->img);
}

void og_draw_image(OG *g, OImg *o, float x, float y, float w, float h, float sx, float sy, float sw, float sh, int part) {
	CGImageRef img = imageNow(o);
	if (!img) return;
	if (part) {
		CGImageRef sub = CGImageCreateWithImageInRect(img, CGRectMake(sx, sy, sw, sh));
		CGImageRelease(img);
		img = sub;
		if (!img) return;
	}
	CGContextSaveGState(g->ctx);
	if (g->interp < 7) { // drawn at about its own size (turned or not): the cheap filter looks the same
		CGAffineTransform m = CGContextGetCTM(g->ctx);
		double sx = sqrt(m.a * m.a + m.b * m.b) * w / CGImageGetWidth(img), sy = sqrt(m.c * m.c + m.d * m.d) * h / CGImageGetHeight(img);
		if (sx > 0.8 && sx < 1.25 && sy > 0.8 && sy < 1.25) CGContextSetInterpolationQuality(g->ctx, kCGInterpolationLow);
	}
	CGContextTranslateCTM(g->ctx, x, y + h); // images are drawn bottom-up: flip back for this one
	CGContextScaleCTM(g->ctx, 1, -1);
	CGContextDrawImage(g->ctx, CGRectMake(0, 0, w, h), img);
	CGContextRestoreGState(g->ctx);
	CGImageRelease(img);
}

void og_draw_image_alpha(OG *g, OImg *o, float x, float y, float w, float h, float alpha) {
	CGContextSaveGState(g->ctx);
	CGContextSetAlpha(g->ctx, alpha);
	og_draw_image(g, o, x, y, w, h, 0, 0, 0, 0, 0);
	CGContextRestoreGState(g->ctx);
}

int og_save_png(OImg *o, const char *path) {
	CGImageRef img = imageNow(o);
	if (!img) return 0;
	CFURLRef u = CFURLCreateFromFileSystemRepresentation(NULL, (const UInt8 *)path, strlen(path), false);
	CGImageDestinationRef d = CGImageDestinationCreateWithURL(u, CFSTR("public.png"), 1, NULL);
	CFRelease(u);
	int ok = 0;
	if (d) {
		CGImageDestinationAddImage(d, img, NULL);
		ok = CGImageDestinationFinalize(d);
		CFRelease(d);
	}
	CGImageRelease(img);
	return ok;
}

void *og_cgimage(OG *g) { return (void *)CGBitmapContextCreateImage(g->ctx); } // for windows (+1)

/* ---------- fonts and text ---------- */

static CFStringRef cfstr(const char *s, int n) { return CFStringCreateWithBytes(NULL, (const UInt8 *)s, n, kCFStringEncodingUTF8, false); }

OFont *og_font(const char *name, float size, int bold) {
	NSString *n = [NSString stringWithUTF8String:name];
	NSFont *f = nil;
	NSFontWeight wt = bold ? NSFontWeightBold : NSFontWeightRegular;
	// Windows' own fonts → the Mac's closest
	if ([n hasPrefix:@"Segoe UI"] || [n isEqualToString:@"Bahnschrift"] || [n isEqualToString:@"Arial"] || n.length == 0) {
		if ([n containsString:@"Semibold"]) wt = bold ? NSFontWeightBold : NSFontWeightSemibold;
		if ([n containsString:@"Light"]) wt = NSFontWeightLight;
		if ([n containsString:@"Black"]) wt = NSFontWeightBlack;
		f = [NSFont systemFontOfSize:size weight:wt];
	} else if ([n isEqualToString:@"Consolas"] || [n isEqualToString:@"Courier New"] || [n isEqualToString:@"Cascadia Mono"]) {
		f = [NSFont monospacedSystemFontOfSize:size weight:wt];
	} else {
		f = [[NSFontManager sharedFontManager] fontWithFamily:n traits:(bold ? NSBoldFontMask : 0) weight:5 size:size];
		if (!f) f = [NSFont fontWithName:n size:size];
		if (f && bold) f = [[NSFontManager sharedFontManager] convertFont:f toHaveTrait:NSBoldFontMask];
		if (!f) f = [NSFont systemFontOfSize:size weight:wt];
	}
	OFont *o = calloc(1, sizeof(OFont));
	o->ct = (CTFontRef)CFBridgingRetain(f);
	o->size = size;
	o->ascent = CTFontGetAscent(o->ct);
	o->descent = CTFontGetDescent(o->ct);
	o->leading = CTFontGetLeading(o->ct);
	return o;
}

void og_font_free(OFont *f) { if (f) { CFRelease(f->ct); free(f); } }

static CTLineRef makeLine(OFont *f, const char *s, int n) {
	CFStringRef str = cfstr(s, n);
	if (!str) return NULL;
	CFStringRef keys[] = {kCTFontAttributeName, kCTForegroundColorFromContextAttributeName};
	CFTypeRef vals[] = {f->ct, kCFBooleanTrue};
	CFDictionaryRef at = CFDictionaryCreate(NULL, (const void **)keys, (const void **)vals, 2, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFAttributedStringRef as = CFAttributedStringCreate(NULL, str, at);
	CTLineRef l = CTLineCreateWithAttributedString(as);
	CFRelease(as);
	CFRelease(at);
	CFRelease(str);
	return l;
}

void og_measure(OFont *f, const char *s, int n, float *w, float *h) {
	*h = f->ascent + f->descent + f->leading;
	*w = 0;
	if (n == 0) return;
	CTLineRef l = makeLine(f, s, n);
	if (!l) return;
	*w = CTLineGetTypographicBounds(l, NULL, NULL, NULL);
	CFRelease(l);
}

// og_text: one line with its top at y; cut with "…" when wider than maxW; align 0 left, 2 right
void og_text(OG *g, OFont *f, const char *s, int n, float x, float y, float maxW, uint32_t c, int align) {
	if (n == 0) return;
	CTLineRef l = makeLine(f, s, n);
	if (!l) return;
	double w = CTLineGetTypographicBounds(l, NULL, NULL, NULL);
	if (maxW > 0 && w > maxW + 0.5) {
		CTLineRef dots = makeLine(f, "\xE2\x80\xA6", 3);
		CTLineRef cut = CTLineCreateTruncatedLine(l, maxW, kCTLineTruncationEnd, dots);
		if (dots) CFRelease(dots);
		if (cut) { CFRelease(l); l = cut; w = CTLineGetTypographicBounds(l, NULL, NULL, NULL); }
	}
	if (align == 2) x = x + maxW - w;
	setFill(g->ctx, c);
	CGContextSetTextPosition(g->ctx, x, y + f->ascent);
	CTLineDraw(l, g->ctx);
	CFRelease(l);
}

// og_text_path: the outlines of a line of text (top at y) added to a path — for glowing lyrics
void og_text_path(OPath *p, OFont *f, const char *s, int n, float x, float y) {
	if (n == 0) return;
	CTLineRef l = makeLine(f, s, n);
	if (!l) return;
	CFArrayRef runs = CTLineGetGlyphRuns(l);
	for (CFIndex r = 0; r < CFArrayGetCount(runs); r++) {
		CTRunRef run = CFArrayGetValueAtIndex(runs, r);
		CTFontRef rf = CFDictionaryGetValue(CTRunGetAttributes(run), kCTFontAttributeName);
		CFIndex gn = CTRunGetGlyphCount(run);
		CGGlyph *gl = malloc(sizeof(CGGlyph) * gn);
		CGPoint *pos = malloc(sizeof(CGPoint) * gn);
		CTRunGetGlyphs(run, CFRangeMake(0, 0), gl);
		CTRunGetPositions(run, CFRangeMake(0, 0), pos);
		for (CFIndex i = 0; i < gn; i++) {
			// glyph space is y-up: flip it into our y-down space at the baseline
			CGAffineTransform m = CGAffineTransformMake(1, 0, 0, -1, x + pos[i].x, y + f->ascent - pos[i].y);
			CGPathRef gp = CTFontCreatePathForGlyph(rf ? rf : f->ct, gl[i], &m);
			if (gp) { CGPathAddPath(p->p, NULL, gp); CGPathRelease(gp); }
		}
		free(gl);
		free(pos);
	}
	CFRelease(l);
}

// og_blend: put a whole canvas onto another at (x, y) with an opacity, ignoring the target's
// transform and clip (like GDI's AlphaBlend onto the DC)
void og_blend(OG *dst, OG *src, float x, float y, float alpha) {
	CGImageRef img = CGBitmapContextCreateImage(src->ctx);
	if (!img) return;
	CGContextRestoreGState(dst->ctx); // back to the bare base state
	CGContextSaveGState(dst->ctx);
	CGContextSetAlpha(dst->ctx, alpha);
	CGContextSetBlendMode(dst->ctx, kCGBlendModeNormal);
	CGContextTranslateCTM(dst->ctx, x, y + src->h);
	CGContextScaleCTM(dst->ctx, 1, -1);
	CGContextDrawImage(dst->ctx, CGRectMake(0, 0, src->w, src->h), img);
	CGImageRelease(img);
	og_apply(dst); // the clip and transform as they were
}
