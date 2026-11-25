package main

import (
	"math"
	"math/rand"
	"testing"
)

func discImg(n int) []uint32 {
	src := make([]uint32, n*n)
	r := float64(n) / 2
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			dx, dy := float64(x)+0.5-r, float64(y)+0.5-r
			if dx*dx+dy*dy < (r-1)*(r-1) {
				src[y*n+x] = 0xFF000000 | uint32(x*255/n)<<16 | uint32(y*255/n)<<8 | 0x40
			}
		}
	}
	return src
}

func TestRotateOver(t *testing.T) {
	n := 200
	src := discImg(n)
	// a red dot to the right of the centre
	for y := 95; y < 105; y++ {
		for x := 170; x < 180; x++ {
			src[y*n+x] = 0xFFFF0000
		}
	}
	dst := make([]uint32, 400*400)
	all := [4]int{0, 0, 400, 400}
	rotateOver(dst, 400, 400, src, n, n, 200, 200, 200, 0, 1, all)
	if dst[200*400+275] != 0xFFFF0000 {
		t.Fatalf("0°: %08x", dst[200*400+275])
	}
	for i := range dst {
		dst[i] = 0
	}
	rotateOver(dst, 400, 400, src, n, n, 200, 200, 200, 90, 1, all) // clockwise: right → below
	if dst[275*400+200] != 0xFFFF0000 {
		t.Fatalf("90°: %08x", dst[275*400+200])
	}
	// twice as large, half see-through, over white: still premultiplied and in range
	for i := range dst {
		dst[i] = 0xFFFFFFFF
	}
	rotateOver(dst, 400, 400, src, n, n, 200, 200, 380, 33, 0.5, all)
	for i, p := range dst {
		a, r, g, b := p>>24, p>>16&255, p>>8&255, p&255
		if r > a || g > a || b > a {
			t.Fatalf("not premultiplied at %d: %08x", i, p)
		}
	}
	if dst[0] != 0xFFFFFFFF || dst[200*400+200]>>24 != 255 {
		t.Fatalf("corners / centre: %08x %08x", dst[0], dst[200*400+200])
	}
	// clip and off-screen centres never write outside
	for k := 0; k < 300; k++ {
		cx, cy := rand.Float64()*600-100, rand.Float64()*600-100
		rotateOver(dst, 400, 400, src, n, n, cx, cy, 10+rand.Float64()*700, rand.Float64()*720-360, rand.Float64(), [4]int{rand.Intn(400), rand.Intn(400), rand.Intn(500), rand.Intn(500)})
	}
}

func BenchmarkRotateOver600(b *testing.B) {
	src := discImg(1200)
	dst := make([]uint32, 1920*1080)
	for i := 0; i < b.N; i++ {
		rotateOver(dst, 1920, 1080, src, 1200, 1200, 400, 400, 620, float64(i), 1, [4]int{0, 0, 1920, 1080})
	}
}

func TestOverBlit(t *testing.T) {
	src := []uint32{0, 0xFF102030, 0x80402010, 0x40404040}
	dst := []uint32{0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF}
	overBlit(dst, 3, 2, src, 2, 2, 1, 0, [4]int{0, 0, 3, 2}, 255)
	want := []uint32{0xFFFFFFFF, 0xFFFFFFFF, 0xFF102030, 0xFFFFFFFF, 0xFFBF9F8F, 0xFFFFFFFF}
	for i := range want {
		if dst[i] != want[i] {
			t.Fatalf("%d: %08x want %08x", i, dst[i], want[i])
		}
	}
	overBlit(dst, 3, 2, src, 2, 2, -5, -5, [4]int{0, 0, 3, 2}, 128) // off the edge: nothing
	big := make([]uint32, 500*500)
	for k := 0; k < 200; k++ {
		overBlit(big, 500, 500, discImg(64), 64, 64, rand.Intn(700)-100, rand.Intn(700)-100, [4]int{rand.Intn(500), rand.Intn(500), rand.Intn(600), rand.Intn(600)}, rand.Intn(300))
	}
	for _, p := range big {
		if a := p >> 24; p>>16&255 > a || p>>8&255 > a || p&255 > a {
			t.Fatalf("not premultiplied: %08x", p)
		}
	}
}

func TestFitScale(t *testing.T) {
	// one short word grows to the cap; a long line stops when it would need too many rows
	if s := fitScale([]float64{100}, 20, 50, 900, 400, 1, 2.4); s != 2.4 {
		t.Fatalf("short: %v", s)
	}
	w := []float64{120, 80, 150, 90, 110, 130, 70, 160, 100, 90, 140, 120}
	s := fitScale(w, 20, 50, 900, 400, 1, 2.4)
	if s <= 1 || s >= 2.4 {
		t.Fatalf("long: %v", s)
	}
	// the answer really fits, and a bit more does not
	rowsAt := func(s float64) (int, float64) {
		rows, rw, widest := 1, 0.0, 0.0
		for _, x := range w {
			x *= s
			if rw > 0 && rw+20*s+x > 900 {
				rows, rw = rows+1, x
			} else if rw > 0 {
				rw += 20*s + x
			} else {
				rw = x
			}
			widest = max(widest, rw)
		}
		return rows, widest
	}
	if r, wd := rowsAt(s); float64(r)*50*s > 400 || wd > 900 {
		t.Fatalf("does not fit: %d rows, %v wide", r, wd)
	}
	if r, _ := rowsAt(s * 1.02); float64(r)*50*s*1.02 <= 400 && s*1.02 < 2.4 {
		t.Fatalf("could be bigger: %v", s)
	}
	if s := fitScale([]float64{2000}, 20, 50, 900, 400, 1, 2.4); s != 1 {
		t.Fatalf("too wide stays: %v", s)
	}
}

func BenchmarkFlowStep1440p(b *testing.B) { // one Flowing-colours step: scale up, grain, lay the still layer over
	W, H := 2560, 1440
	bg, comp, over, mid := make([]uint32, W*H), make([]uint32, W*H), make([]uint32, W*H), make([]uint32, 160*90)
	for i := range mid {
		mid[i] = 0xFF203040 + uint32(i)
	}
	for y := 300; y < 1100; y++ { // a cover and some text: ~1/5 of the screen covered
		for x := 200; x < 900; x++ {
			over[y*W+x] = 0xFF808080
		}
	}
	var tmp []uint32
	for i := 0; i < b.N; i++ {
		scaleSmooth(bg, W, H, mid, 160, 90, &tmp)
		grain(bg, 2)
		copy(comp, bg)
		overBlit(comp, W, H, over, W, H, 0, 0, [4]int{0, 0, W, H}, 255)
	}
}

func TestOverBlitFeather(t *testing.T) {
	src := make([]uint32, 100*100)
	for i := range src {
		src[i] = 0xFFFFFFFF
	}
	dst := make([]uint32, 200*200)
	overBlitFeather(dst, 200, 200, src, 100, 100, 50, 50, [4]int{0, 0, 200, 200}, 20)
	if dst[100*200+100] != 0xFFFFFFFF { // the middle: untouched
		t.Fatalf("middle %08x", dst[100*200+100])
	}
	if a := dst[100*200+50] >> 24; a != 0 { // the very edge: gone
		t.Fatalf("edge %d", a)
	}
	if a := dst[100*200+60] >> 24; a < 100 || a > 150 { // half-way in: half
		t.Fatalf("ramp %d", a)
	}
	for k := 0; k < 100; k++ {
		overBlitFeather(dst, 200, 200, discImg(64), 64, 64, rand.Intn(300)-80, rand.Intn(300)-80, [4]int{rand.Intn(200), rand.Intn(200), rand.Intn(260), rand.Intn(260)}, rand.Intn(40))
	}
	for _, p := range dst {
		if a := p >> 24; p>>16&255 > a || p>>8&255 > a || p&255 > a {
			t.Fatalf("not premultiplied: %08x", p)
		}
	}
}

func TestTonearm(t *testing.T) {
	for _, prog := range []float64{0, 0.5, 1} {
		p := tonearm(500, 500, 300, prog, 1)
		r := math.Hypot(p.Nx-500, p.Ny-500)
		want := 300 * (0.93 - 0.27*prog)
		if math.Abs(r-want) > 0.5 {
			t.Fatalf("progress %v: needle at r=%.1f, want %.1f", prog, r, want)
		}
		if p.Ny < 500-50 { // in the lower half, on the right
			t.Fatalf("needle up top: %+v", p)
		}
	}
	rest := tonearm(500, 500, 300, 0, 0)
	if math.Hypot(rest.Nx-500, rest.Ny-500) < 300*1.15 {
		t.Fatalf("resting needle on the record: %+v", rest)
	}
	// the spring gets there, overshoots only a little, at 30 and 144 fps alike
	for _, fps := range []float64{30, 60, 144} {
		x, v, peak := 0.0, 0.0, 0.0
		for i := 0; i < int(fps*2); i++ {
			x, v = armSpring(x, v, 1, 1/fps)
			peak = math.Max(peak, x)
		}
		if math.Abs(x-1) > 0.01 || peak > 1.12 || peak < 1.0 {
			t.Fatalf("%v fps: end %.3f peak %.3f", fps, x, peak)
		}
	}
}

func TestSharpen(t *testing.T) {
	w, h := 20, 20
	px := make([]uint32, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint32(60)
			if x >= 10 {
				v = 180
			}
			px[y*w+x] = 0xFF000000 | v<<16 | v<<8 | v
		}
	}
	px[0] = 0x80404040 // a see-through corner stays as it is
	sharpen(px, w, h, 0.5)
	if px[10*w+9]&255 >= 60 || px[10*w+10]&255 <= 180 { // the edge gets crisper on both sides
		t.Fatalf("edge %d %d", px[10*w+9]&255, px[10*w+10]&255)
	}
	if px[10*w+3]&255 != 60 || px[0] != 0x80404040 {
		t.Fatalf("flat area or corner changed: %08x %08x", px[10*w+3], px[0])
	}
}

func TestBlurRGB(t *testing.T) {
	w, h := 40, 30
	px := make([]uint32, w*h)
	for i := range px {
		px[i] = 0xFF000000
	}
	px[15*w+20] = 0xFFFFFFFF
	blurRGB(px, w, h, 2, 3)
	if px[15*w+20]&255 == 255 || px[15*w+20]&255 == 0 || px[15*w+22]&255 == 0 {
		t.Fatalf("not spread: %08x %08x", px[15*w+20], px[15*w+22])
	}
	if px[0] != 0xFF000000 || px[15*w+20]>>24 != 255 {
		t.Fatalf("far / alpha changed")
	}
	flat := make([]uint32, w*h)
	for i := range flat {
		flat[i] = 0xFF336699
	}
	blurRGB(flat, w, h, 3, 2)
	for _, p := range flat {
		if p != 0xFF336699 {
			t.Fatalf("a flat colour changed: %08x", p)
		}
	}
}

func TestRingsOver(t *testing.T) {
	W := 200
	dst := make([]uint32, W*W)
	ringsOver(dst, W, W, 100, 100, []ring{{60, 4, 1}, {80, 2, 0.5}}, 0xFFFFFF, [4]int{0, 0, W, W})
	if a := dst[100*W+160] >> 24; a < 240 { // on the first ring, right of the centre
		t.Fatalf("ring 1: %d", a)
	}
	if a := dst[100*W+180] >> 24; a < 100 || a > 150 { // half-opaque second ring
		t.Fatalf("ring 2: %d", a)
	}
	if dst[100*W+100] != 0 || dst[100*W+170] != 0 || dst[0] != 0 { // centre, between, corner untouched
		t.Fatalf("outside the rings: %08x %08x %08x", dst[100*W+100], dst[100*W+170], dst[0])
	}
	if dst[40*W+100]>>24 < 240 { // top of ring 1 too
		t.Fatalf("top")
	}
	for _, p := range dst {
		if a := p >> 24; p>>16&255 > a || p&255 > a {
			t.Fatalf("not premultiplied: %08x", p)
		}
	}
}

func BenchmarkRingsOver(b *testing.B) {
	dst := make([]uint32, 900*900)
	rs := []ring{}
	for i := 0; i < 12; i++ {
		rs = append(rs, ring{300 * (0.975 - 0.3*float64(i)/11), 6, 0.4})
	}
	for i := 0; i < b.N; i++ {
		ringsOver(dst, 900, 900, 450, 450, rs, 0xC09040, [4]int{0, 0, 900, 900})
	}
}
