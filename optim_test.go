package main

import "testing"

func TestContentBox(t *testing.T) {
	w, h := 50, 40
	px := make([]uint32, w*h)
	if b := contentBox(px, w, h); b != [4]int{} {
		t.Fatal(b)
	}
	px[10*w+7] = 0xFF000000
	px[30*w+42] = 0x80101010
	if b := contentBox(px, w, h); b != [4]int{7, 10, 43, 31} {
		t.Fatal(b)
	}
}

func TestOverBlitScaledIdentity(t *testing.T) {
	sw, sh := 20, 16
	src := make([]uint32, sw*sh)
	for i := range src {
		if i%3 == 0 {
			src[i] = 0xFF336699
		}
	}
	a := make([]uint32, 64*64)
	b := make([]uint32, 64*64)
	overBlit(a, 64, 64, src, sw, sh, 10, 12, [4]int{0, 0, 64, 64}, 255)
	overBlitScaled(b, 64, 64, src, sw, sh, 10, 12, 1, 1, [4]int{0, 0, 64, 64}, 255)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("pixel %d: %08x vs %08x", i, a[i], b[i])
		}
	}
	c := make([]uint32, 64*64)
	overBlitScaled(c, 64, 64, src, sw, sh, 10, 12, 1.1, 1.1, [4]int{0, 0, 64, 64}, 255)
	if c[11*64+9] == 0 && c[11*64+10] == 0 { // grown past its edge
		t.Error("no growth")
	}
}

func TestGlowAndLine(t *testing.T) {
	d := make([]uint32, 100*100)
	glowOver(d, 100, 100, 50, 50, 40, 0xFFFF0000, [4]int{0, 0, 100, 100}, 50, 50, 10)
	if d[50*100+50] != 0 || d[50*100+75]>>24 == 0 || d[50*100+95] != 0 {
		t.Errorf("glow: %08x %08x %08x", d[50*100+50], d[50*100+75], d[50*100+95])
	}
	e := make([]uint32, 100*100)
	lineOver(e, 100, 100, 10, 50, 90, 50, 4, 0xFFFFFFFF, [4]int{0, 0, 100, 100})
	if e[50*100+50] != 0xFFFFFFFF || e[40*100+50] != 0 || e[50*100+95] != 0 {
		t.Errorf("line: %08x %08x %08x", e[50*100+50], e[40*100+50], e[50*100+95])
	}
}

func TestBloomBox(t *testing.T) {
	w, h := 120, 90
	px := make([]uint32, w*h)
	for y := 30; y < 50; y++ {
		for x := 41; x < 77; x++ {
			px[y*w+x] = 0xFFFFFFFF
		}
	}
	a, _, _ := bloomFrom(px, w, h, 4, 3)
	b, _, _ := bloomFromBox(px, w, h, 4, 3, contentBox(px, w, h))
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("%d: %d vs %d", i, a[i], b[i])
		}
	}
}
