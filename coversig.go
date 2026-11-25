//go:build windows || darwin

package main

// sigFromHandle: a picture's middle square at 8×8 — averaged from a 64×64 high-quality copy, so a
// detailed cover (clouds, grain) gives the same signature at 300 px and at 2000 px. (Drawn straight
// to 8×8 it was a handful of sampled pixels: a 2000 px copy never matched Spotify's 300 px one on
// Windows, so the sharp cover was thrown away and the full-screen background stayed blurry.)
func sigFromHandle(img uintptr) *coverSig {
	if img == 0 {
		return nil
	}
	const n = 64
	b := newBitmap(n, n)
	if b == nil {
		return nil
	}
	defer b.Free()
	gClear(b.g, 0xFF000000)
	setInterp(b.g, interpHighQualityBicubic)
	drawCoverSquare(b.g, img, 0, 0, n)
	gFlush(b.g)
	var sig coverSig
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			c := pixel(b.bmp, x, y)
			i := (y*8/n)*8 + x*8/n
			sig[i][0] += float64(c >> 16 & 255)
			sig[i][1] += float64(c >> 8 & 255)
			sig[i][2] += float64(c & 255)
		}
	}
	for i := range sig {
		for k := 0; k < 3; k++ {
			sig[i][k] /= (n / 8) * (n / 8)
		}
	}
	return &sig
}
