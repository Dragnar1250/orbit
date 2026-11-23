package main

import (
	"math"
	"testing"
	"time"
)

func BenchmarkSpectrum(b *testing.B) {
	s := &Spectrum{rate: 48000, peak: 1e-3}
	buf := make([]float32, 480)
	for i := range buf {
		buf[i] = float32(math.Sin(float64(i) * 0.05))
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s.Feed(buf, 48000)
		s.lastFeed = time.Now()
		s.Compute()
	}
}
