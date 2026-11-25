package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

// what Shadow and Outline add round white letters on a see-through picture (as the lyrics widget is)
func TestEffectsVisible(t *testing.T) {
	if !gdipInit() {
		t.Skip("no GDI+")
	}
	f := newFont("Segoe UI", 44, true)
	fxs := []int{fxNone, fxShadow, fxOutline, fxSoftGlow}
	out := image.NewRGBA(image.Rect(0, 0, 640, 120*len(fxs)))
	for i, fx := range fxs {
		b := newBitmap(320, 120)
		gClear(b.g, 0)
		tp := cachedTextPath(f, "Hello")
		gTranslate(b.g, 40, 25)
		drawEffect(b.g, tp.path, 0xFFFFFFFF, f.size, fx)
		gReset(b.g)
		dark := 0
		for y := 0; y < 120; y++ {
			for x := 0; x < 320; x++ {
				c := pixel(b.bmp, x, y)
				a := float64(c>>24) / 255
				if a > 0.15 && c&255 < 90 {
					dark++
				}
				for k, bg := range []float64{24, 150} { // on a dark and a mid-grey screen
					mix := func(v uint32) uint8 { return uint8(float64(v&255)*a + bg*(1-a)) }
					out.Set(x+k*320, y+i*120, color.RGBA{mix(c >> 16), mix(c >> 8), mix(c), 255})
				}
			}
		}
		b.Free()
		t.Logf("%-9s dark pixels %5d", effectNames[fx], dark)
	}
	if p := os.Getenv("FX_PNG"); p != "" {
		fh, _ := os.Create(p)
		png.Encode(fh, out)
		fh.Close()
	}
}
