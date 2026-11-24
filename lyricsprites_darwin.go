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
	"unsafe"
)

// Lyrics as pictures (the Mac): a Retina screen has four times the pixels, and redrawing every
// word with its glow 60 times a second cost most of Orbit's CPU. Each word is now drawn once — its
// crisp letters and its glow as two pictures — and the GPU places, turns, scales and fades them.

type sprite struct {
	img, glow unsafe.Pointer // CGImages (+1)
	w, h, m   float32        // picture size and the margin around the text (canvas pixels)
	tw, th    float32        // the text's own size
	used      int
}

var (
	sprites     = map[string]*sprite{}
	spriteFrame int
)

func (sp *sprite) free() {
	C.os_cgimage_release(sp.img)
	C.os_cgimage_release(sp.glow)
	sp.img, sp.glow = nil, nil
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
// picture's own soft layer (blurred), the letters to the picture.
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
	fake := &Widget{name: "sprite", w: W, h: H, softK: 0.5 * scale, offscreen: true, canvas: cv}
	canvasOwner[cv.g] = fake
	rr := r
	rr.x, rr.y, rr.dx, rr.dy, rr.rot, rr.rotC, rr.scale, rr.alpha = 0, 0, 0, 0, 0, false, 1, 1
	drawRun(cv.g, rr, m, m, F, fx)
	gReset(cv.g)
	delete(canvasOwner, cv.g)
	sp := &sprite{w: float32(W), h: float32(H), m: m, tw: tw, th: th}
	sp.img = C.og_cgimage((*C.OG)(unsafe.Pointer(cv.g)))
	if fake.soft != nil {
		if fake.softUsed {
			if fake.softBlurR > 0 {
				C.og_blur((*C.OG)(unsafe.Pointer(fake.soft.g)), C.float(float64(fake.softBlurR)*float64(fake.soft.w)/float64(W)))
			}
			sp.glow = C.og_cgimage((*C.OG)(unsafe.Pointer(fake.soft.g)))
		}
		fake.soft.Free()
	}
	cv.Free()
	return sp
}

// spriteRuns shows the runs it can as pictures and returns the others (drawn on the canvas).
// runs == nil: no pictures.
func (w *Widget) spriteRuns(runs []run, ox, oy float32, F float64, fxFor func(run) int) []run {
	spriteFrame++
	var rest []run
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
			if sp = makeSprite(r, F, fx); sp == nil {
				rest = append(rest, r)
				continue
			}
			sprites[k] = sp
		}
		sp.used = spriteFrame
		x, y := ox+r.x+r.dx, oy+r.y+r.dy
		d := C.OSprite{img: sp.img, glow: sp.glow, w: C.double(sp.w), h: C.double(sp.h), sx: 1, sy: 1, alpha: C.double(r.alpha)}
		sc := float64(r.scale)
		if sc == 0 {
			sc = 1
		}
		switch {
		case r.rotC: // turned (and scaled) around the word's centre
			d.x, d.y, d.ax, d.ay = C.double(x+sp.tw/2), C.double(y+sp.th/2), 0.5, 0.5
			d.rot, d.sx, d.sy = C.double(r.rot), C.double(sc), C.double(sc)
		case r.rot != 0: // turned around its top-left corner
			d.x, d.y = C.double(x), C.double(y)
			d.ax, d.ay = C.double(sp.m/sp.w), C.double(sp.m/sp.h)
			d.rot = C.double(r.rot)
		default: // scaled around its centre (or not at all)
			d.x, d.y, d.ax, d.ay = C.double(x+sp.tw/2), C.double(y+sp.th/2), 0.5, 0.5
			d.sx, d.sy = C.double(sc), C.double(sc)
		}
		out = append(out, d)
	}
	if len(out) == 0 {
		C.ow_sprites(unsafe.Pointer(w.win), 0, nil)
	} else {
		C.ow_sprites(unsafe.Pointer(w.win), C.int(len(out)), &out[0])
	}
	w.noMain = runs != nil && len(rest) == 0
	if len(sprites) > 160 { // forget words not shown for a few seconds
		for k, sp := range sprites {
			if spriteFrame-sp.used > 180 {
				sp.free()
				delete(sprites, k)
			}
		}
	}
	return rest
}
