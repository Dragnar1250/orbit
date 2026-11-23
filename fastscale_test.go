package main

import (
	"math/rand"
	"testing"
)

func TestScaleBilinear(t *testing.T) {
	// a flat colour stays exactly that colour
	src := make([]uint32, 4*3)
	for i := range src {
		src[i] = 0xFF336699
	}
	dst := make([]uint32, 37*23)
	scaleBilinear(dst, 37, 23, src, 4, 3)
	for i, v := range dst {
		if v != 0xFF336699 {
			t.Fatalf("pixel %d = %08X", i, v)
		}
	}
	// a ramp stays a ramp: monotonic, ends at the source ends
	src = []uint32{0xFF000000, 0xFFFFFFFF}
	dst = make([]uint32, 100)
	scaleBilinear(dst, 100, 1, src, 2, 1)
	if dst[0]&0xFF > 8 || dst[99]&0xFF < 247 {
		t.Fatalf("ends %08X %08X", dst[0], dst[99])
	}
	for i := 1; i < 100; i++ {
		if dst[i]&0xFF < dst[i-1]&0xFF || dst[i]>>24 != 0xFF {
			t.Fatalf("not monotonic at %d: %08X %08X", i, dst[i-1], dst[i])
		}
	}
	// premultiplied pixels stay valid (no channel above alpha), any sizes, no panics
	r := rand.New(rand.NewSource(1))
	for k := 0; k < 300; k++ {
		sw, sh, dw, dh := 1+r.Intn(40), 1+r.Intn(40), r.Intn(300), r.Intn(200)
		src = make([]uint32, sw*sh)
		for i := range src {
			a := uint32(r.Intn(256))
			src[i] = a<<24 | uint32(r.Intn(int(a)+1))<<16 | uint32(r.Intn(int(a)+1))<<8 | uint32(r.Intn(int(a)+1))
		}
		dst = make([]uint32, dw*dh)
		scaleBilinear(dst, dw, dh, src, sw, sh)
		for _, v := range dst {
			a := v >> 24
			if v>>16&0xFF > a+1 || v>>8&0xFF > a+1 || v&0xFF > a+1 {
				t.Fatalf("broken premultiplied pixel %08X", v)
			}
		}
	}
	scaleBilinear(nil, 10, 10, src, 3, 3) // too small a buffer: nothing happens
}

func BenchmarkUpscale1080(b *testing.B) {
	src := make([]uint32, 480*270)
	for i := range src {
		src[i] = uint32(i) | 0xFF000000
	}
	dst := make([]uint32, 1920*1080)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scaleBilinear(dst, 1920, 1080, src, 480, 270)
	}
}

func BenchmarkUpscale4K(b *testing.B) {
	src := make([]uint32, 1920*1080)
	dst := make([]uint32, 3840*2160)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scaleBilinear(dst, 3840, 2160, src, 1920, 1080)
	}
}

func BenchmarkCopyFrame1080(b *testing.B) {
	src := make([]uint32, 1920*1080)
	dst := make([]uint32, 1920*1080)
	for i := 0; i < b.N; i++ {
		copy(dst, src)
	}
}

func TestScaleSmooth(t *testing.T) {
	// a dark gradient: no long runs of one value (banding) after stretching, and the mean stays put
	sw, sh := 64, 4
	src := make([]uint32, sw*sh)
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			v := uint32(10 + x*12/sw) // only 12 levels across: the worst case for bands
			src[y*sw+x] = 0xFF000000 | v<<16 | v<<8 | v
		}
	}
	dw, dh := 1024, 64
	dst := make([]uint32, dw*dh)
	var tmp []uint32
	scaleSmooth(dst, dw, dh, src, sw, sh, &tmp)
	row := dst[32*dw : 33*dw]
	longest, run := 0, 1
	for i := 1; i < dw; i++ {
		if row[i] == row[i-1] {
			run++
		} else {
			run = 1
		}
		if run > longest {
			longest = run
		}
	}
	plain := make([]uint32, dw*dh)
	scaleBilinear(plain, dw, dh, src, sw, sh)
	plongest, prun := 0, 1
	prow := plain[32*dw : 33*dw]
	for i := 1; i < dw; i++ {
		if prow[i] == prow[i-1] {
			prun++
		} else {
			prun = 1
		}
		if prun > plongest {
			plongest = prun
		}
	}
	// level changes: the plain stretch steps once per level (hard edges = visible bands); the
	// dithered one blends each step over many pixels
	changes := func(r []uint32) int {
		n := 0
		for i := 1; i < len(r); i++ {
			if r[i] != r[i-1] {
				n++
			}
		}
		return n
	}
	if changes(row) < changes(prow)*4 {
		t.Fatalf("still banded: %d changes vs %d plain (runs %d / %d)", changes(row), changes(prow), longest, plongest)
	}
	var sa, sb float64
	for i := range row {
		sa += float64(row[i] & 0xFF)
		sb += float64(prow[i] & 0xFF)
	}
	if d := (sa - sb) / float64(dw); d > 0.6 || d < -0.6 {
		t.Fatalf("brightness moved by %.2f levels", d)
	}
	for _, v := range dst {
		if v>>24 != 0xFF {
			t.Fatal("alpha changed")
		}
	}
	// premultiplied, transparent edges, odd sizes: valid pixels, no panics
	r := rand.New(rand.NewSource(2))
	for k := 0; k < 200; k++ {
		sw, sh, dw, dh := 1+r.Intn(30), 1+r.Intn(30), r.Intn(400), r.Intn(300)
		src = make([]uint32, sw*sh)
		for i := range src {
			a := uint32(r.Intn(256))
			src[i] = a<<24 | uint32(r.Intn(int(a)+1))<<16 | uint32(r.Intn(int(a)+1))<<8 | uint32(r.Intn(int(a)+1))
		}
		dst = make([]uint32, dw*dh)
		scaleSmooth(dst, dw, dh, src, sw, sh, &tmp)
		for _, v := range dst {
			a := v >> 24
			if v>>16&0xFF > a || v>>8&0xFF > a || v&0xFF > a {
				t.Fatalf("broken premultiplied pixel %08X", v)
			}
		}
	}
}

func BenchmarkSmooth1080(b *testing.B) {
	src := make([]uint32, 480*270)
	for i := range src {
		src[i] = uint32(i*7) | 0xFF000000
	}
	dst := make([]uint32, 1920*1080)
	var tmp []uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scaleSmooth(dst, 1920, 1080, src, 480, 270, &tmp)
	}
}
