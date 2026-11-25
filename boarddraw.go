//go:build windows || darwin

package main

// 1.19: Split-flap and LED board lyrics — layout into runs and the drawing of their shapes.

import (
	"math"
)

/* ---------- Split-flap ---------- */

func flapLayout(L *Line, words []string, wi int, p, F, maxW float64, txt, acc uint32, animate bool) ([]run, float32, float32) {
	cw, ch, gap := float32(F*0.66), float32(F*1.06), float32(F*0.07)
	maxChars := int((maxW + float64(gap)) / float64(cw+gap))
	rows := flapRows(words, maxChars)
	if len(rows) > 4 {
		rows = rows[:4]
	}
	fnt := font(cfg.FontName, F*0.74, true)
	e := 99.0
	if animate {
		e = p - L.T
	}
	var runs []run
	y, boxW := float32(0), float32(0)
	k := 0
	for _, row := range rows {
		n := 0
		for j, i := range row {
			if j > 0 {
				n++
			}
			n += len([]rune(words[i]))
		}
		if n > maxChars {
			n = maxChars
		}
		rowW := float32(n)*(cw+gap) - gap
		if rowW > boxW {
			boxW = rowW
		}
		x := -rowW / 2
		col := 0
		put := func(ch0 rune, wordIdx int) {
			if col >= n {
				return
			}
			c := withAlpha(txt, 0.62)
			switch {
			case wordIdx < 0:
			case wordIdx < wi:
				c = txt
			case wordIdx == wi:
				c = acc
			}
			settled, turn, phase := flapState(k, e)
			letter := ch0
			if !settled {
				letter = flapGlyph(k, turn, ch0)
			}
			tx := x + float32(col)*(cw+gap)
			runs = append(runs, run{kind: rkTile, x: tx, y: y, cw: cw, ch: ch, alpha: 1, color: 0xFF1A1A1F})
			if letter != ' ' && (settled || e >= float64(k)*flapStagger) {
				s := string(letter)
				tw, th := measure(fnt, s)
				runs = append(runs, run{text: s, x: tx + (cw-tw)/2, y: y + (ch-th)/2, f: fnt, color: c, alpha: 1})
			}
			ph := float32(0)
			if !settled && e >= float64(k)*flapStagger {
				ph = float32(math.Max(0.001, phase))
			}
			runs = append(runs, run{kind: rkFlap, x: tx, y: y, cw: cw, ch: ch, size: ph, alpha: 1, color: 0xFF1A1A1F})
			col++
			k++
		}
		for j, i := range row {
			if j > 0 {
				put(' ', -1)
			}
			for _, r := range []rune(words[i]) {
				put(r, i)
			}
		}
		y += ch + gap*2
	}
	return runs, boxW, y
}

func drawTile(g uintptr, r run, x, y float32) {
	a := float64(r.alpha)
	path := roundRectPath(x, y, r.cw, r.ch, r.cw*0.1)
	fillPathGradient(g, path, withAlpha(mixColor(r.color, 0xFFFFFFFF, 0.07), a), withAlpha(mixColor(r.color, 0xFF000000, 0.25), a), y, y+r.ch)
	strokePath(g, path, withAlpha(0xFFFFFFFF, 0.1*a), 1)
	freePath(path)
}

// drawFlap: the hinge across the middle, and while a letter changes the flap falling over it.
func drawFlap(g uintptr, r run, x, y float32) {
	a := float64(r.alpha)
	mid := y + r.ch/2
	half := r.ch / 2
	if ph := float64(r.size); ph > 0 {
		if ph < 0.5 { // the top flap folds down onto the hinge
			h := half * float32(math.Cos(ph*math.Pi))
			fillRectGrad(g, x+0.5, mid-h, r.cw-1, h, 0, mid-h, 0, mid, withAlpha(mixColor(r.color, 0xFFFFFFFF, 0.1), a), withAlpha(mixColor(r.color, 0xFF000000, 0.45), a))
		} else { // … and carries on below it, uncovering the new letter
			h := half * float32(-math.Cos(ph*math.Pi))
			fillRectGrad(g, x+0.5, mid, r.cw-1, h, 0, mid, 0, mid+h, withAlpha(mixColor(r.color, 0xFF000000, 0.4), a), withAlpha(mixColor(r.color, 0xFFFFFFFF, 0.05), a))
		}
	}
	line(g, withAlpha(0xFF000000, 0.85*a), float32(math.Max(1.2, float64(r.ch)*0.028)), x, mid, x+r.cw, mid)
	line(g, withAlpha(0xFFFFFFFF, 0.08*a), 1, x, mid+r.ch*0.03, x+r.cw, mid+r.ch*0.03)
	pin := r.cw * 0.07
	fillRect(g, withAlpha(0xFF050506, a), x-pin*0.3, mid-pin, pin, pin*2)
	fillRect(g, withAlpha(0xFF050506, a), x+r.cw-pin*0.7, mid-pin, pin, pin*2)
}

/* ---------- LED board ---------- */

const ledRows = 9

var ledGlyphCache = map[rune][]uint16{}

// ledFit: font size and drop that make a capital letter exactly 7 dot rows tall with its top
// on the first row (measured once on a big "H"); the last 2 rows take the descenders.
var ledFit struct {
	done        bool
	size, shift float32
}

const ledK = 4 // render 4× and boil each 4×4 block down to one dot

func ledCalibrate() {
	ledFit.done = true
	ledFit.size, ledFit.shift = float32(ledRows*ledK)*0.95, -float32(ledRows*ledK)*0.3 // fallback
	const em = 60
	f := font("Segoe UI", em, true)
	tw, th := measure(f, "H")
	W, H := int(tw)+8, int(th)+8
	b := newBitmap(W, H)
	if b == nil {
		return
	}
	defer b.Free()
	gClear(b.g, 0)
	drawText(b.g, f, "H", 2, 0, float32(W), 0xFFFFFFFF)
	top, bot := -1, -1
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			if pixel(b.bmp, x, y)>>24 > 128 {
				if top < 0 {
					top = y
				}
				bot = y
				break
			}
		}
	}
	if top < 0 || bot-top < 10 {
		return
	}
	k := float32(7*ledK) / float32(bot-top+1)
	ledFit.size = em * k
	ledFit.shift = -float32(top) * k
}

// ledGlyph renders a letter big and turns it into dot columns (kept for the session).
func ledGlyph(ch rune) []uint16 {
	if c, ok := ledGlyphCache[ch]; ok {
		return c
	}
	if !ledFit.done {
		ledCalibrate()
	}
	H := ledRows * ledK
	f := font("Segoe UI", float64(ledFit.size), true)
	s := string(ch)
	tw, _ := measure(f, s)
	W := int(tw) + 8
	var cols []uint16
	if b := newBitmap(W, H); b != nil {
		gClear(b.g, 0)
		drawText(b.g, f, s, 2, ledFit.shift, float32(W), 0xFFFFFFFF)
		al := make([]uint8, W*H)
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				al[y*W+x] = uint8(pixel(b.bmp, x, y) >> 24)
			}
		}
		b.Free()
		cols = downsampleGlyph(al, W, H, ledRows)
	}
	if len(ledGlyphCache) > 400 {
		ledGlyphCache = map[rune][]uint16{}
	}
	ledGlyphCache[ch] = cols
	return cols
}

func ledWord(w string) []uint16 {
	var out []uint16
	for i, r := range []rune(w) {
		if i > 0 {
			out = append(out, 0)
		}
		out = append(out, ledGlyph(r)...)
	}
	return out
}

func ledLayout(L *Line, words []string, wi int, p, F, maxW float64, txt, acc uint32, animate bool) ([]run, float32, float32) {
	d := float32(F * 0.15)
	panelW := float32(math.Min(maxW, F*16))
	panel := int(panelW / d)
	panelW = float32(panel) * d
	wc := make([][]uint16, len(words))
	for i, w := range words {
		wc[i] = ledWord(w)
	}
	cols, start, end := ledLine(wc)
	off := float32(1)
	hi0, hi1, sung := 0, 0, len(cols)
	if animate {
		frac := 0.0
		if wi >= 0 && wi < len(L.Words) {
			frac = (p - L.Words[wi].T) / math.Max(0.15, wordDur(L, wi))
		}
		off = float32(ledScroll(start, end, wi, frac, (p-L.T)/0.6, panel))
		if len(cols) < panel*55/100 && wi >= 0 { // a short line fits: no need to scroll it
			off = float32(panel-len(cols)) / 2
		}
		sung = 0
		if wi >= 0 && wi < len(start) {
			sung, hi0, hi1 = start[wi], start[wi], end[wi]
		}
	}
	H := float32(ledRows+1) * d
	r := run{kind: rkLED, x: -panelW/2 - d/2, y: 0, cw: panelW + d, ch: H, size: d, led: cols, ledOff: off,
		hi0: hi0, hi1: hi1, sung: sung, color: acc | 0xFF000000, alpha: 1}
	return []run{r}, panelW + d, H + d*0.4
}

var ledBack struct {
	b   *Bitmap
	key [4]float32
}

func drawLED(g uintptr, r run, x, y float32) {
	d := r.size
	n := int((r.cw - d) / d)
	acc := r.color
	key := [4]float32{float32(n), d, float32(acc & 0xFFFFFF), r.ch}
	if ledBack.b == nil || ledBack.key != key { // the panel and its dark dots, made once
		ledBack.b.Free()
		ledBack.b = newBitmap(int(r.cw)+2, int(r.ch)+2)
		ledBack.key = key
		if b := ledBack.b; b != nil {
			gClear(b.g, 0)
			path := roundRectPath(0.5, 0.5, r.cw, r.ch, d*0.8)
			fillPathColor(b.g, path, 0xF2050506)
			strokePath(b.g, path, 0x30FFFFFF, 1)
			freePath(path)
			off := withAlpha(mixColor(acc, 0xFF000000, 0.55), 0.22)
			for c := 0; c < n; c++ {
				for row := 0; row < ledRows; row++ {
					fillCircle(b.g, off, 0.5+d+float32(c)*d, 0.5+d+float32(row)*d, d*0.33)
				}
			}
		}
	}
	if ledBack.b != nil {
		gImage(g, ledBack.b.bmp, x-0.5, y-0.5, float32(ledBack.b.w), float32(ledBack.b.h))
	}
	a := float64(r.alpha)
	sh := int(math.Floor(float64(r.ledOff))) // real panels move a whole column at a time
	bright := mixColor(acc, 0xFFFFFFFF, 0.45)
	for c := 0; c < n; c++ {
		j := c - sh
		if j < 0 || j >= len(r.led) || r.led[j] == 0 {
			continue
		}
		col := withAlpha(acc, 0.6*a)
		switch {
		case j >= r.hi0 && j < r.hi1:
			col = withAlpha(bright, a)
		case j < r.sung:
			col = withAlpha(acc, a)
		}
		cx := x + d + float32(c)*d
		for row := 0; row < ledRows; row++ {
			if r.led[j]&(1<<uint(row)) == 0 {
				continue
			}
			cy := y + d + float32(row)*d
			if sp := ledDot(col, d); sp != nil {
				h := float32(sp.w) / 2
				gImage(g, sp.bmp, cx-h, cy-h, float32(sp.w), float32(sp.h))
			} else {
				fillCircle(g, withAlpha(col, 0.22), cx, cy, d*0.62)
				fillCircle(g, col, cx, cy, d*0.36)
			}
		}
	}
}

// ledDot: one lit dot (its glow and its core), drawn once per colour and size and then copied —
// a panel lights a few hundred of them every frame.
var ledDots = map[[2]uint32]*Bitmap{}

func ledDot(col uint32, d float32) *Bitmap {
	col = col&0x00FFFFFF | (col>>24)&0xF0<<24 // 16 steps of fading are plenty
	k := [2]uint32{col, uint32(d * 8)}
	if b, ok := ledDots[k]; ok {
		return b
	}
	if len(ledDots) > 96 {
		for _, b := range ledDots {
			b.Free()
		}
		ledDots = map[[2]uint32]*Bitmap{}
	}
	n := int(math.Ceil(float64(d)*1.3)) + 2
	b := newBitmap(n, n)
	if b == nil {
		return nil
	}
	gClear(b.g, 0)
	c := float32(n) / 2
	fillCircle(b.g, withAlpha(col, 0.22), c, c, d*0.62)
	fillCircle(b.g, col, c, c, d*0.36)
	ledDots[k] = b
	return b
}
