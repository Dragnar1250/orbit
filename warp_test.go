package main

import (
	"math"
	"math/rand"
	"testing"
)

func TestHomography(t *testing.T) {
	q := [4][2]float64{{10, 20}, {110, 30}, {100, 140}, {5, 120}}
	m := squareToQuad(q)
	for i, uv := range [][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
		x, y := m.apply(uv[0], uv[1])
		if math.Abs(x-q[i][0]) > 1e-6 || math.Abs(y-q[i][1]) > 1e-6 {
			t.Fatalf("corner %d → %.3f,%.3f", i, x, y)
		}
	}
	inv, ok := m.inverse()
	if !ok {
		t.Fatal("no inverse")
	}
	for k := 0; k < 100; k++ {
		u, v := rand.Float64(), rand.Float64()
		x, y := m.apply(u, v)
		u2, v2 := inv.apply(x, y)
		if math.Abs(u-u2) > 1e-6 || math.Abs(v-v2) > 1e-6 {
			t.Fatal("inverse wrong")
		}
	}
}

func TestWarpOnto(t *testing.T) {
	// an opaque red square warped onto black: the quad is filled, the outside untouched,
	// the edges blend smoothly (no hard stair steps), and pixels stay valid
	sw, sh := 40, 40
	src := make([]uint32, sw*sh)
	for i := range src {
		src[i] = 0xFFFF0000
	}
	dw, dh := 200, 160
	dst := make([]uint32, dw*dh)
	for i := range dst {
		dst[i] = 0xFF000000
	}
	q := tiltQuad(50, 30, 100, 100, 0.5, 600)
	warpOnto(dst, dw, dh, src, sw, sh, q, 1)
	inside := dst[80*dw+100]
	if inside != 0xFFFF0000 {
		t.Fatalf("centre %08X", inside)
	}
	if dst[5*dw+5] != 0xFF000000 || dst[150*dw+190] != 0xFF000000 {
		t.Fatal("outside touched")
	}
	// the far (right) edge is shorter than the near one: real perspective
	if h := q[2][1] - q[1][1]; h >= q[3][1]-q[0][1] {
		t.Fatalf("no perspective: right %.1f left %.1f", h, q[3][1]-q[0][1])
	}
	// edge pixels: some are partly red (anti-aliased)
	partial := 0
	for x := 0; x < dw; x++ {
		r := dst[80*dw+x] >> 16 & 0xFF
		if r > 0 && r < 255 {
			partial++
		}
	}
	if partial == 0 {
		t.Fatal("hard edges")
	}
	for _, p := range dst {
		if p>>24 != 0xFF {
			t.Fatal("alpha broken over an opaque background")
		}
	}
	// half opacity over transparent: premultiplied stays valid
	dst2 := make([]uint32, dw*dh)
	warpOnto(dst2, dw, dh, src, sw, sh, q, 0.5)
	c := dst2[80*dw+100]
	if a := c >> 24; a < 120 || a > 135 || c>>16&0xFF > a {
		t.Fatalf("half opacity %08X", c)
	}
	// degenerate and odd inputs never panic
	r := rand.New(rand.NewSource(4))
	for k := 0; k < 300; k++ {
		var qq [4][2]float64
		for i := range qq {
			qq[i] = [2]float64{r.Float64()*400 - 100, r.Float64()*300 - 100}
		}
		warpOnto(dst, dw, dh, src, sw, sh, qq, r.Float64()*1.5)
	}
	warpOnto(dst, dw, dh, src, sw, sh, [4][2]float64{}, 1)
	warpOnto(nil, dw, dh, src, sw, sh, q, 1)
}

func BenchmarkWarpCard(b *testing.B) {
	src := make([]uint32, 360*440)
	for i := range src {
		src[i] = 0xFF336699
	}
	dst := make([]uint32, 1920*1080)
	q := tiltQuad(400, 300, 300, 370, 0.45, 1200)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		warpOnto(dst, 1920, 1080, src, 360, 440, q, 1)
	}
}
