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
