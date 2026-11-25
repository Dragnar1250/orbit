//go:build darwin

package main

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// TestDrawCosts (ORBIT_DRAWBENCH=1): what each kind of drawing costs at Retina sizes on this Mac,
// to choose how the widgets draw. Prints a table; never fails.
func TestDrawCosts(t *testing.T) {
	if os.Getenv("ORBIT_DRAWBENCH") == "" {
		t.Skip("ORBIT_DRAWBENCH not set")
	}
	cv := newCanvas(1400, 500)
	g := cv.g
	f := newFont("Helvetica Neue", 150, true)
	words := []string{"YOU", "HELP", "ME", "REARRANGE"}
	paths := func(x0 float32) []uintptr {
		var ps []uintptr
		x := x0
		for _, w := range words {
			p := newPath()
			addTextPath(p, f, w, x, 120)
			ww, _ := measure(f, w)
			x += ww + 40
			ps = append(ps, p)
		}
		return ps
	}
	ps := paths(40)
	time1 := func(name string, n int, fn func()) {
		t0 := time.Now()
		for i := 0; i < n; i++ {
			fn()
		}
		fmt.Printf("DRAWCOST %-44s %7.3f ms\n", name, float64(time.Since(t0).Microseconds())/1000/float64(n))
	}
	time1("build 4 word paths (CoreText outlines)", 50, func() {
		for _, p := range paths(40) {
			freePath(p)
		}
	})
	time1("clear 1400x500", 50, func() { gClear(g, 0) })
	time1("fill 4 words", 50, func() {
		for _, p := range ps {
			fillPathColor(g, p, 0xFFFFFFFF)
		}
	})
	time1("soft glow (shadow + 3 strokes + fill) 4 words", 20, func() {
		for _, p := range ps {
			drawGlowPath(g, p, 0xFFFFFFFF, 0xFFFFFFFF, 150*0.22, true)
		}
	})
	time1("strong glow (4 strokes + fill) 4 words", 20, func() {
		for _, p := range ps {
			for _, wm := range []float32{1, 0.65, 0.35, 0.15} {
				strokePath(g, p, 0x22FFFFFF, 75*wm+1)
			}
			fillPathColor(g, p, 0xFFFFFFFF)
		}
	})
	time1("blurred-shadow glow (1 fill) 4 words", 20, func() {
		for _, p := range ps {
			fillPathGlow(g, p, 0xFFFFFFFF, 0xB0FFFFFF, 30)
		}
	})
	// sprites: each word drawn once into its own bitmap, then only copied
	var sprites []*Bitmap
	for _, w := range words {
		ww, _ := measure(f, w)
		b := newBitmap(int(ww)+120, 300)
		p := newPath()
		addTextPath(p, f, w, 60, 60)
		drawGlowPath(b.g, p, 0xFFFFFFFF, 0xFFFFFFFF, 150*0.22, true)
		freePath(p)
		sprites = append(sprites, b)
	}
	time1("sprites: 4 words copied", 50, func() {
		x := float32(0)
		for _, b := range sprites {
			gImage(g, b.bmp, x, 60, float32(b.w), float32(b.h))
			x += float32(b.w)
		}
	})
	time1("sprites: 4 words rotated + scaled + alpha", 50, func() {
		x := float32(0)
		for _, b := range sprites {
			gTranslate(g, x+float32(b.w)/2, 200)
			gRotate(g, 12)
			gScale(g, 1.1, 1.1)
			gImageAlpha(g, b.bmp, b.w, b.h, -float32(b.w)/2, -150, float32(b.w), float32(b.h), 0.7)
			gReset(g)
			x += float32(b.w)
		}
	})
	// the turntable: its cached picture copied over the window, the label turned
	big := newCanvas(1100, 1100)
	base := newBitmap(1100, 1100)
	fillCircle(base.g, 0xFF202020, 550, 550, 500)
	label := newBitmap(420, 420)
	fillCircle(label.g, 0xFFAA3355, 210, 210, 210)
	time1("turntable: copy 1100x1100 cached picture", 30, func() {
		setCopyMode(big.g, true)
		gImage(big.g, base.bmp, 0, 0, 1100, 1100)
		setCopyMode(big.g, false)
	})
	time1("turntable: same, clipped to 800x800", 30, func() {
		clipRect(big.g, 150, 150, 800, 800)
		setCopyMode(big.g, true)
		gImage(big.g, base.bmp, 0, 0, 1100, 1100)
		setCopyMode(big.g, false)
		clipReset(big.g)
	})
	time1("turntable: 420 px label turned", 30, func() {
		gTranslate(big.g, 550, 550)
		gRotate(big.g, 33)
		gImage(big.g, label.bmp, -210, -210, 420, 420)
		gReset(big.g)
	})
	time1("turntable: 3 glow rings", 30, func() {
		for _, w := range []float32{0.16, 0.1, 0.05} {
			strokeCircle(big.g, 0x40FF00FF, 550, 550, 500*(1+w/2), 500*w)
		}
	})
	time1("radial gradient 1100x1100", 30, func() { radialFill(big.g, 550, 550, 600, 600, 0x80FF0000, 0) })
	for _, p := range ps {
		freePath(p)
	}
}
