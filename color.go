package main

import "math"

func rgbToHsl(r, g, b float64) (h, s, l float64) {
	r, g, b = r/255, g/255, b/255
	mx, mn := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	l = (mx + mn) / 2
	if mx == mn {
		return 0, 0, l
	}
	d := mx - mn
	if l > 0.5 {
		s = d / (2 - mx - mn)
	} else {
		s = d / (mx + mn)
	}
	switch mx {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	return h / 6, s, l
}

func hslToRgb(h, s, l float64) (float64, float64, float64) {
	if s == 0 {
		return l * 255, l * 255, l * 255
	}
	q := l + s - l*s
	if l < 0.5 {
		q = l * (1 + s)
	}
	p := 2*l - q
	f := func(t float64) float64 {
		if t < 0 {
			t++
		}
		if t > 1 {
			t--
		}
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 0.5:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		}
		return p
	}
	return f(h+1.0/3) * 255, f(h) * 255, f(h-1.0/3) * 255
}

// accentFromPixels picks a vivid, readable accent colour from ARGB samples of an album cover.
func accentFromPixels(px []uint32) uint32 {
	type bucket struct{ w, r, g, b float64 }
	var bk [24]bucket
	grayW, gr, gg, gb := 0.0, 0.0, 0.0, 0.0
	for _, c := range px {
		r, g, b := float64(c>>16&255), float64(c>>8&255), float64(c&255)
		h, s, l := rgbToHsl(r, g, b)
		if s < 0.18 || l < 0.08 || l > 0.95 {
			grayW += l
			gr, gg, gb = gr+r*l, gg+g*l, gb+b*l
			continue
		}
		w := s * (1 - math.Abs(l-0.5)*1.4)
		k := int(h*24) % 24
		bk[k].w += w
		bk[k].r += r * w
		bk[k].g += g * w
		bk[k].b += b * w
	}
	best := -1
	for i := range bk {
		if best < 0 || bk[i].w > bk[best].w {
			best = i
		}
	}
	var r, g, b float64
	if best < 0 || bk[best].w < float64(len(px))*0.04 {
		if grayW == 0 {
			return 0xFFF2F2F2
		}
		r, g, b = gr/grayW, gg/grayW, gb/grayW
	} else {
		r, g, b = bk[best].r/bk[best].w, bk[best].g/bk[best].w, bk[best].b/bk[best].w
	}
	h, s, l := rgbToHsl(r, g, b)
	l = math.Max(l, 0.62)
	if s > 0.05 {
		s = math.Max(s, 0.55)
	}
	r, g, b = hslToRgb(h, s, l)
	return 0xFF000000 | uint32(clamp255(r))<<16 | uint32(clamp255(g))<<8 | uint32(clamp255(b))
}

func clamp255(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v + 0.5)
}

// armAngle returns the tonearm angle (radians, from the pivot) so the stylus sits at radius rt
// of the record. Of the two possible solutions it picks the one where the stylus is higher up
// (the arm reaches in over the top of the record, like a real deck).
func armAngle(px, py, cx, cy, armLen, rt float64) float64 {
	dx, dy := cx-px, cy-py
	d := math.Hypot(dx, dy)
	c := (armLen*armLen + d*d - rt*rt) / (2 * armLen * d)
	c = math.Max(-1, math.Min(1, c))
	base, off := math.Atan2(dy, dx), math.Acos(c)
	a1, a2 := base-off, base+off
	if py+armLen*math.Sin(a2) < py+armLen*math.Sin(a1) {
		return a2
	}
	return a1
}

// paletteFromPixels picks up to n distinct, lively colours from a downscaled cover
// (most saturated first; greys only when nothing else is there).
func paletteFromPixels(px []uint32, n int) []uint32 {
	type cand struct {
		c          uint32
		score      float64
		hue        float64
		grey       bool
		brightness float64
	}
	var cs []cand
	for _, c := range px {
		r, g, b := float64(c>>16&255)/255, float64(c>>8&255)/255, float64(c&255)/255
		mx, mn := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
		if mx < 0.12 || c>>24 < 128 {
			continue
		}
		sat := 0.0
		if mx > 0 {
			sat = (mx - mn) / mx
		}
		hue := 0.0
		if d := mx - mn; d > 1e-6 {
			switch mx {
			case r:
				hue = math.Mod((g-b)/d, 6)
			case g:
				hue = (b-r)/d + 2
			default:
				hue = (r-g)/d + 4
			}
			hue = math.Mod(hue/6+1, 1)
		}
		cs = append(cs, cand{c | 0xFF000000, sat*0.8 + mx*0.4, hue, sat < 0.15, mx})
	}
	for i := 1; i < len(cs); i++ { // most lively first (insertion sort: few pixels)
		for j := i; j > 0 && cs[j].score > cs[j-1].score; j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
	var out []uint32
	var kept []cand
	for _, c := range cs {
		if len(out) >= n {
			break
		}
		near := false
		for _, k := range kept {
			if k.grey != c.grey {
				continue
			}
			d := math.Abs(k.hue - c.hue)
			if (!c.grey && math.Min(d, 1-d) < 0.06) || (c.grey && math.Abs(k.brightness-c.brightness) < 0.15) {
				near = true
				break
			}
		}
		if !near {
			out = append(out, c.c)
			kept = append(kept, c)
		}
	}
	if len(out) == 0 {
		out = []uint32{0xFFF2F2F2}
	}
	return out
}

// softTint: a colour from the album that reads well on dark glass — the hue kept, the
// saturation calmed, the lightness lifted (like Apple's tinted labels). Greys stay neutral.
func softTint(c uint32) uint32 {
	r, g, b := float64(c>>16&255), float64(c>>8&255), float64(c&255)
	h, s, l := rgbToHsl(r, g, b)
	if s < 0.08 {
		return 0xFFE6E6EB
	}
	s = math.Min(s, 0.72)
	s = math.Max(s, 0.35)
	l = math.Max(0.68, math.Min(0.8, l))
	R, G, B := hslToRgb(h, s, l)
	return 0xFF000000 | uint32(clamp255(R))<<16 | uint32(clamp255(G))<<8 | uint32(clamp255(B))
}
