package main

// Cinema (1.20): the full-screen view rebuilt from scratch — a calm, Apple TV / streaming-app
// style "now playing" screen. This file holds the parts that are plain maths (layout, lyric
// wrapping and scrolling, the cross-fade) so they can be unit-tested; drawing is in
// cinema_windows.go.

import (
	"math"
	"sync"
)

// rectF is a rectangle in frame pixels.
type rectF struct{ X, Y, W, H float64 }

func (r rectF) has(x, y float64) bool { return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H }
func (r rectF) right() float64        { return r.X + r.W }
func (r rectF) bottom() float64       { return r.Y + r.H }
func (r rectF) grow(d float64) rectF  { return rectF{r.X - d, r.Y - d, r.W + 2*d, r.H + 2*d} }

// overlaps: do two rectangles share any pixel?
func (r rectF) overlaps(o rectF) bool {
	return r.X < o.right() && o.X < r.right() && r.Y < o.bottom() && o.Y < r.bottom()
}

// cinLayout: where everything goes on a W×H screen. With lyrics the cover and its controls sit
// in the left column and the lyrics fill the right; without, the cover block is centred.
type cinLayout struct {
	W, H, U float64 // U: one "point" (H/1080)
	Lyrics  bool
	Art     rectF // the cover
	Text    rectF // title / artist / album (left-aligned under the cover, or centred)
	Centred bool
	Title   float64 // baselines (tops) of the three text lines
	Artist  float64
	Album   float64
	Prog    rectF // the time line (thin track) — hit area is ProgHit
	ProgHit rectF
	Times   float64 // top of the elapsed / remaining times
	Ctrl    rectF   // the control row (dynamic region)
	Buttons [5]rectF
	Next    rectF // the "Up next" line under the controls
	Lyr     rectF // the lyrics column (dynamic region)
	LyrMid  float64
	Clock   rectF // top right
	Toast   rectF // top centre
}

// cinemaLayout lays the screen out. Everything scales with the height; the cover never takes
// more than a third of the width so very tall or narrow screens still work.
func cinemaLayout(W, H float64, withLyrics, big bool) cinLayout {
	U := math.Min(H/1080, W/1500) // a narrow (portrait) screen scales by its width
	l := cinLayout{W: W, H: H, U: U, Lyrics: withLyrics, Centred: !withLyrics}
	A := math.Min(0.43*H, 0.33*W)
	if !withLyrics {
		A = math.Min(0.46*H, 0.4*W)
	}
	if big { // "Big cover": as large as the screen allows, the song and controls still under it
		A = math.Min(H-U*(338+70+104+34), 0.42*W) // (room for the messages above it)
		if !withLyrics {
			A = math.Min(H-U*(338+70+104+34), 0.6*W)
		}
	}
	block := A + U*(36+38+28+22+26+10+24+22+56) // cover + text + time line + controls
	top := math.Max(U*70, (H-block)/2-U*14)
	if big {
		top = math.Max(U*104, (H-block-U*70)/2+U*20)
	}
	x := 0.045 * W
	if !withLyrics {
		x = (W - A) / 2
	}
	l.Art = rectF{math.Round(x), math.Round(top), math.Round(A), math.Round(A)}
	y := l.Art.bottom() + U*36
	l.Title = y
	l.Artist = y + U*38
	l.Album = y + U*66
	l.Text = rectF{l.Art.X, y, A, U * 90}
	y += U * (38 + 28 + 22 + 26)
	l.Prog = rectF{l.Art.X, math.Round(y), A, math.Max(2, math.Round(U*4))}
	l.ProgHit = rectF{l.Art.X - U*8, y - U*16, A + U*16, U * 36}
	l.Times = y + U*14
	y += U * (10 + 24 + 22)
	// (the region redrawn for the buttons: room around them for the play button's glow)
	l.Ctrl = rectF{l.Art.X - U*20, math.Round(y-U*10) + 1, A + U*40, math.Round(U * 76)}
	cy := y + U*28
	cx := l.Art.X + A/2
	// shuffle · previous · PLAY · next · repeat, spread over the cover's width
	sizes := [5]float64{34, 44, 56, 44, 34}
	offs := [5]float64{-0.34, -0.18, 0, 0.18, 0.34}
	for i := range l.Buttons {
		s := sizes[i] * U
		bx := cx + offs[i]*A
		l.Buttons[i] = rectF{math.Round(bx - s/2), math.Round(cy - s/2), math.Round(s), math.Round(s)}
	}
	l.Next = rectF{l.Art.X, y + U*(56+18), A, U * 40}
	if withLyrics {
		// the lyrics stage: the right half, from under the clock almost to the bottom
		lx := math.Max(0.47*W, l.Art.right()+0.045*W)
		l.Lyr = rectF{math.Round(lx), math.Round(0.1 * H), math.Round(0.965*W - lx), math.Round(0.86 * H)}
		l.LyrMid = H * 0.52
	}
	l.Clock = rectF{W - U*420, U * 34, U * 380, U * 56}
	// messages: over the cover's column, above the cover
	acx := l.Art.X + l.Art.W/2
	tw := math.Min(U*720, 2*(acx-U*10))
	if !withLyrics {
		tw = math.Min(U*720, W*0.9)
	}
	l.Toast = rectF{acx - tw/2, U * 36, tw, U * 54}
	return l
}

/* ---------- lyrics: wrapping and scrolling ---------- */

// lyrBlock: one lyric line wrapped into rows. Word i sits in row Row[i] at X[i] (from the
// row's left), W[i] wide.
type lyrBlock struct {
	Rows  int
	Row   []int
	X, Wd []float64
	H     float64 // height of the block
}

// wrapBlock lays words (with their widths) into rows no wider than maxW.
func wrapBlock(widths []float64, space, maxW, lineH float64) lyrBlock {
	b := lyrBlock{Row: make([]int, len(widths)), X: make([]float64, len(widths)), Wd: append([]float64(nil), widths...)}
	row, x := 0, 0.0
	for i, w := range widths {
		if x > 0 && x+space+w > maxW {
			row++
			x = 0
		} else if x > 0 {
			x += space
		}
		b.Row[i], b.X[i] = row, x
		x += w
	}
	b.Rows = row + 1
	if len(widths) == 0 {
		b.Rows = 1
	}
	b.H = float64(b.Rows) * lineH
	return b
}

// lyrOffsets: the top of every block when block cur is centred on mid (gap between blocks).
func lyrOffsets(heights []float64, cur int, gap, mid float64) []float64 {
	ys := make([]float64, len(heights))
	if len(heights) == 0 {
		return ys
	}
	if cur < 0 {
		cur = 0 // before the first line: the first line waits in the middle
	}
	if cur >= len(heights) {
		cur = len(heights) - 1
	}
	ys[cur] = mid - heights[cur]/2
	for i := cur - 1; i >= 0; i-- {
		ys[i] = ys[i+1] - gap - heights[i]
	}
	for i := cur + 1; i < len(heights); i++ {
		ys[i] = ys[i-1] + heights[i-1] + gap
	}
	return ys
}

// springTo moves v toward target smoothly (frame-rate independent): about 90 % of the way in
// a quarter of a second, never overshooting.
func springTo(v, target, dt float64) float64 {
	if math.Abs(target-v) < 0.25 {
		return target
	}
	return target + (v-target)*math.Exp(-dt*9)
}

// lyrAlpha: how bright a block is — the line being sung full, the others dim, fading out
// toward the top and bottom of the lyrics column (top..bottom).
func lyrAlpha(dist int, y, h, top, bottom float64) float64 {
	a := 0.34
	switch {
	case dist == 0:
		a = 1
	case dist < 0:
		a = 0.26 // what was sung
	}
	edge := (bottom - top) * 0.22
	c := y + h/2
	if c < top+edge {
		a *= math.Max(0, (c-top)/edge)
	} else if c > bottom-edge {
		a *= math.Max(0, (bottom-c)/edge)
	}
	return a
}

// gapDots: during a long pause in the singing (an intro, a solo) three dots fill up one by one
// until the next line. Returns how far (0..1) and whether to show them.
func gapDots(p, from, to float64) (float64, bool) {
	if to-from < 4 || p < from || p >= to {
		return 0, false
	}
	return (p - from) / (to - from), true
}

/* ---------- the cross-fade between two frames ---------- */

// mixFrames: dst = a·(1-t) + b·t for premultiplied 32-bit pixels, split over all cores.
func mixFrames(dst, a, b []uint32, t float64) {
	n := len(dst)
	if len(a) < n || len(b) < n {
		return
	}
	k := uint32(math.Round(math.Max(0, math.Min(1, t)) * 256))
	if k == 0 {
		copy(dst, a[:n])
		return
	}
	if k == 256 {
		copy(dst, b[:n])
		return
	}
	work := func(lo, hi int) {
		ik := 256 - k
		for i := lo; i < hi; i++ {
			x, y := a[i], b[i]
			rb := ((x&0x00FF00FF)*ik + (y&0x00FF00FF)*k) >> 8 & 0x00FF00FF
			ag := ((x>>8)&0x00FF00FF*ik + (y>>8)&0x00FF00FF*k) & 0xFF00FF00
			dst[i] = rb | ag
		}
	}
	parts := workers()
	if parts > 16 {
		parts = 16
	}
	if n < 1<<16 || parts < 2 {
		work(0, n)
		return
	}
	var wg sync.WaitGroup
	step := (n + parts - 1) / parts
	for lo := 0; lo < n; lo += step {
		hi := lo + step
		if hi > n {
			hi = n
		}
		wg.Add(1)
		go func(lo, hi int) { defer wg.Done(); work(lo, hi) }(lo, hi)
	}
	wg.Wait()
}

// grain adds a faint, fixed noise (±amp) to an opaque image: smooth blurred backgrounds band
// into visible steps on 8-bit screens without it. Split over all cores.
// grainTab: ±amp noise for 65536 pixels, made once per amp (the screen repeats it; noise has
// no pattern to see), so grain costs a table look-up instead of a hash per pixel.
var grainTab = map[int][]int8{}
var grainMu sync.Mutex

func grainTable(amp int) []int8 {
	grainMu.Lock()
	defer grainMu.Unlock()
	if t, ok := grainTab[amp]; ok {
		return t
	}
	t := make([]int8, 1<<16)
	for i := range t {
		h := hash32(uint32(i)*2654435761 + 0x9E3779B9)
		t[i] = int8(int(h%uint32(2*amp+1)) - amp)
	}
	grainTab[amp] = t
	return t
}

func grain(px []uint32, amp int) {
	if amp <= 0 {
		return
	}
	tab := (*[1 << 16]int8)(grainTable(amp))
	work := func(lo, hi int) {
		seg := px[lo:hi]
		for j, c := range seg {
			d := int32(tab[uint16(lo+j)])
			r, g, b := int32(c>>16&255)+d, int32(c>>8&255)+d, int32(c&255)+d
			if uint32(r|g|b) > 255 { // only near black or white: clamp
				r, g, b = min(max(r, 0), 255), min(max(g, 0), 255), min(max(b, 0), 255)
			}
			seg[j] = c&0xFF000000 | uint32(r)<<16 | uint32(g)<<8 | uint32(b)
		}
	}
	n := len(px)
	parts := workers()
	if parts > 16 {
		parts = 16
	}
	if n < 1<<16 || parts < 2 {
		work(0, n)
		return
	}
	var wg sync.WaitGroup
	step := (n + parts - 1) / parts
	for lo := 0; lo < n; lo += step {
		hi := min(lo+step, n)
		wg.Add(1)
		go func(lo, hi int) { defer wg.Done(); work(lo, hi) }(lo, hi)
	}
	wg.Wait()
}

func clampByte(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// blendWhite lays white text onto an opaque picture: src holds the text (premultiplied white,
// so its alpha is the ink), sw×sh; the part [sx0,sx1)×[sy0,sy1) of it lands with its corner
// (0,0) at (dx,dy) of dst (dw×dh), only inside clip (x0,y0,x1,y1), at strength k/256.
// This is what GDI+ did with a colour matrix per line — 10× slower; that made Cinema stutter.
func blendWhite(dst []uint32, dw, dh int, src []uint32, sw, sh, dx, dy, sx0, sy0, sx1, sy1 int, clip [4]int, k uint32) {
	if k == 0 {
		return
	}
	if k > 256 {
		k = 256
	}
	sx0, sy0 = max(sx0, 0), max(sy0, 0)
	sx1, sy1 = min(sx1, sw), min(sy1, sh)
	// the destination rectangle, clipped
	x0, y0 := max(dx+sx0, clip[0], 0), max(dy+sy0, clip[1], 0)
	x1, y1 := min(dx+sx1, clip[2], dw), min(dy+sy1, clip[3], dh)
	if x1 <= x0 || y1 <= y0 || len(dst) < dw*dh || len(src) < sw*sh {
		return
	}
	job := func(ya, yb int) {
		for y := ya; y < yb; y++ {
			srow := src[(y-dy)*sw:]
			drow := dst[y*dw:]
			for x := x0; x < x1; x++ {
				a := (srow[x-dx] >> 24) * k >> 8
				if a == 0 {
					continue
				}
				d := drow[x]
				if a >= 255 {
					drow[x] = 0xFFFFFFFF
					continue
				}
				// d + (255 - d)·a/255 for each colour (white ink over the picture)
				r, g, b := d>>16&255, d>>8&255, d&255
				r += ((255-r)*a + 127) / 255
				g += ((255-g)*a + 127) / 255
				b += ((255-b)*a + 127) / 255
				drow[x] = 0xFF000000 | r<<16 | g<<8 | b
			}
		}
	}
	if (x1-x0)*(y1-y0) < 1<<17 {
		job(y0, y1)
		return
	}
	parallelRows(y1-y0, func(a, b int) { job(y0+a, y0+b) })
}

/* ---------- a real glow: blur the ink, lay it under the text as light ---------- */

// bloomFrom shrinks the ink (alpha) of a premultiplied w×h image by k and blurs it (three box
// passes of the given radius ≈ a gaussian). The small result (gw×gh) is what addBloom spreads.
func bloomFrom(src []uint32, w, h, k, radius int) ([]uint16, int, int) {
	return bloomFromBox(src, w, h, k, radius, [4]int{0, 0, w, h})
}

// bloomFromBox: bloomFrom when only box (x0, y0, x1, y1) holds anything.
func bloomFromBox(src []uint32, w, h, k, radius int, box [4]int) ([]uint16, int, int) {
	if k < 1 || w <= 0 || h <= 0 || len(src) < w*h {
		return nil, 0, 0
	}
	gw, gh := (w+k-1)/k, (h+k-1)/k
	sums := make([]uint32, gw*gh)
	gx0, gy0 := max(0, box[0]/k), max(0, box[1]/k)
	gx1, gy1 := min(gw, (box[2]+k-1)/k), min(gh, (box[3]+k-1)/k)
	if gx1 <= gx0 || gy1 <= gy0 {
		gx0, gy0, gx1, gy1 = 0, 0, 0, 0
	}
	parallelRows(gy1-gy0, func(ga, gb int) {
		for gy := gy0 + ga; gy < gy0+gb; gy++ {
			out := sums[gy*gw : gy*gw+gw]
			for y := gy * k; y < min(gy*k+k, h); y++ {
				row := src[y*w : y*w+w]
				for gx := gx0; gx < gx1; gx++ {
					var t uint32
					for _, p := range row[gx*k : min(gx*k+k, w)] {
						t += p >> 24
					}
					out[gx] += t
				}
			}
		}
	})
	a := make([]uint16, gw*gh) // 0..255·16 (a little extra precision for the blur)
	kk := uint32(k * k)
	for i, v := range sums {
		a[i] = uint16(v * 16 / kk)
	}
	tmp := make([]uint16, len(a))
	for pass := 0; pass < 3; pass++ {
		boxBlur(a, tmp, gw, gh, radius, true)
		boxBlur(tmp, a, gw, gh, radius, false)
	}
	return a, gw, gh
}

// boxBlur: one running-sum box blur along rows (horiz) or columns, src → dst.
func boxBlur(src, dst []uint16, w, h, r int, horiz bool) {
	n, m, step, lineStep := w, h, 1, w
	if !horiz {
		n, m, step, lineStep = h, w, w, 1
	}
	div := uint32(2*r + 1)
	for line := 0; line < m; line++ {
		base := line * lineStep
		var sum uint32
		for i := -r; i <= r; i++ {
			j := min(max(i, 0), n-1)
			sum += uint32(src[base+j*step])
		}
		for i := 0; i < n; i++ {
			dst[base+i*step] = uint16(sum / div)
			out, in := max(i-r, 0), min(i+r+1, n-1)
			sum += uint32(src[base+in*step])
			sum -= uint32(src[base+out*step])
		}
	}
}

// addBloom lays the glow (gw×gh, made with bloomFrom's k) onto opaque dst, its corner at
// (dx,dy), inside clip, as coloured light (screen blend) at the given strength.
func addBloom(dst []uint32, dw, dh int, glow []uint16, gw, gh, k, dx, dy int, clip [4]int, col uint32, strength float64) {
	if len(glow) < gw*gh || gw < 2 || gh < 2 || strength <= 0 {
		return
	}
	x0, y0 := max(dx, clip[0], 0), max(dy, clip[1], 0)
	x1, y1 := min(dx+gw*k, clip[2], dw), min(dy+gh*k, clip[3], dh)
	if x1 <= x0 || y1 <= y0 {
		return
	}
	cr, cg, cb := col>>16&255, col>>8&255, col&255
	st := uint32(math.Min(4, strength) * 256)
	// the sample columns, once (bilinear in 8-bit fractions)
	n := x1 - x0
	xs0, xs1, txs := make([]int32, n), make([]int32, n), make([]uint32, n)
	for i := 0; i < n; i++ {
		fx := (float64(x0+i-dx)+0.5)/float64(k) - 0.5
		ix := int(math.Floor(fx))
		txs[i] = uint32((fx - float64(ix)) * 256)
		xs0[i], xs1[i] = int32(min(max(ix, 0), gw-1)), int32(min(max(ix+1, 0), gw-1))
	}
	// where each light row has anything: whole empty rows are skipped, the rest only between
	// their first and last lit sample (most of the stage is dark)
	lo, hi := make([]int32, gh), make([]int32, gh)
	for y := 0; y < gh; y++ {
		row := glow[y*gw : y*gw+gw]
		a, b := -1, -1
		for x, v := range row {
			if v != 0 {
				if a < 0 {
					a = x
				}
				b = x
			}
		}
		lo[y], hi[y] = int32(a), int32(b)
	}
	// glow column → first / last screen column that samples it
	colFrom := func(gx int32) int { return max(0, (int(gx)-1)*k+dx-x0) }
	colTo := func(gx int32) int { return min(n, (int(gx)+2)*k+dx-x0) }
	job := func(ya, yb int) {
		for y := ya; y < yb; y++ {
			fy := (float64(y-dy)+0.5)/float64(k) - 0.5
			iy := int(math.Floor(fy))
			ty := uint32((fy - float64(iy)) * 256)
			iy0, iy1 := min(max(iy, 0), gh-1), min(max(iy+1, 0), gh-1)
			if lo[iy0] < 0 && lo[iy1] < 0 {
				continue
			}
			gl, gr := lo[iy0], hi[iy0]
			if gl < 0 || (lo[iy1] >= 0 && lo[iy1] < gl) {
				gl = lo[iy1]
			}
			if hi[iy1] > gr {
				gr = hi[iy1]
			}
			ia, ib := colFrom(gl), colTo(gr)
			if ib <= ia {
				continue
			}
			r0, r1 := glow[iy0*gw:iy0*gw+gw], glow[iy1*gw:iy1*gw+gw]
			drow := dst[y*dw+x0 : y*dw+x1]
			for i := ia; i < ib; i++ {
				a0, a1 := r0[xs0[i]], r0[xs1[i]]
				b0, b1 := r1[xs0[i]], r1[xs1[i]]
				if a0|a1|b0|b1 == 0 {
					continue
				}
				tx := txs[i]
				top := uint32(a0)*(256-tx) + uint32(a1)*tx
				bot := uint32(b0)*(256-tx) + uint32(b1)*tx
				v := (top*(256-ty) + bot*ty) >> 16 // 0..255·16
				a := v * st >> 12                  // 0..255 (times strength)
				if a == 0 {
					continue
				}
				if a > 255 {
					a = 255
				}
				d := drow[i]
				// screen: lighter where the light falls, never past white (x/255 ≈ x·257>>16)
				lr, lg, lb := cr*a*257>>16, cg*a*257>>16, cb*a*257>>16
				r, g, b := d>>16&255, d>>8&255, d&255
				r += lr - (r*lr*257)>>16
				g += lg - (g*lg*257)>>16
				b += lb - (b*lb*257)>>16
				drow[i] = 0xFF000000 | r<<16 | g<<8 | b
			}
		}
	}
	if (x1-x0)*(y1-y0) < 1<<17 {
		job(y0, y1)
		return
	}
	parallelRows(y1-y0, func(a, b int) { job(y0+a, y0+b) })
}

// rotateOver draws src (sw×sh premultiplied BGRA — a round record label) over dst, turned by
// ang degrees (clockwise on screen, like GDI+'s RotateTransform), L pixels wide, centred on
// (cx, cy), with opacity a, only inside clip [x0 y0 x1 y1]. Bilinear and spread over every
// core: much faster and sharper than GDI+ turning a big bitmap each frame.
func rotateOver(dst []uint32, dw, dh int, src []uint32, sw, sh int, cx, cy, L, ang, a float64, clip [4]int) {
	if L < 2 || a <= 0 || sw < 2 || sh < 2 || len(src) < sw*sh || len(dst) < dw*dh {
		return
	}
	x0, y0 := max(clip[0], 0, int(math.Floor(cx-L/2-1))), max(clip[1], 0, int(math.Floor(cy-L/2-1)))
	x1, y1 := min(clip[2], dw, int(math.Ceil(cx+L/2+1))), min(clip[3], dh, int(math.Ceil(cy+L/2+1)))
	if x1 <= x0 || y1 <= y0 {
		return
	}
	s := float64(sw) / L
	sn, cs := math.Sincos(ang * math.Pi / 180)
	const fp = 1 << 16
	dux, dvx := int64(cs*s*fp), int64(-sn*s*fp)
	ka := uint32(math.Min(1, a)*256 + 0.5)
	r2 := (L/2 + 1) * (L/2 + 1)
	umax, vmax := int64(sw-1)*fp, int64(sh-1)*fp
	rows := func(ya, yb int) {
		for y := ya; y < yb; y++ {
			dy := float64(y) + 0.5 - cy
			if dy*dy > r2 {
				continue
			}
			half := math.Sqrt(r2 - dy*dy) // only the part of the row inside the disc
			xa, xb := max(x0, int(cx-half)), min(x1, int(math.Ceil(cx+half)))
			if xb <= xa {
				continue
			}
			dx := float64(xa) + 0.5 - cx
			u := int64(((dx*cs+dy*sn)*s + float64(sw)/2 - 0.5) * fp)
			v := int64(((-dx*sn+dy*cs)*s + float64(sh)/2 - 0.5) * fp)
			row := dst[y*dw : y*dw+dw]
			for x := xa; x < xb; x, u, v = x+1, u+dux, v+dvx {
				if u < 0 || v < 0 || u >= umax || v >= vmax {
					continue
				}
				iu, iv := int(u>>16), int(v>>16)
				fu, fv := uint32(u>>8)&0xFF, uint32(v>>8)&0xFF
				i := iv*sw + iu
				p00, p01, p10, p11 := src[i], src[i+1], src[i+sw], src[i+sw+1]
				if p00|p01|p10|p11 == 0 {
					continue
				}
				w00 := (256 - fu) * (256 - fv) >> 8
				w01 := fu * (256 - fv) >> 8
				w10 := (256 - fu) * fv >> 8
				w11 := 256 - w00 - w01 - w10 // the four always add up to 256
				// two channels at a time (0x00FF00FF lanes)
				rb := ((p00&0xFF00FF)*w00 + (p01&0xFF00FF)*w01 + (p10&0xFF00FF)*w10 + (p11&0xFF00FF)*w11) >> 8 & 0xFF00FF
				ag := ((p00>>8&0xFF00FF)*w00 + (p01>>8&0xFF00FF)*w01 + (p10>>8&0xFF00FF)*w10 + (p11>>8&0xFF00FF)*w11) >> 8 & 0xFF00FF
				if ka < 256 {
					rb = (rb*ka + 0x800080) >> 8 & 0xFF00FF
					ag = (ag*ka + 0x800080) >> 8 & 0xFF00FF
				}
				sa := ag >> 16
				if sa >= 255 {
					row[x] = rb | ag<<8
					continue
				}
				d := row[x]
				inv := 255 - sa
				drb := mul255(d&0xFF00FF, inv)
				dag := mul255(d>>8&0xFF00FF, inv)
				row[x] = (rb + drb) | (ag+dag)<<8
			}
		}
	}
	n := workers()
	if n < 2 || (x1-x0)*(y1-y0) < 40000 {
		rows(y0, y1)
		return
	}
	var wg sync.WaitGroup
	step := (y1 - y0 + n - 1) / n
	for ya := y0; ya < y1; ya += step {
		wg.Add(1)
		go func(ya, yb int) { defer wg.Done(); rows(ya, yb) }(ya, min(y1, ya+step))
	}
	wg.Wait()
}

// overBlit lays src (premultiplied BGRA, sw×sh) over dst with its top-left at (dx, dy), at
// opacity alpha (0–255), inside clip: what AlphaBlend does, but skipping the empty pixels
// (lyrics are mostly empty) and on every core.
// mul255 multiplies both 8-bit lanes of x (0x00FF00FF) by k/255, rounded exactly.
func mul255(x, k uint32) uint32 {
	t := x*k + 0x800080
	return (t + (t >> 8 & 0xFF00FF)) >> 8 & 0xFF00FF
}

func overBlit(dst []uint32, dw, dh int, src []uint32, sw, sh, dx, dy int, clip [4]int, alpha int) {
	x0, y0 := max(dx, clip[0], 0), max(dy, clip[1], 0)
	x1, y1 := min(dx+sw, clip[2], dw), min(dy+sh, clip[3], dh)
	if x1 <= x0 || y1 <= y0 || alpha <= 0 || len(src) < sw*sh || len(dst) < dw*dh {
		return
	}
	ka := uint32(min(alpha, 255))
	rows := func(ya, yb int) {
		for y := ya; y < yb; y++ {
			srow := src[(y-dy)*sw+(x0-dx) : (y-dy)*sw+(x1-dx)]
			drow := dst[y*dw+x0 : y*dw+x1]
			for i, s := range srow {
				if s == 0 {
					continue
				}
				if ka < 255 {
					s = mul255(s&0xFF00FF, ka) | mul255(s>>8&0xFF00FF, ka)<<8
				}
				sa := s >> 24
				if sa == 255 {
					drow[i] = s
					continue
				}
				d := drow[i]
				inv := 255 - sa
				drow[i] = s + (mul255(d&0xFF00FF, inv) | mul255(d>>8&0xFF00FF, inv)<<8)
			}
		}
	}
	n := workers()
	if n < 2 || (x1-x0)*(y1-y0) < 60000 {
		rows(y0, y1)
		return
	}
	var wg sync.WaitGroup
	step := (y1 - y0 + n - 1) / n
	for ya := y0; ya < y1; ya += step {
		wg.Add(1)
		go func(ya, yb int) { defer wg.Done(); rows(ya, yb) }(ya, min(y1, ya+step))
	}
	wg.Wait()
}

// fitScale: how much bigger a line can be drawn so that, wrapped at maxW, it fills the box —
// as wide as maxW at most and as tall as maxH at most (rows of lineH each). widths are the
// words at the normal size; the answer is between lo and hi.
func fitScale(widths []float64, space, lineH, maxW, maxH, lo, hi float64) float64 {
	if len(widths) == 0 || maxW <= 0 || maxH <= 0 {
		return lo
	}
	fits := func(s float64) bool {
		rows, rowW := 1, 0.0
		for _, w := range widths {
			w *= s
			if w > maxW {
				return false // one word wider than the box
			}
			if rowW > 0 && rowW+space*s+w > maxW {
				rows++
				rowW = w
			} else if rowW > 0 {
				rowW += space*s + w
			} else {
				rowW = w
			}
		}
		return float64(rows)*lineH*s <= maxH
	}
	if !fits(lo) {
		return lo
	}
	if fits(hi) {
		return hi
	}
	a, b := lo, hi
	for i := 0; i < 18; i++ {
		m := (a + b) / 2
		if fits(m) {
			a = m
		} else {
			b = m
		}
	}
	return a
}

// overBlitFeather is overBlit with src fading out over its last `feather` pixels on every side
// (a record travelling past the edge of its own picture dissolves there instead of being cut).
func overBlitFeather(dst []uint32, dw, dh int, src []uint32, sw, sh, dx, dy int, clip [4]int, feather int) {
	if feather <= 0 {
		overBlit(dst, dw, dh, src, sw, sh, dx, dy, clip, 255)
		return
	}
	x0, y0 := max(dx, clip[0], 0), max(dy, clip[1], 0)
	x1, y1 := min(dx+sw, clip[2], dw), min(dy+sh, clip[3], dh)
	if x1 <= x0 || y1 <= y0 || len(src) < sw*sh || len(dst) < dw*dh {
		return
	}
	ramp := func(d int) uint32 { return uint32(min(255, max(0, d*255/feather))) }
	kx := make([]uint32, x1-x0) // the edge fade across a row, worked out once
	for x := x0; x < x1; x++ {
		sx := x - dx
		kx[x-x0] = ramp(min(sx, sw-1-sx))
	}
	parallelArea((x1-x0)*(y1-y0), y1-y0, func(ya, yb int) {
		for y := y0 + ya; y < y0+yb; y++ {
			sy := y - dy
			ky := ramp(min(sy, sh-1-sy))
			srow := src[sy*sw+x0-dx : sy*sw+x1-dx]
			drow := dst[y*dw+x0 : y*dw+x1]
			for i, s := range srow {
				if s == 0 {
					continue
				}
				k := min(ky, kx[i])
				if k < 255 {
					s = mul255(s&0xFF00FF, k) | mul255(s>>8&0xFF00FF, k)<<8
				}
				sa := s >> 24
				if sa == 255 {
					drow[i] = s
					continue
				}
				d := drow[i]
				inv := 255 - sa
				drow[i] = s + (mul255(d&0xFF00FF, inv) | mul255(d>>8&0xFF00FF, inv)<<8)
			}
		}
	})
}

// armPose: where the full-screen tonearm's parts are. P pivot, K where the tube bends into the
// headshell, N the needle, W0–W1 the counterweight, Rest the post it lies on.
type armPose struct {
	Px, Py, Kx, Ky, Nx, Ny, W0x, W0y, W1x, W1y, RestX, RestY, Theta float64
}

// tonearm: a record of radius R centred on (cx, cy); the pivot sits up and to the right. t is 0
// with the arm on its rest beside the record and 1 with the needle in the groove (it may go a
// little past either end while it settles). progress 0–1 walks the needle inwards.
func tonearm(cx, cy, R, progress, t float64) armPose {
	px, py := cx+1.10*R, cy-0.74*R
	L := 1.30 * R
	rt := R * (0.93 - 0.27*math.Min(1, math.Max(0, progress)))
	// the needle on the circle of radius rt that is L from the pivot (the lower of the two)
	dx, dy := cx-px, cy-py
	d := math.Hypot(dx, dy)
	a := (L*L - rt*rt + d*d) / (2 * d)
	h := math.Sqrt(math.Max(0, L*L-a*a))
	bx, by := px+a*dx/d, py+a*dy/d
	nx, ny := bx-h*dy/d, by+h*dx/d
	if n2y := by - h*dx/d; n2y > ny {
		nx, ny = bx+h*dy/d, n2y
	}
	play := math.Atan2(ny-py, nx-px)
	rest := math.Pi/2 - 0.12
	th := rest + (play-rest)*t
	ux, uy := math.Cos(th), math.Sin(th)
	bend := th - 0.16 // the tube runs a little outside the needle line, then the headshell turns in
	p := armPose{Px: px, Py: py, Theta: th}
	p.Nx, p.Ny = px+L*ux, py+L*uy
	p.Kx, p.Ky = px+0.80*L*math.Cos(bend), py+0.80*L*math.Sin(bend)
	p.W0x, p.W0y = px-0.13*R*ux, py-0.13*R*uy
	p.W1x, p.W1y = px-0.36*R*ux, py-0.36*R*uy
	p.RestX, p.RestY = px+0.86*L*math.Cos(rest-0.16), py+0.86*L*math.Sin(rest-0.16)
	return p
}

// armSpring moves the arm toward target like a damped spring (a slight settle at the end).
func armSpring(x, v, target, dt float64) (float64, float64) {
	dt = math.Min(dt, 1.0/20)
	for s := 0; s < 4; s++ { // a few small steps: steady at any frame rate
		h := dt / 4
		acc := 34*(target-x) - 9.5*v
		v += acc * h
		x += v * h
	}
	if math.Abs(target-x) < 0.0005 && math.Abs(v) < 0.001 {
		return target, 0
	}
	return x, v
}

// sharpen: a light unsharp mask (3×3) on the opaque pixels — what a picture loses in being
// resized, given back. amount 0–1. See-through pixels (round corners) are left alone.
func sharpen(px []uint32, w, h int, amount float64) {
	if w < 3 || h < 3 || amount <= 0 || len(px) < w*h {
		return
	}
	src := append([]uint32(nil), px...)
	k := int32(amount * 256)
	parallelRows(h-2, func(a, b int) {
		for y := a + 1; y < b+1; y++ {
			for x := 1; x < w-1; x++ {
				i := y*w + x
				c := src[i]
				if c>>24 != 255 {
					continue
				}
				var out uint32 = 0xFF000000
				for sh := uint(0); sh < 24; sh += 8 {
					sum := int32(0)
					opaque := true
					for dy := -1; dy <= 1; dy++ {
						for dx := -1; dx <= 1; dx++ {
							n := src[i+dy*w+dx]
							if n>>24 != 255 {
								opaque = false
							}
							sum += int32(n >> sh & 255)
						}
					}
					v := int32(c >> sh & 255)
					if opaque {
						v += (v - sum/9) * k >> 8
					}
					out |= uint32(min(max(v, 0), 255)) << sh
				}
				px[i] = out
			}
		}
	})
}

// blurRGB softens an opaque picture (box blur, horizontal then vertical, `passes` times —
// three passes look like a gaussian). Edges repeat their last pixel.
func blurRGB(px []uint32, w, h, r, passes int) {
	if r < 1 || w < 2 || h < 2 || len(px) < w*h {
		return
	}
	tmp := make([]uint32, w*h)
	line := func(src, dst []uint32, n, stride, start int) {
		var sr, sg, sb int
		at := func(i int) uint32 { return src[start+min(max(i, 0), n-1)*stride] }
		for i := -r; i <= r; i++ {
			c := at(i)
			sr, sg, sb = sr+int(c>>16&255), sg+int(c>>8&255), sb+int(c&255)
		}
		d := 2*r + 1
		for i := 0; i < n; i++ {
			dst[start+i*stride] = 0xFF000000 | uint32(sr/d)<<16 | uint32(sg/d)<<8 | uint32(sb/d)
			a, b := at(i-r), at(i+r+1)
			sr += int(b>>16&255) - int(a>>16&255)
			sg += int(b>>8&255) - int(a>>8&255)
			sb += int(b&255) - int(a&255)
		}
	}
	for p := 0; p < passes; p++ {
		parallelRows(h, func(a, b int) {
			for y := a; y < b; y++ {
				line(px, tmp, w, 1, y*w)
			}
		})
		parallelRows(w, func(a, b int) {
			for x := a; x < b; x++ {
				line(tmp, px, h, w, x)
			}
		})
	}
}

// ring is one soft circle: radius, width and opacity (0–1).
type ring struct{ r, w, a float64 }

// ringsOver draws anti-aliased circles of colour col (RGB) around (cx, cy) over dst
// (premultiplied BGRA), only inside clip — what a GDI+ circle stroke does, but for many rings at
// once and on every core: the alpha along the radius is worked out once (half-pixel steps), then
// each pixel of the band looks its radius up.
func ringsOver(dst []uint32, dw, dh int, cx, cy float64, rings []ring, col uint32, clip [4]int) {
	if len(rings) == 0 || len(dst) < dw*dh {
		return
	}
	r0, r1 := math.Inf(1), 0.0
	for _, g := range rings {
		r0 = math.Min(r0, g.r-g.w/2-1)
		r1 = math.Max(r1, g.r+g.w/2+1)
	}
	r0 = math.Max(0, r0)
	const step = 0.5
	n := int((r1-r0)/step) + 2
	prof := make([]uint16, n) // alpha × 256
	for k := range prof {
		d := r0 + float64(k)*step
		keep := 1.0 // what shows through every ring at this radius
		for _, g := range rings {
			cov := math.Max(0, math.Min(1, g.w/2+0.5-math.Abs(d-g.r)))
			keep *= 1 - cov*math.Min(1, g.a)
		}
		prof[k] = uint16((1 - keep) * 256)
	}
	x0, y0 := max(clip[0], 0, int(cx-r1)), max(clip[1], 0, int(cy-r1))
	x1, y1 := min(clip[2], dw, int(cx+r1)+1), min(clip[3], dh, int(cy+r1)+1)
	if x1 <= x0 || y1 <= y0 {
		return
	}
	cr, cg, cb := col>>16&255, col>>8&255, col&255
	in2, out2 := r0*r0, r1*r1
	parallelArea((x1-x0)*(y1-y0), y1-y0, func(a, b int) {
		for y := y0 + a; y < y0+b; y++ {
			dy := float64(y) + 0.5 - cy
			dy2 := dy * dy
			if dy2 > out2 {
				continue
			}
			// only the band between r0 and r1 on this row
			half := math.Sqrt(out2 - dy2)
			xa, xb := max(x0, int(cx-half)), min(x1, int(cx+half)+1)
			var inHalf float64
			if dy2 < in2 {
				inHalf = math.Sqrt(in2 - dy2)
			}
			row := dst[y*dw : y*dw+dw]
			for x := xa; x < xb; x++ {
				dx := float64(x) + 0.5 - cx
				if inHalf > 0 && math.Abs(dx) < inHalf-1 {
					if t := int(cx+inHalf) - 2; dx < 0 && t > x {
						x = t // jump over the hole in the middle
					}
					continue
				}
				f := (math.Sqrt(dx*dx+dy2) - r0) / step
				k := int(f)
				if k < 0 || k+1 >= n {
					continue
				}
				t := uint32((f - float64(k)) * 256)
				av := (uint32(prof[k])*(256-t) + uint32(prof[k+1])*t) >> 8 // 0..256
				if av == 0 {
					continue
				}
				d := row[x]
				inv := 256 - av
				r := (cr*av + (d>>16&255)*inv) >> 8
				g := (cg*av + (d>>8&255)*inv) >> 8
				bb := (cb*av + (d&255)*inv) >> 8
				al := (255*av + (d>>24)*inv) >> 8
				row[x] = al<<24 | r<<16 | g<<8 | bb
			}
		}
	})
}

// glowOver lays a soft round light (GDI+'s path-gradient glow: col at the centre fading
// linearly to nothing at radius r) over dst, inside clip, leaving out a hole (the record).
func glowOver(dst []uint32, dw, dh int, cx, cy, r float64, col uint32, clip [4]int, hx, hy, hr float64) {
	a0 := float64(col>>24) / 255
	if a0 <= 0 || r <= 1 || len(dst) < dw*dh {
		return
	}
	x0, y0 := max(int(cx-r), clip[0], 0), max(int(cy-r), clip[1], 0)
	x1, y1 := min(int(cx+r)+1, clip[2], dw), min(int(cy+r)+1, clip[3], dh)
	if x1 <= x0 || y1 <= y0 {
		return
	}
	// the colour at each distance, looked up by the squared distance (no square root per pixel)
	const N = 1024
	var lut [N + 1]uint32
	for k := 0; k <= N; k++ {
		a := a0 * (1 - math.Sqrt(float64(k)/N))
		sa := uint32(a*255 + 0.5)
		lut[k] = sa<<24 | uint32(float64(col>>16&255)*a+0.5)<<16 | uint32(float64(col>>8&255)*a+0.5)<<8 | uint32(float64(col&255)*a+0.5)
	}
	r2 := r * r
	scale := N / r2
	hin2, hout2 := 0.0, 0.0
	if hr > 0 {
		hin2, hout2 = math.Max(0, hr-0.5)*math.Max(0, hr-0.5), (hr+0.5)*(hr+0.5)
	}
	rows := func(ya, yb int) {
		for y := ya; y < yb; y++ {
			fy := float64(y) + 0.5 - cy
			if fy*fy >= r2 {
				continue
			}
			half := math.Sqrt(r2 - fy*fy)
			xa, xb := max(x0, int(cx-half)), min(x1, int(cx+half)+1)
			hy2 := float64(y) + 0.5 - hy
			row := dst[y*dw : y*dw+dw]
			for x := xa; x < xb; x++ {
				fx := float64(x) + 0.5 - cx
				d2 := fx*fx + fy*fy
				if d2 >= r2 {
					continue
				}
				s := lut[int(d2*scale)]
				if hr > 0 {
					hx2 := float64(x) + 0.5 - hx
					h2 := hx2*hx2 + hy2*hy2
					if h2 <= hin2 {
						continue
					}
					if h2 < hout2 { // the record's edge, anti-aliased
						k := uint32((math.Sqrt(h2) - hr + 0.5) * 255)
						s = mul255(s&0xFF00FF, k) | mul255(s>>8&0xFF00FF, k)<<8
					}
				}
				sa := s >> 24
				if sa == 0 {
					continue
				}
				d := row[x]
				inv := 255 - sa
				row[x] = s + (mul255(d&0xFF00FF, inv) | mul255(d>>8&0xFF00FF, inv)<<8)
			}
		}
	}
	n := workers()
	if n < 2 || (x1-x0)*(y1-y0) < 60000 {
		rows(y0, y1)
		return
	}
	var wg sync.WaitGroup
	step := (y1 - y0 + n - 1) / n
	for ya := y0; ya < y1; ya += step {
		wg.Add(1)
		go func(ya, yb int) { defer wg.Done(); rows(ya, yb) }(ya, min(y1, ya+step))
	}
	wg.Wait()
}

// lineOver draws an anti-aliased straight line with flat ends (a GDI+ pen line) of width w
// and colour col (alpha included) over dst, inside clip.
func lineOver(dst []uint32, dw, dh int, x1, y1, x2, y2, w float64, col uint32, clip [4]int) {
	a0 := float64(col>>24) / 255
	dx, dy := x2-x1, y2-y1
	L := math.Hypot(dx, dy)
	if a0 <= 0 || L < 1e-3 || w <= 0 || len(dst) < dw*dh {
		return
	}
	ux, uy := dx/L, dy/L
	hw := w / 2
	pad := hw + 1
	x0, y0 := max(int(math.Min(x1, x2)-pad), clip[0], 0), max(int(math.Min(y1, y2)-pad), clip[1], 0)
	xe, ye := min(int(math.Max(x1, x2)+pad)+1, clip[2], dw), min(int(math.Max(y1, y2)+pad)+1, clip[3], dh)
	cr, cg, cb := float64(col>>16&255), float64(col>>8&255), float64(col&255)
	cov := func(d float64) float64 { return math.Max(0, math.Min(1, d+0.5)) } // d: inside by d pixels
	for y := y0; y < ye; y++ {
		py := float64(y) + 0.5 - y1
		row := dst[y*dw : y*dw+dw]
		for x := x0; x < xe; x++ {
			px := float64(x) + 0.5 - x1
			along := px*ux + py*uy
			across := math.Abs(px*uy - py*ux)
			c := cov(hw-across) * cov(math.Min(along, L-along))
			if w < 1 {
				c *= w
			}
			if c <= 0 {
				continue
			}
			a := a0 * c
			sa := uint32(a*255 + 0.5)
			if sa == 0 {
				continue
			}
			s := sa<<24 | uint32(cr*a+0.5)<<16 | uint32(cg*a+0.5)<<8 | uint32(cb*a+0.5)
			d := row[x]
			inv := 255 - sa
			row[x] = s + (mul255(d&0xFF00FF, inv) | mul255(d>>8&0xFF00FF, inv)<<8)
		}
	}
}

// overBlitScaled lays src (premultiplied BGRA, sw×sh, placed at dx,dy) over dst scaled by
// sx, sy about its own centre, with bilinear sampling and an overall alpha (0–255), only
// inside clip. Transparent parts cost almost nothing (most of a lyrics canvas is empty).
func overBlitScaled(dst []uint32, dw, dh int, src []uint32, sw, sh, dx, dy int, sx, sy float64, clip [4]int, alpha int) {
	if sx <= 0 || sy <= 0 || alpha <= 0 || len(src) < sw*sh || len(dst) < dw*dh || sw < 2 || sh < 2 {
		return
	}
	cx, cy := float64(dx)+float64(sw)/2, float64(dy)+float64(sh)/2
	hw, hh := float64(sw)/2*sx, float64(sh)/2*sy
	x0, y0 := max(int(cx-hw)-1, clip[0], 0), max(int(cy-hh)-1, clip[1], 0)
	x1, y1 := min(int(cx+hw)+2, clip[2], dw), min(int(cy+hh)+2, clip[3], dh)
	if x1 <= x0 || y1 <= y0 {
		return
	}
	ka := uint32(min(alpha, 255))
	isx, isy := 1/sx, 1/sy
	parallelArea((x1-x0)*(y1-y0), y1-y0, func(a, b int) {
		for y := y0 + a; y < y0+b; y++ {
			fy := (float64(y)+0.5-cy)*isy + float64(sh)/2 - 0.5
			if fy < -1 || fy > float64(sh) {
				continue
			}
			iy := int(math.Floor(fy))
			ty := uint32((fy - float64(iy)) * 256)
			drow := dst[y*dw : y*dw+dw]
			for x := x0; x < x1; x++ {
				fx := (float64(x)+0.5-cx)*isx + float64(sw)/2 - 0.5
				if fx < -1 || fx > float64(sw) {
					continue
				}
				ix := int(math.Floor(fx))
				var p00, p10, p01, p11 uint32
				if iy >= 0 && iy < sh {
					r := src[iy*sw : iy*sw+sw]
					if ix >= 0 && ix < sw {
						p00 = r[ix]
					}
					if ix+1 >= 0 && ix+1 < sw {
						p10 = r[ix+1]
					}
				}
				if iy+1 >= 0 && iy+1 < sh {
					r := src[(iy+1)*sw : (iy+1)*sw+sw]
					if ix >= 0 && ix < sw {
						p01 = r[ix]
					}
					if ix+1 >= 0 && ix+1 < sw {
						p11 = r[ix+1]
					}
				}
				if p00|p10|p01|p11 == 0 {
					continue
				}
				tx := uint32((fx - float64(ix)) * 256)
				s := lerpPx(lerpPx(p00, p10, tx), lerpPx(p01, p11, tx), ty)
				if ka < 255 {
					s = mul255(s&0xFF00FF, ka) | mul255(s>>8&0xFF00FF, ka)<<8
				}
				sa := s >> 24
				if sa == 0 {
					continue
				}
				if sa == 255 {
					drow[x] = s
					continue
				}
				d := drow[x]
				inv := 255 - sa
				drow[x] = s + (mul255(d&0xFF00FF, inv) | mul255(d>>8&0xFF00FF, inv)<<8)
			}
		}
	})
}

// lerpPx mixes two premultiplied pixels, t in 0–256.
func lerpPx(a, b, t uint32) uint32 {
	if t == 0 || a == b {
		return a
	}
	const m = 0xFF00FF
	it := 256 - t
	rb := ((a&m)*it + (b&m)*t) >> 8 & m
	ag := ((a>>8&m)*it + (b>>8&m)*t) >> 8 & m
	return rb | ag<<8
}

// contentBox: the smallest box holding every visible pixel of a canvas (x0, y0, x1, y1);
// all zero when it is empty. A lyrics canvas is mostly air: drawing, restoring and pushing
// only this box saves most of the work.
func contentBox(px []uint32, w, h int) [4]int {
	if len(px) < w*h || w <= 0 || h <= 0 {
		return [4]int{}
	}
	y0, y1 := -1, -1
	x0, x1 := w, 0
	for y := 0; y < h; y++ {
		row := px[y*w : y*w+w]
		a := -1
		for x, v := range row {
			if v != 0 {
				a = x
				break
			}
		}
		if a < 0 {
			continue
		}
		b := a
		for x := w - 1; x > a; x-- {
			if row[x] != 0 {
				b = x
				break
			}
		}
		if y0 < 0 {
			y0 = y
		}
		y1 = y
		x0, x1 = min(x0, a), max(x1, b)
	}
	if y0 < 0 {
		return [4]int{}
	}
	return [4]int{x0, y0, x1 + 1, y1 + 1}
}
