package main

import (
	"math"
	"math/rand"
	"testing"
)

func TestScatterPlaceNoOverlap(t *testing.T) {
	for seed := int64(0); seed < 300; seed++ {
		r := rand.New(rand.NewSource(seed))
		n := 1 + r.Intn(16)
		W, H := 200+r.Float64()*1200, 80+r.Float64()*900
		sizes := make([][2]float64, n)
		for i := range sizes {
			sizes[i] = [2]float64{20 + r.Float64()*260, 30 + r.Float64()*40}
		}
		sp := scatterPlace(sizes, seed, W, H)
		if len(sp) != n {
			t.Fatalf("seed %d: %d spots for %d words", seed, len(sp), n)
		}
		type b struct{ l, t, r, b float64 }
		bs := make([]b, n)
		for i, s := range sp {
			th := math.Abs(s.Rot) * math.Pi / 180
			w, h := sizes[i][0]*s.Scale, sizes[i][1]*s.Scale*0.82
			hw, hh := (w*math.Cos(th)+h*math.Sin(th))/2, (w*math.Sin(th)+h*math.Cos(th))/2
			bs[i] = b{s.X - hw, s.Y - hh, s.X + hw, s.Y + hh}
			if bs[i].l < -0.5 || bs[i].t < -0.5 || bs[i].r > W+0.5 || bs[i].b > H+0.5 {
				t.Fatalf("seed %d word %d outside the box: %+v in %vx%v", seed, i, bs[i], W, H)
			}
			for j := 0; j < i; j++ {
				if bs[i].l < bs[j].r && bs[j].l < bs[i].r && bs[i].t < bs[j].b && bs[j].t < bs[i].b {
					t.Fatalf("seed %d: words %d and %d overlap", seed, i, j)
				}
			}
		}
		again := scatterPlace(sizes, seed, W, H)
		for i := range sp {
			if sp[i] != again[i] {
				t.Fatalf("seed %d: not the same twice", seed)
			}
		}
	}
}

func TestScatterAvoidsOldLine(t *testing.T) {
	sizes := [][2]float64{{120, 50}, {80, 50}, {160, 50}, {60, 50}}
	old := scatterPlace(sizes, 7, 900, 700)
	avoid := scatterBoxes(sizes, old)
	clear := 0
	for seed := int64(0); seed < 50; seed++ {
		sp := scatterPlaceAvoid(sizes, seed, 900, 700, avoid)
		hit := false
		for _, b := range scatterBoxes(sizes, sp) {
			for _, a := range avoid {
				if math.Abs(b[0]-a[0]) < b[2]+a[2] && math.Abs(b[1]-a[1]) < b[3]+a[3] {
					hit = true
				}
			}
		}
		if !hit {
			clear++
		}
	}
	if clear < 48 { // with this much room, it should (almost) always manage
		t.Fatalf("only %d of 50 lines kept clear of the old one", clear)
	}
}
