package main

import (
	"math"
	"testing"
)

var cinScreens = [][2]float64{{1920, 1080}, {2560, 1440}, {3840, 2160}, {1366, 768}, {1280, 720}, {2560, 1080}, {3440, 1440}, {1920, 1200}, {1080, 1920}, {1600, 900}}

func TestCinemaLayoutFits(t *testing.T) {
	for _, s := range cinScreens {
		for k, lyr := range []bool{true, false, true, false} {
			big := k >= 2
			W, H := s[0], s[1]
			l := cinemaLayout(W, H, lyr, big)
			on := func(name string, r rectF) {
				if r.X < 0 || r.Y < 0 || r.right() > W+0.5 || r.bottom() > H+0.5 || r.W <= 0 || r.H <= 0 {
					t.Errorf("%vx%v lyrics=%v: %s off screen %+v", W, H, lyr, name, r)
				}
			}
			on("art", l.Art)
			on("prog", l.Prog)
			on("controls", l.Ctrl)
			on("next", l.Next)
			on("clock", l.Clock)
			on("toast", l.Toast)
			if l.Toast.overlaps(l.Art) {
				t.Errorf("%vx%v lyrics=%v: messages cover the cover", W, H, lyr)
			}
			for i, b := range l.Buttons {
				on("button", b)
				if !l.Ctrl.grow(0.5).has(b.X, b.Y) || !l.Ctrl.grow(0.5).has(b.right()-1, b.bottom()-1) {
					t.Errorf("%vx%v: button %d outside the control row", W, H, i)
				}
				if i > 0 && b.overlaps(l.Buttons[i-1]) {
					t.Errorf("%vx%v: buttons %d and %d overlap", W, H, i-1, i)
				}
			}
			// the regions redrawn every frame never overlap each other or the still text
			prog := rectF{l.Prog.X - 14*l.U, l.Prog.Y - 16*l.U, l.Prog.W + 28*l.U, (l.Times + 30*l.U) - (l.Prog.Y - 16*l.U)}
			text := rectF{l.Text.X, l.Title, l.Text.W, l.Album + 30*l.U - l.Title}
			pairs := map[string][2]rectF{"prog/ctrl": {prog, l.Ctrl}, "prog/text": {prog, text}, "ctrl/next": {l.Ctrl, l.Next}, "art/text": {l.Art, text}}
			if lyr {
				on("lyrics", l.Lyr)
				for k, r := range map[string]rectF{"art": l.Art, "prog": prog, "ctrl": l.Ctrl, "next": l.Next, "clock": l.Clock, "toast": l.Toast, "text": text} {
					pairs["lyrics/"+k] = [2]rectF{l.Lyr, r}
				}
			}
			for name, p := range pairs {
				if p[0].overlaps(p[1]) {
					t.Errorf("%vx%v lyrics=%v: %s overlap %+v %+v", W, H, lyr, name, p[0], p[1])
				}
			}
			if big && l.Art.W < cinemaLayout(W, H, lyr, false).Art.W {
				t.Errorf("%vx%v: the big cover is smaller", W, H)
			}
			if !lyr && math.Abs(l.Art.X+l.Art.W/2-W/2) > 1 {
				t.Errorf("%vx%v: without lyrics the cover is centred", W, H)
			}
			if l.Next.bottom() > H-20*l.U {
				t.Errorf("%vx%v lyrics=%v: up next too close to the bottom (%v)", W, H, lyr, l.Next.bottom())
			}
		}
	}
}

func TestWrapBlock(t *testing.T) {
	b := wrapBlock([]float64{100, 80, 120, 60}, 10, 200, 50)
	if b.Rows != 2 || b.Row[0] != 0 || b.Row[1] != 0 || b.Row[2] != 1 || b.Row[3] != 1 {
		t.Fatalf("rows %+v", b)
	}
	if b.X[1] != 110 || b.X[2] != 0 || b.X[3] != 130 || b.H != 100 {
		t.Fatalf("x %+v", b)
	}
	if w := wrapBlock([]float64{500}, 10, 200, 50); w.Rows != 1 {
		t.Fatal("a word wider than the column keeps its own row")
	}
	if e := wrapBlock(nil, 10, 200, 50); e.Rows != 1 || e.H != 50 {
		t.Fatal("empty line")
	}
}

func TestLyrOffsetsAndAlpha(t *testing.T) {
	hs := []float64{50, 100, 50, 50}
	ys := lyrOffsets(hs, 1, 20, 500)
	if ys[1] != 450 || ys[0] != 380 || ys[2] != 570 || ys[3] != 640 {
		t.Fatalf("offsets %v", ys)
	}
	if lyrOffsets(hs, -1, 20, 500)[0] != 475 {
		t.Fatal("before the first line it waits in the middle")
	}
	if a := lyrAlpha(0, 450, 100, 100, 900); a != 1 {
		t.Fatalf("current line full: %v", a)
	}
	if lyrAlpha(-1, 450, 100, 100, 900) >= lyrAlpha(1, 450, 100, 100, 900) {
		t.Fatal("sung lines dimmer than the next ones")
	}
	if lyrAlpha(2, 60, 40, 100, 900) != 0 || lyrAlpha(2, 120, 20, 100, 900) >= lyrAlpha(2, 450, 20, 100, 900) {
		t.Fatal("lines fade toward the edge")
	}
}

func TestSpringAndGapDots(t *testing.T) {
	v := 0.0
	for i := 0; i < 15; i++ { // a quarter of a second at 60 fps
		v = springTo(v, 100, 1.0/60)
	}
	if v < 88 || v > 100 {
		t.Fatalf("spring after 0.25 s: %v", v)
	}
	if springTo(99.9, 100, 0.016) != 100 {
		t.Fatal("snaps when close")
	}
	if _, ok := gapDots(5, 3, 6); ok {
		t.Fatal("short pause: no dots")
	}
	if p, ok := gapDots(7, 4, 10); !ok || math.Abs(p-0.5) > 1e-9 {
		t.Fatalf("dots %v %v", p, ok)
	}
	if n := stageNames(); len(n) != len(styleNames)+1 || n[0] != "Lines" || n[1+stScatter] != "Scatter" {
		t.Fatal("stage names")
	}
	if d := defaultConfig(); d.FsStage != 1+stScatter || d.FsFPS != 60 {
		t.Fatal("Cinema opens on Scatter at 60 fps")
	}
}

func TestMixFrames(t *testing.T) {
	n := 1 << 17 // big enough to run on several cores
	a, b, d := make([]uint32, n), make([]uint32, n), make([]uint32, n)
	for i := range a {
		a[i], b[i] = 0xFF000000, 0xFFFFFFFF
	}
	a[7], b[7] = 0x80402010, 0xFF804020
	mixFrames(d, a, b, 0.5)
	if d[0] != 0xFF7F7F7F && d[0] != 0xFF808080 {
		t.Fatalf("half way %08x", d[0])
	}
	if d[7]>>24 < 0xBE || d[7]>>24 > 0xC0 {
		t.Fatalf("alpha mixed too %08x", d[7])
	}
	mixFrames(d, a, b, 0)
	if d[1] != a[1] {
		t.Fatal("t=0 is a")
	}
	mixFrames(d, a, b, 1)
	if d[n-1] != b[n-1] {
		t.Fatal("t=1 is b")
	}
	px := []uint32{0xFF808080, 0xFF000000, 0xFFFFFFFF}
	grain(px, 2)
	for i, c := range px {
		for _, ch := range []uint32{c >> 16 & 255, c >> 8 & 255, c & 255} {
			want := []uint32{0x80, 0, 255}[i]
			if int(ch)-int(want) > 2 || int(want)-int(ch) > 2 {
				t.Fatalf("grain too strong %08x", c)
			}
		}
		if c>>24 != 0xFF {
			t.Fatal("grain keeps alpha")
		}
	}
}

func BenchmarkMixFrames1440p(b *testing.B) {
	n := 2560 * 1440
	x, y, d := make([]uint32, n), make([]uint32, n), make([]uint32, n)
	for i := 0; i < b.N; i++ {
		mixFrames(d, x, y, 0.37)
	}
}

func TestBlendWhite(t *testing.T) {
	dw, dh := 8, 4
	dst := make([]uint32, dw*dh)
	for i := range dst {
		dst[i] = 0xFF000000
	}
	sw, sh := 3, 2
	src := []uint32{0xFFFFFFFF, 0x80808080, 0, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF}
	blendWhite(dst, dw, dh, src, sw, sh, 2, 1, 0, 0, sw, sh, [4]int{0, 0, dw, dh}, 256)
	if dst[1*dw+2] != 0xFFFFFFFF || dst[1*dw+4] != 0xFF000000 || dst[0] != 0xFF000000 {
		t.Fatalf("full ink: %08x %08x", dst[1*dw+2], dst[1*dw+4])
	}
	if g := dst[1*dw+3] & 255; g < 0x7E || g > 0x82 {
		t.Fatalf("half ink: %08x", dst[1*dw+3])
	}
	// strength and clipping
	for i := range dst {
		dst[i] = 0xFF000000
	}
	blendWhite(dst, dw, dh, src, sw, sh, 2, 1, 0, 0, sw, sh, [4]int{0, 0, 3, dh}, 128)
	if g := dst[1*dw+2] & 255; g < 0x7E || g > 0x81 || dst[1*dw+3] != 0xFF000000 {
		t.Fatalf("strength / clip: %08x %08x", dst[1*dw+2], dst[1*dw+3])
	}
	// a part of the source only, and off the edge without a panic
	blendWhite(dst, dw, dh, src, sw, sh, 5, 2, 1, 1, 3, 2, [4]int{0, 0, dw, dh}, 256)
	blendWhite(dst, dw, dh, src, sw, sh, -5, -5, 0, 0, sw, sh, [4]int{0, 0, dw, dh}, 256)
	blendWhite(dst, dw, dh, src, sw, sh, 100, 100, 0, 0, sw, sh, [4]int{0, 0, dw, dh}, 256)
	if dst[3*dw+7] != 0xFFFFFFFF {
		t.Fatalf("part of the source: %08x", dst[3*dw+7])
	}
}

func BenchmarkBlendWhiteLyrics1440p(b *testing.B) {
	dw, dh := 2560, 1440
	dst := make([]uint32, dw*dh)
	sw, sh := 1150, 160
	src := make([]uint32, sw*sh)
	for i := range src {
		if i%3 == 0 {
			src[i] = 0xFFFFFFFF
		}
	}
	for i := 0; i < b.N; i++ {
		for k := 0; k < 8; k++ { // eight lines on screen
			blendWhite(dst, dw, dh, src, sw, sh, 1200, 150+k*150, 0, 0, sw, sh, [4]int{1200, 140, 2470, 1380}, 100)
		}
	}
}

func TestBloom(t *testing.T) {
	w, h := 64, 32
	src := make([]uint32, w*h)
	for y := 12; y < 20; y++ { // a bar of ink in the middle
		for x := 20; x < 44; x++ {
			src[y*w+x] = 0xFFFFFFFF
		}
	}
	g, gw, gh := bloomFrom(src, w, h, 4, 2)
	if gw != 16 || gh != 8 || len(g) != gw*gh {
		t.Fatalf("size %dx%d", gw, gh)
	}
	if g[4*gw+8] == 0 || g[0] != 0 && g[0] >= g[4*gw+8] {
		t.Fatal("glow strongest at the ink")
	}
	dst := make([]uint32, w*h)
	for i := range dst {
		dst[i] = 0xFF101010
	}
	addBloom(dst, w, h, g, gw, gh, 4, 0, 0, [4]int{0, 0, w, h}, 0xFFFFFFFF, 1)
	mid, edge := dst[16*w+32]&255, dst[16*w+2]&255
	if mid <= 0x10 || mid <= edge {
		t.Fatalf("light at the ink %x, less away from it %x", mid, edge)
	}
	for _, p := range dst {
		if p>>24 != 0xFF {
			t.Fatal("stays opaque")
		}
	}
	// clipped and off-screen: no panic
	addBloom(dst, w, h, g, gw, gh, 4, -30, -30, [4]int{0, 0, 10, 10}, 0xFFFFFFFF, 2)
	addBloom(dst, w, h, g, gw, gh, 4, 500, 500, [4]int{0, 0, w, h}, 0xFFFFFFFF, 2)
	if a, _, _ := bloomFrom(nil, 0, 0, 4, 2); a != nil {
		t.Fatal("empty")
	}
}

func BenchmarkBloomStage1440p(b *testing.B) {
	w, h := 1250, 1100
	src := make([]uint32, w*h)
	for i := range src {
		if (i/w)%90 < 50 && (i%w)%300 < 200 {
			src[i] = 0xFFFFFFFF
		}
	}
	dst := make([]uint32, 2560*1440)
	for i := 0; i < b.N; i++ {
		g, gw, gh := bloomFrom(src, w, h, 4, 3)
		addBloom(dst, 2560, 1440, g, gw, gh, 4, 1250, 150, [4]int{1200, 140, 2470, 1380}, 0xFFFFE0C0, 0.6)
	}
}

func BenchmarkBloomMake(b *testing.B) {
	w, h := 1250, 1100
	src := make([]uint32, w*h)
	for i := 0; i < b.N; i++ {
		bloomFrom(src, w, h, 4, 3)
	}
}

func BenchmarkBloomAdd(b *testing.B) {
	w, h := 1250, 1100
	src := make([]uint32, w*h)
	for i := range src {
		if (i/w)%90 < 50 && (i%w)%300 < 200 {
			src[i] = 0xFFFFFFFF
		}
	}
	g, gw, gh := bloomFrom(src, w, h, 4, 3)
	dst := make([]uint32, 2560*1440)
	for i := 0; i < b.N; i++ {
		addBloom(dst, 2560, 1440, g, gw, gh, 4, 1250, 150, [4]int{1200, 140, 2470, 1380}, 0xFFFFE0C0, 0.6)
	}
}

func TestCinemaLooksClamp(t *testing.T) {
	c := defaultConfig()
	if c.FsBg != 0 || c.FsCover != 0 || c.FsDisc != 0 {
		t.Fatalf("defaults: bg %d cover %d disc %d", c.FsBg, c.FsCover, c.FsDisc)
	}
	c.FsBg, c.FsCover, c.FsDisc = 99, -3, 99
	c.validateFields()
	if c.FsBg != len(fsBgNames)-1 || c.FsCover != 0 || c.FsDisc != len(discNames) {
		t.Fatalf("clamp: bg %d cover %d disc %d", c.FsBg, c.FsCover, c.FsDisc)
	}
	if len(fsBgNames) != 5 || len(fsCoverNames) != 3 {
		t.Fatal("names")
	}
}
