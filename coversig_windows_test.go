package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"math/rand"
	"testing"
)

// a detailed picture (fine cloud-like noise): the case where a few sampled pixels disagree
func detailedCover(n int) image.Image {
	r := rand.New(rand.NewSource(7))
	im := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			v := uint8(80 + r.Intn(120) + (x*60)/n)
			im.Set(x, y, color.RGBA{v, v, uint8(int(v) * 9 / 10), 255})
		}
	}
	return im
}

func enc(im image.Image) []byte {
	var b bytes.Buffer
	jpeg.Encode(&b, im, &jpeg.Options{Quality: 92})
	return b.Bytes()
}

func TestSigFromHandleMatchesBig(t *testing.T) {
	if !gdipInit() {
		t.Skip("no GDI+")
	}
	big := detailedCover(2000)
	small := image.NewRGBA(image.Rect(0, 0, 250, 250)) // (a box-averaged 2000 → 250 copy)
	for y := 0; y < 250; y++ {
		for x := 0; x < 250; x++ {
			var r, g, b uint32
			for j := 0; j < 8; j++ {
				for i := 0; i < 8; i++ {
					cr, cg, cb, _ := big.At(x*8+i, y*8+j).RGBA()
					r, g, b = r+cr>>8, g+cg>>8, b+cb>>8
				}
			}
			small.Set(x, y, color.RGBA{uint8(r / 64), uint8(g / 64), uint8(b / 64), 255})
		}
	}
	bigB, smallB := enc(big), enc(small)
	hs, hb := imageFromBytes(smallB), imageFromBytes(bigB)
	ref := sigFromHandle(hs)
	goBig := sigFromImage(mustDecode(bigB))
	d1, d2 := sigDiff(ref, goBig), imageDiff(hs, hb)
	t.Logf("player 300 px vs 2000 px (Go): %.3f; GDI+ both: %.3f", d1, d2)
	if d1 > 0.08 || d2 > 0.08 {
		t.Fatal("the same cover must match")
	}
	// the old way: drawn straight to 8×8
	old := func(a, b uintptr) float64 {
		A, B := newBitmap(8, 8), newBitmap(8, 8)
		defer A.Free()
		defer B.Free()
		drawCoverSquare(A.g, a, 0, 0, 8)
		drawCoverSquare(B.g, b, 0, 0, 8)
		var d float64
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				p, q := pixel(A.bmp, x, y), pixel(B.bmp, x, y)
				for s := 0; s < 24; s += 8 {
					d += math.Abs(float64(p>>s&255) - float64(q>>s&255))
				}
			}
		}
		return d / (64 * 3 * 255)
	}
	t.Logf("old way: %.3f", old(hs, hb))
}
