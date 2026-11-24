//go:build darwin

package main

/*
#cgo CFLAGS: -fobjc-arc -Wno-deprecated-declarations
#include <stdlib.h>
#include "gdip_darwin.h"
#include "app_darwin.h"
*/
import "C"

import (
	"fmt"
	"math"
	"time"
	"unsafe"
)

// Lyrics as pictures (the Mac): a Retina screen has four times the pixels, and redrawing every
// word with its glow 60 times a second cost most of Orbit's CPU. Each word is now drawn once — its
// crisp letters and its glow as two pictures — and the GPU places, turns, scales and fades them.

type sprite struct {
	img, glow unsafe.Pointer // CGImages (+1), each cropped to what is on it
	ir, gr    [4]float32     // where they sit in the drawing box (x, y, w, h; canvas pixels)
	m         float32        // the margin the text was drawn at in that box
	tw, th    float32        // the text's own size
	bytes     int
	used      time.Time
}

var (
	sprites     = map[string]*sprite{}
	spriteBytes int
)

func (sp *sprite) free() {
	C.os_cgimage_release(sp.img)
	C.os_cgimage_release(sp.glow)
	sp.img, sp.glow = nil, nil
	spriteBytes -= sp.bytes
}

// trimSprites keeps the newest words: at most 40, none unused for 3 s, under 48 MB
func trimSprites(now time.Time) {
	for len(sprites) > 0 {
		var oldK string
		var old *sprite
		for k, sp := range sprites {
			if old == nil || sp.used.Before(old.used) {
				oldK, old = k, sp
			}
		}
		if len(sprites) <= 40 && now.Sub(old.used) < 3*time.Second && spriteBytes < 48<<20 {
			return
		}
		if now.Sub(old.used) < 50*time.Millisecond { // all on screen now
			return
		}
		old.free()
		delete(sprites, oldK)
	}
}

// spriteable: a run the GPU can show as a picture — plain text, the whole word bright, no
// effect that changes inside the word from frame to frame
func spriteable(r run) bool {
	return r.kind == rkText && !r.caret && r.chroma == 0 && r.bg == 0 && (r.fill <= 0 || r.fill >= 1) && r.haze <= 0 && lyrHaze <= 0 && r.text != ""
}

func spriteKey(r run, fx int) string {
	return fmt.Sprintf("%s\x00%s|%08x|%d|%v|%.3f|%.3f|%08x", r.text, r.f.key, r.color, fx, r.hollow, cfg.GlowAmount, lyrMotion, accent())
}

// makeSprite draws run r alone (unmoved, fully opaque) with its effect: the glow goes to the
// picture's own soft layer (a quarter of the pixels, blurred), the letters to the picture; both
// are then cut down to what is on them.
func makeSprite(r run, F float64, fx int) *sprite {
	tw, th := measure(r.f, r.text)
	size := float64(r.f.size)
	amt := cfg.GlowAmount
	if cfg.Motion == moGlow {
		amt = amt*(1+float64(lyrMotion)*1.4) + float64(lyrMotion)
	}
	m := float32(math.Ceil(size*(0.32*amt+0.12) + 6))
	W, H := int(math.Ceil(float64(tw+2*m))), int(math.Ceil(float64(th+2*m)))
	if W*H > 4096*2048 {
		return nil
	}
	cv := newCanvas(W, H)
	if cv == nil {
		return nil
	}
	scale := float64(C.os_scale())
	fake := &Widget{name: "sprite", w: W, h: H, softK: 0.25 * scale, offscreen: true, canvas: cv}
	canvasOwner[cv.g] = fake
	rr := r
	rr.x, rr.y, rr.dx, rr.dy, rr.rot, rr.rotC, rr.scale, rr.alpha = 0, 0, 0, 0, 0, false, 1, 1
	drawRun(cv.g, rr, m, m, F, fx)
	gReset(cv.g)
	delete(canvasOwner, cv.g)
	sp := &sprite{m: m, tw: tw, th: th}
	var rc [4]C.int
	sp.img = C.og_cgimage_crop((*C.OG)(unsafe.Pointer(cv.g)), &rc[0])
	sp.ir = [4]float32{float32(rc[0]), float32(rc[1]), float32(rc[2]), float32(rc[3])}
	sp.bytes = int(rc[2]) * int(rc[3]) * 4
	if fake.soft != nil {
		if fake.softUsed {
			k := float32(fake.soft.w) / float32(W)
			if fake.softBlurR > 0 {
				C.og_blur((*C.OG)(unsafe.Pointer(fake.soft.g)), C.float(fake.softBlurR*k))
			}
			sp.glow = C.og_cgimage_crop((*C.OG)(unsafe.Pointer(fake.soft.g)), &rc[0])
			if sp.glow != nil {
				sp.gr = [4]float32{float32(rc[0]) / k, float32(rc[1]) / k, float32(rc[2]) / k, float32(rc[3]) / k}
				sp.bytes += int(rc[2]) * int(rc[3]) * 4
			}
		}
		fake.soft.Free()
	}
	cv.Free()
	if sp.img == nil && sp.glow == nil {
		return nil
	}
	spriteBytes += sp.bytes
	return sp
}

// spriteRuns shows the runs it can as pictures and returns the others (drawn on the canvas).
// runs == nil: no pictures.
func (w *Widget) spriteRuns(runs []run, ox, oy float32, F float64, fxFor func(run) int) []run {
	now := time.Now()
	var rest []run
	made := now
	budget := 0 // words drawn this frame
	var out []C.OSprite
	for _, r := range runs {
		if !spriteable(r) || r.alpha <= 0.01 {
			if !spriteable(r) {
				rest = append(rest, r)
			}
			continue
		}
		fx := fxFor(r)
		k := spriteKey(r, fx)
		sp := sprites[k]
		if sp == nil {
			// a new line brings several words at once: drawing them all in one frame would make
			// it late. Past 4 ms the rest wait for the next frame (they are just appearing).
			if budget > 0 && time.Since(made) > 4*time.Millisecond {
				continue
			}
			budget++
			if sp = makeSprite(r, F, fx); sp == nil {
				rest = append(rest, r)
				continue
			}
			sprites[k] = sp
		}
		sp.used = now
		x, y := ox+r.x+r.dx, oy+r.y+r.dy
		d := C.OSprite{img: sp.img, glow: sp.glow, sx: 1, sy: 1, alpha: C.double(r.alpha)}
		sc := float64(r.scale)
		if sc == 0 {
			sc = 1
		}
		// the turning point, in the drawing box and in the window
		px, py := sp.m+sp.tw/2, sp.m+sp.th/2
		switch {
		case r.rotC: // turned (and scaled) around the word's centre
			d.rot, d.sx, d.sy = C.double(r.rot), C.double(sc), C.double(sc)
		case r.rot != 0: // turned around its top-left corner
			px, py = sp.m, sp.m
			d.rot = C.double(r.rot)
		default: // scaled around its centre (or not at all)
			d.sx, d.sy = C.double(sc), C.double(sc)
		}
		d.x, d.y = C.double(x-sp.m+px), C.double(y-sp.m+py)
		anchor := func(rc [4]float32) (C.double, C.double, C.double, C.double) {
			if rc[2] <= 0 || rc[3] <= 0 {
				return 0, 0, 0, 0
			}
			return C.double(rc[2]), C.double(rc[3]), C.double((px - rc[0]) / rc[2]), C.double((py - rc[1]) / rc[3])
		}
		d.iw, d.ih, d.iax, d.iay = anchor(sp.ir)
		d.gw, d.gh, d.gax, d.gay = anchor(sp.gr)
		out = append(out, d)
	}
	if len(out) == 0 {
		C.ow_sprites(unsafe.Pointer(w.win), 0, nil)
	} else {
		C.ow_sprites(unsafe.Pointer(w.win), C.int(len(out)), &out[0])
	}
	w.noMain = runs != nil && len(rest) == 0
	trimSprites(now)
	return rest
}
