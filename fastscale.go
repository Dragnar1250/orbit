package main

// Fast bilinear upscaling of 32-bit pixels in plain Go, spread over the CPU cores. GDI+ took tens
// of milliseconds to stretch a small background to full screen every frame (the main reason full
// screen ran at a few frames per second); this does it in a few.

import (
	"runtime"
	"sync"
)

// scaleBilinear stretches src (sw×sh) into dst (dw×dh). Both are tightly packed rows of BGRA.
func scaleBilinear(dst []uint32, dw, dh int, src []uint32, sw, sh int) {
	if dw <= 0 || dh <= 0 || sw <= 0 || sh <= 0 || len(dst) < dw*dh || len(src) < sw*sh {
		return
	}
	// 16.16 fixed point, sampling pixel centres
	xs := make([]int32, dw) // source x (16.16) for each destination column
	for x := 0; x < dw; x++ {
		v := ((int64(x)*2+1)*int64(sw)<<16)/(int64(dw)*2) - 1<<15
		if v < 0 {
			v = 0
		}
		if max := int64(sw-1) << 16; v > max {
			v = max
		}
		xs[x] = int32(v)
	}
	rowJob := func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			v := ((int64(y)*2+1)*int64(sh)<<16)/(int64(dh)*2) - 1<<15
			if v < 0 {
				v = 0
			}
			if max := int64(sh-1) << 16; v > max {
				v = max
			}
			sy := int(v >> 16)
			fy := uint32(v>>8) & 0xFF
			sy2 := sy + 1
			if sy2 >= sh {
				sy2 = sh - 1
			}
			r0 := src[sy*sw : sy*sw+sw]
			r1 := src[sy2*sw : sy2*sw+sw]
			out := dst[y*dw : y*dw+dw]
			for x, xv := range xs {
				sx := int(xv >> 16)
				fx := uint32(xv>>8) & 0xFF
				sx2 := sx + 1
				if sx2 >= sw {
					sx2 = sw - 1
				}
				out[x] = lerp4(lerp4(r0[sx], r0[sx2], fx), lerp4(r1[sx], r1[sx2], fx), fy)
			}
		}
	}
	parallelRows(dh, rowJob)
}

// lerp4 mixes two pixels channel by channel (t = 0..255), two channels at a time.
func lerp4(a, b, t uint32) uint32 {
	if a == b || t == 0 {
		return a
	}
	const m = 0x00FF00FF
	it := 256 - t
	t++
	rb := ((a&m)*it + (b&m)*t) >> 8 & m
	ag := ((a>>8&m)*it + (b>>8&m)*t) >> 8 & m
	return rb | ag<<8
}

// workers: how many cores Orbit's pixel work may use at once — half the PC (at least 2,
// at most 6), so the desktop, the mouse and other apps always have room.
func workers() int {
	return scaleWorkers
}

var scaleWorkers = func() int {
	n := runtime.NumCPU() / 2
	if n < 2 {
		n = min(2, runtime.NumCPU())
	}
	if n > 6 {
		n = 6 // leave the rest of the PC alone
	}
	if n < 1 {
		n = 1
	}
	return n
}()

// parallelRows runs job over [0,h) in a few bands at once.
// parallelArea: parallelRows for work over an area of about area pixels — below ~130 000 pixels
// it runs right here: waking up worker threads for a small blit cost more than the blit
// (on the Mac a quarter of all of Orbit's time was spent just waking threads).
func parallelArea(area, h int, job func(y0, y1 int)) {
	if area < 1<<17 {
		job(0, h)
		return
	}
	parallelRows(h, job)
}

func parallelRows(h int, job func(y0, y1 int)) {
	n := scaleWorkers
	if h < 64 || n == 1 {
		job(0, h)
		return
	}
	var wg sync.WaitGroup
	step := (h + n - 1) / n
	for y := 0; y < h; y += step {
		y1 := y + step
		if y1 > h {
			y1 = h
		}
		wg.Add(1)
		go func(a, b int) {
			defer wg.Done()
			job(a, b)
		}(y, y1)
	}
	wg.Wait()
}

// scaleSmooth: a softer, band-free stretch for big enlargements (the small backgrounds):
// first ×2 into tmp, then to the full size with an ordered dither — dark gradients keep no steps.
// tmp is reused between calls (it grows as needed).
func scaleSmooth(dst []uint32, dw, dh int, src []uint32, sw, sh int, tmp *[]uint32) {
	if dw <= 0 || dh <= 0 || sw <= 0 || sh <= 0 || len(dst) < dw*dh || len(src) < sw*sh {
		return
	}
	if dw >= sw*3 && dh >= sh*3 {
		mw, mh := sw*2, sh*2
		if cap(*tmp) < mw*mh {
			*tmp = make([]uint32, mw*mh)
		}
		mid := (*tmp)[:mw*mh]
		scaleBilinear(mid, mw, mh, src, sw, sh)
		src, sw, sh = mid, mw, mh
	}
	scaleDither(dst, dw, dh, src, sw, sh)
}

var bayer4 = [16]uint32{0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5}

// scaleDither is scaleBilinear with the last rounding step spread by a 4×4 Bayer pattern.
func scaleDither(dst []uint32, dw, dh int, src []uint32, sw, sh int) {
	xs := make([]int32, dw)
	for x := 0; x < dw; x++ {
		v := ((int64(x)*2+1)*int64(sw)<<16)/(int64(dw)*2) - 1<<15
		if v < 0 {
			v = 0
		}
		if m := int64(sw-1) << 16; v > m {
			v = m
		}
		xs[x] = int32(v)
	}
	parallelRows(dh, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			v := ((int64(y)*2+1)*int64(sh)<<16)/(int64(dh)*2) - 1<<15
			if v < 0 {
				v = 0
			}
			if mx := int64(sh-1) << 16; v > mx {
				v = mx
			}
			sy := int(v >> 16)
			fy := uint32(v>>8) & 0xFF
			sy2 := sy + 1
			if sy2 >= sh {
				sy2 = sh - 1
			}
			r0, r1 := src[sy*sw:sy*sw+sw], src[sy2*sw:sy2*sw+sw]
			out := dst[y*dw : y*dw+dw]
			brow := bayer4[(y&3)*4 : (y&3)*4+4]
			ify := 256 - fy
			for x, xv := range xs {
				sx := int(xv >> 16)
				fx := uint32(xv>>8) & 0xFF
				sx2 := sx + 1
				if sx2 >= sw {
					sx2 = sw - 1
				}
				// full precision (16 fraction bits) per channel, then a dithered rounding
				p00, p01, p10, p11 := r0[sx], r0[sx2], r1[sx], r1[sx2]
				if p00 == p01 && p10 == p11 && p00 == p10 { // flat here: nothing to blend
					out[x] = p00
					continue
				}
				ifx := 256 - fx
				d := (brow[x&3]<<4 + 8) << 8 // 0.03 .. 0.97 of a level
				w00, w01, w10, w11 := ifx*ify, fx*ify, ifx*fy, fx*fy
				ch := func(sh uint) uint32 {
					v := ((p00>>sh&0xFF)*w00 + (p01>>sh&0xFF)*w01 + (p10>>sh&0xFF)*w10 + (p11>>sh&0xFF)*w11 + d) >> 16
					if v > 255 {
						v = 255
					}
					return v
				}
				var p uint32
				if p00&p01&p10&p11 >= 0xFF000000 { // opaque (the backgrounds): skip alpha
					p = 0xFF000000 | ch(16)<<16 | ch(8)<<8 | ch(0)
				} else {
					p = ch(24)<<24 | ch(16)<<16 | ch(8)<<8 | ch(0)
				}
				// premultiplied: a colour channel may never pass the alpha
				if al := p >> 24; p>>16&0xFF > al || p>>8&0xFF > al || p&0xFF > al {
					p = lerp4(lerp4(p00, p01, fx), lerp4(p10, p11, fx), fy)
				}
				out[x] = p
			}
		}
	})
}
