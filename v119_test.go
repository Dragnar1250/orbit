package main

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestScratchDelta(t *testing.T) {
	for _, c := range []struct{ a, b, want float64 }{
		{0, 10, 10}, {10, 0, -10}, {170, -170, 20}, {-170, 170, -20}, {359, 1, 2}, {0, 180, 180}, {45, 45, 0},
	} {
		if got := scratchDelta(c.a, c.b); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("scratchDelta(%v,%v)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestScratchPos(t *testing.T) {
	if got := scratchPos(30, 360, 6, 200); math.Abs(got-36) > 1e-9 {
		t.Fatalf("one turn forward: %v", got)
	}
	if got := scratchPos(30, -180, 6, 200); math.Abs(got-27) > 1e-9 {
		t.Fatalf("half a turn back: %v", got)
	}
	if got := scratchPos(1, -3600, 6, 200); got != 0 {
		t.Fatalf("stops at the start: %v", got)
	}
	if got := scratchPos(195, 3600, 6, 200); got != 199.5 {
		t.Fatalf("stops before the end: %v", got)
	}
	// a whole circle in many small steps ends where one big step does
	p := 50.0
	a := 0.0
	for i := 0; i < 72; i++ {
		b := math.Mod(a+5+180, 360) - 180
		p = scratchPos(p, scratchDelta(a, b), 6, 300)
		a = b
	}
	if math.Abs(p-56) > 1e-6 {
		t.Fatalf("72 × 5° = one turn = 6 s, got %v", p-50)
	}
}

func TestNewDiscStylesClamp(t *testing.T) {
	c := defaultConfig()
	c.DiscStyle = discGlowDark
	c.ScratchSec = 500
	c.SwapStyle = 9
	c.validateFields()
	if c.SwapStyle != len(swapStyleNames)-1 || defaultConfig().SwapStyle != 0 {
		t.Fatalf("SwapStyle clamp / default: %d", c.SwapStyle)
	}
	if c.DiscStyle != discGlowDark || len(discNames) != discGlowDark+1 {
		t.Fatalf("disc style list out of step: %d names, style %d", len(discNames), c.DiscStyle)
	}
	if c.ScratchSec != 30 {
		t.Fatalf("ScratchSec not clamped: %v", c.ScratchSec)
	}
	if !defaultConfig().SwapAnim || !defaultConfig().Scratch || defaultConfig().Worn {
		t.Fatal("defaults: swap + scratch on, wear off")
	}
	if len(visNames) != visLiquid+1 || len(styleNames) != stLED+1 {
		t.Fatal("visualizer / lyric style names out of step with their constants")
	}
	for _, k := range []string{"Worn", "SwapAnim", "Scratch", "ScratchSec"} {
		v := "true"
		if k == "ScratchSec" {
			v = "8"
		}
		if err := c.setField(k, []byte(v)); err != nil {
			t.Fatalf("set %s: %v", k, err)
		}
	}
}

func TestFlapRows(t *testing.T) {
	rows := flapRows(strings.Fields("TURN IT UP AND LET THE NIGHT DECIDE"), 12)
	for _, r := range rows {
		n := -1
		for _, i := range r {
			n += 1 + len(strings.Fields("TURN IT UP AND LET THE NIGHT DECIDE")[i])
		}
		if n > 12 {
			t.Fatalf("row too long: %v (%d)", r, n)
		}
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %v", rows)
	}
	if r := flapRows([]string{"SUPERCALIFRAGILISTIC"}, 8); len(r) != 1 {
		t.Fatalf("a long word gets one row: %v", r)
	}
	if r := flapRows(nil, 10); len(r) != 0 {
		t.Fatal("no words, no rows")
	}
}

func TestFlapTiming(t *testing.T) {
	words := []string{"HELLO", "WORLD"}
	end := flapSettle(words)
	for k := 0; k < 11; k++ {
		if s, _, _ := flapState(k, -0.1); s {
			t.Fatal("settled before the line")
		}
		if s, _, _ := flapState(k, end); !s {
			t.Fatalf("tile %d not settled by flapSettle", k)
		}
		prevTurn := -1
		for e := 0.0; e < end; e += 0.01 {
			s, turn, ph := flapState(k, e)
			if s {
				break
			}
			if ph < 0 || ph >= 1 || turn < prevTurn {
				t.Fatalf("tile %d at %.2f: turn %d phase %.2f", k, e, turn, ph)
			}
			prevTurn = turn
		}
	}
	if flapGlyph(3, 1, ' ') != ' ' {
		t.Fatal("a space stays a space")
	}
	if flapGlyph(3, 1, 'A') != flapGlyph(3, 1, 'A') {
		t.Fatal("in-between letters must be the same every frame")
	}
}

func TestLedLineAndScroll(t *testing.T) {
	cols, start, end := ledLine([][]uint16{{1, 2}, {3}, {4, 5, 6}})
	want := []uint16{1, 2, 0, 0, 0, 3, 0, 0, 0, 4, 5, 6}
	if len(cols) != len(want) {
		t.Fatalf("cols %v", cols)
	}
	for i := range want {
		if cols[i] != want[i] {
			t.Fatalf("cols %v", cols)
		}
	}
	if start[1] != 5 || end[1] != 6 || start[2] != 9 || end[2] != 12 {
		t.Fatalf("start %v end %v", start, end)
	}
	panel := 100
	// the sung point sits at 42 % of the panel and moves forward with the song
	a := ledScroll(start, end, 2, 0, 1, panel)
	b := ledScroll(start, end, 2, 1, 1, panel)
	if math.Abs(a-(42-9)) > 1e-9 || math.Abs(b-(42-12)) > 1e-9 {
		t.Fatalf("scroll %v %v", a, b)
	}
	// before the first word the line slides in from beyond the anchor
	if in0, in1 := ledScroll(start, end, -1, 0, 0, panel), ledScroll(start, end, -1, 0, 1, panel); in0 <= in1 || in1 != 42 {
		t.Fatalf("intro %v → %v", in0, in1)
	}
	if ledScroll(nil, nil, 0, 0, 0, panel) != 0 {
		t.Fatal("empty line")
	}
}

func TestDownsampleGlyph(t *testing.T) {
	// a 4x-scale "L": a full-height bar 2 dots wide plus a foot, with an empty margin
	const rows, k = 9, 4
	w, h := 7*k, rows*k
	al := make([]uint8, w*h)
	set := func(c, r int) {
		for y := r * k; y < (r+1)*k; y++ {
			for x := c * k; x < (c+1)*k; x++ {
				al[y*w+x] = 255
			}
		}
	}
	for r := 0; r < rows; r++ {
		set(1, r)
		set(2, r)
	}
	for c := 3; c < 5; c++ {
		set(c, rows-1)
	}
	cols := downsampleGlyph(al, w, h, rows)
	if len(cols) != 4 {
		t.Fatalf("trimmed width: %d cols %v", len(cols), cols)
	}
	if cols[0] != 0x1FF || cols[1] != 0x1FF || cols[2] != 1<<8 || cols[3] != 1<<8 {
		t.Fatalf("cols %b", cols)
	}
	if c := downsampleGlyph(make([]uint8, w*h), w, h, rows); len(c) != 1 || c[0] != 0 {
		t.Fatalf("blank letter keeps one empty column: %v", c)
	}
	if downsampleGlyph(nil, 0, 0, rows) != nil {
		t.Fatal("bad size")
	}
}

func TestPaletteFromPixels(t *testing.T) {
	px := []uint32{0xFF101010, 0xFFFF2020, 0xFFFF2828, 0xFF20FF20, 0xFF2020FF, 0xFF808080, 0x00FF00FF}
	pal := paletteFromPixels(px, 6)
	if len(pal) != 4 { // red (once), green, blue, grey — the near-black and see-through pixels are skipped
		t.Fatalf("palette %x", pal)
	}
	for _, c := range pal {
		if c>>24 != 0xFF {
			t.Fatalf("opaque colours only: %x", c)
		}
	}
	if p := paletteFromPixels(nil, 3); len(p) != 1 {
		t.Fatal("an empty cover still gives one colour")
	}
}

func TestSpectrumWave(t *testing.T) {
	s := &Spectrum{rate: 48000, peak: 1e-3}
	out := make([]float32, 200)
	if s.Wave(out) {
		t.Fatal("no sound yet")
	}
	buf := make([]float32, 1024)
	for i := range buf {
		buf[i] = float32(0.3 * math.Sin(2*math.Pi*float64(i)/48)) // 1 kHz
	}
	s.Feed(buf, 48000)
	for i := 0; i < 20; i++ { // let the gain settle
		s.Wave(out)
	}
	if !s.Wave(out) {
		t.Fatal("sound coming in")
	}
	// starts on a rising zero crossing and is scaled to about ±0.9
	if out[0] < -0.05 || out[0] > 0.2 || out[1] <= out[0] {
		t.Fatalf("not triggered on a rising edge: %v %v", out[0], out[1])
	}
	mx := float32(0)
	for _, v := range out {
		if v > mx {
			mx = v
		}
	}
	if mx < 0.75 || mx > 1.2 {
		t.Fatalf("gain: peak %v", mx)
	}
	if s.Wave(make([]float32, 900)) {
		t.Fatal("too long a window is refused")
	}
}

func TestPositionEasesSmallCorrections(t *testing.T) {
	m := &Media{}
	m.SetTrack("Song", "Artist", "", "smtc")
	m.SetDuration(keyOf("Song", "Artist"), 200)
	m.SetPosition(30, true)
	time.Sleep(50 * time.Millisecond)
	before := m.Snapshot().Pos
	m.SetPosition(before+0.3, true) // the player is 0.3 s ahead: no jump …
	if p := m.Snapshot().Pos; p-before > 0.1 {
		t.Fatalf("jumped %v → %v", before, p)
	}
	time.Sleep(time.Duration((slewDur + 0.05) * float64(time.Second)))
	exp := before + 0.3 + slewDur + 0.05
	if p := m.Snapshot().Pos; math.Abs(p-exp) > 0.08 { // … but caught up after slewDur
		t.Fatalf("not caught up: %v want %v", p, exp)
	}
	// behind by 0.8 s: it slows down but never goes backwards
	cur := m.Snapshot().Pos
	m.SetPosition(cur-0.8, true)
	last := m.Snapshot().Pos
	for i := 0; i < 30; i++ {
		time.Sleep(20 * time.Millisecond)
		p := m.Snapshot().Pos
		if p < last-1e-6 {
			t.Fatalf("went backwards %v → %v", last, p)
		}
		last = p
	}
	// a real jump is followed at once
	m.SetPosition(100, true)
	if p := m.Snapshot().Pos; math.Abs(p-100) > 0.05 {
		t.Fatalf("real jump not followed: %v", p)
	}
	// pausing keeps the eased position and stops
	m.SetPosition(100.2, true)
	m.SetPlaying(false)
	p1 := m.Snapshot().Pos
	time.Sleep(60 * time.Millisecond)
	if m.Snapshot().Pos != p1 {
		t.Fatal("paused position moved")
	}
}

func TestSoftTint(t *testing.T) {
	for _, c := range []uint32{0xFF8B0000, 0xFF0A1A40, 0xFFFFF7A0, 0xFF2E7D32, 0xFF101010, 0xFFFFFFFF} {
		got := softTint(c)
		if lum(got) < 0.5 {
			t.Fatalf("%x → %x too dark to read on glass (lum %.2f)", c, got, lum(got))
		}
		if got>>24 != 0xFF {
			t.Fatalf("opaque: %x", got)
		}
	}
	if softTint(0xFF808080) != 0xFFE6E6EB {
		t.Fatal("greys stay neutral")
	}
	// the hue survives: a red cover gives a reddish tint
	if c := softTint(0xFFB00020); c>>16&255 <= c&255 {
		t.Fatalf("red cover lost its hue: %x", c)
	}
}
