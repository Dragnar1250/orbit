package main

// Perspective drawing in plain Go: a flat image (premultiplied BGRA) is drawn into any
// four-cornered shape — a card turned toward the middle, a panel tilted back — with smooth
// (bilinear) sampling and soft edges. GDI+ can only shear; slicing the image into strips left
// jagged seams.

import "math"

// homography maps (u,v) in the unit square to the quad q (TL, TR, BR, BL).
type homography [9]float64

func squareToQuad(q [4][2]float64) homography {
	x0, y0, x1, y1, x2, y2, x3, y3 := q[0][0], q[0][1], q[1][0], q[1][1], q[2][0], q[2][1], q[3][0], q[3][1]
	dx1, dy1, dx2, dy2 := x1-x2, y1-y2, x3-x2, y3-y2
	sx, sy := x0-x1+x2-x3, y0-y1+y2-y3
	var g, h float64
	if det := dx1*dy2 - dx2*dy1; det != 0 {
		g = (sx*dy2 - dx2*sy) / det
		h = (dx1*sy - sx*dy1) / det
	}
	return homography{x1 - x0 + g*x1, x3 - x0 + h*x3, x0, y1 - y0 + g*y1, y3 - y0 + h*y3, y0, g, h, 1}
}

func (m homography) inverse() (homography, bool) {
	a, b, c, d, e, f, g, h, i := m[0], m[1], m[2], m[3], m[4], m[5], m[6], m[7], m[8]
	A, B, C := e*i-f*h, -(d*i - f*g), d*h-e*g
	det := a*A + b*B + c*C
	if math.Abs(det) < 1e-12 {
		return homography{}, false
	}
	k := 1 / det
	return homography{A * k, -(b*i - c*h) * k, (b*f - c*e) * k, B * k, (a*i - c*g) * k, -(a*f - c*d) * k, C * k, -(a*h - b*g) * k, (a*e - b*d) * k}, true
}

func (m homography) apply(x, y float64) (float64, float64) {
	w := m[6]*x + m[7]*y + m[8]
	if w == 0 {
		w = 1e-9
	}
	return (m[0]*x + m[1]*y + m[2]) / w, (m[3]*x + m[4]*y + m[5]) / w
}

// warpOnto draws src (sw×sh) into the quad q of dst (dw×dh), blended over what is there,
// with an extra opacity (0..1).
func warpOnto(dst []uint32, dw, dh int, src []uint32, sw, sh int, q [4][2]float64, opacity float64) {
	if sw < 2 || sh < 2 || dw <= 0 || dh <= 0 || len(src) < sw*sh || len(dst) < dw*dh || opacity <= 0 {
		return
	}
	inv, ok := squareToQuad(q).inverse()
	if !ok {
		return
	}
	minX, minY, maxX, maxY := q[0][0], q[0][1], q[0][0], q[0][1]
	for _, p := range q {
		minX, maxX = math.Min(minX, p[0]), math.Max(maxX, p[0])
		minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
	}
	x0, x1 := max(0, int(math.Floor(minX))), min(dw, int(math.Ceil(maxX))+1)
	y0, y1 := max(0, int(math.Floor(minY))), min(dh, int(math.Ceil(maxY))+1)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	op := uint32(math.Round(math.Min(1, opacity) * 256))
	fw, fh := float64(sw), float64(sh)
	parallelArea((x1-x0)*(y1-y0), y1-y0, func(a, b int) {
		for y := y0 + a; y < y0+b; y++ {
			row := dst[y*dw : y*dw+dw]
			for x := x0; x < x1; x++ {
				u, v := inv.apply(float64(x)+0.5, float64(y)+0.5)
				// sample position in source pixels (centres at +0.5)
				sx, sy := u*fw-0.5, v*fh-0.5
				if sx < -1 || sy < -1 || sx > fw || sy > fh {
					continue
				}
				p := sampleEdge(src, sw, sh, sx, sy)
				if op < 256 {
					p = scalePix(p, op)
				}
				if p>>24 == 0 {
					continue
				}
				row[x] = over(p, row[x])
			}
		}
	})
}

// sampleEdge: bilinear, with transparent outside — so the warped edges come out soft, not jagged.
func sampleEdge(src []uint32, sw, sh int, x, y float64) uint32 {
	ix, iy := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := uint32((x-float64(ix))*256), uint32((y-float64(iy))*256)
	at := func(px, py int) uint32 {
		if px < 0 || py < 0 || px >= sw || py >= sh {
			return 0
		}
		return src[py*sw+px]
	}
	return lerp4(lerp4(at(ix, iy), at(ix+1, iy), fx), lerp4(at(ix, iy+1), at(ix+1, iy+1), fx), fy)
}

// scalePix multiplies a premultiplied pixel by k/256.
func scalePix(p, k uint32) uint32 {
	const m = 0x00FF00FF
	return ((p&m)*k>>8)&m | ((p>>8&m)*k>>8&m)<<8
}

// over: premultiplied source over destination.
func over(s, d uint32) uint32 {
	a := s >> 24
	if a == 255 {
		return s
	}
	k := 255 - a
	const m = 0x00FF00FF
	// exact x·k/255 per channel (two at a time): an opaque background stays exactly opaque
	t1 := (d&m)*k + 0x00800080
	t2 := (d>>8&m)*k + 0x00800080
	rb := (t1 + (t1 >> 8 & m)) >> 8 & m
	ag := (t2 + (t2 >> 8 & m)) >> 8 & m
	return s + (rb | ag<<8)
}

// tiltQuad: the corners of a w×h rectangle at (x, y) turned around its vertical axis by angle
// (radians; positive = the right edge goes back), seen from distance (in pixels).
func tiltQuad(x, y, w, h, angle, distance float64) [4][2]float64 {
	cx, cy := x+w/2, y+h/2
	pt := func(px, py float64) [2]float64 {
		dx := px - cx
		z := dx * math.Sin(angle) // depth: >0 = further away
		X := dx * math.Cos(angle) * distance / (distance + z)
		Y := (py - cy) * distance / (distance + z)
		return [2]float64{cx + X, cy + Y}
	}
	return [4][2]float64{pt(x, y), pt(x+w, y), pt(x+w, y+h), pt(x, y+h)}
}
