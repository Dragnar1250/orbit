package main

import (
	"math"
	"math/cmplx"
	"sync"
	"time"
)

const fftN = 1024
const nBands = 64

// Spectrum turns raw PCM from the loopback capture into 64 smoothed log-spaced bands (0..1).
type Spectrum struct {
	mu       sync.Mutex
	ring     [fftN]float32
	pos      int
	rate     float64
	lastFeed time.Time
	peak     float64
	bands    [nBands]float32
	win      [fftN]float64
	edges    [nBands + 1]int
	lastRate float64
}

var spectrum = &Spectrum{rate: 48000, peak: 1e-3}

func (s *Spectrum) setup(rate float64) {
	for i := range s.win {
		s.win[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(fftN-1))
	}
	lo, hi := 40.0, math.Min(16000, rate/2-1)
	prev := 1
	for b := 0; b <= nBands; b++ {
		f := lo * math.Pow(hi/lo, float64(b)/nBands)
		bin := int(math.Round(f * fftN / rate))
		if bin <= prev && b > 0 {
			bin = prev + 1
		}
		if bin > fftN/2 {
			bin = fftN / 2
		}
		s.edges[b] = bin
		prev = bin
	}
	s.lastRate = rate
}

// Feed adds mono samples (-1..1).
func (s *Spectrum) Feed(samples []float32, rate float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rate = rate
	for _, v := range samples {
		s.ring[s.pos] = v
		s.pos = (s.pos + 1) % fftN
	}
	s.lastFeed = time.Now()
}

// Compute runs one FFT; call ~30x per second from the UI thread.
func (s *Spectrum) Compute() ([nBands]float32, float32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	active := time.Since(s.lastFeed) < 300*time.Millisecond
	if !active {
		for i := range s.bands {
			s.bands[i] *= 0.85
		}
		return s.bands, bassOf(s.bands), false
	}
	if s.lastRate != s.rate {
		s.setup(s.rate)
	}
	var buf [fftN]complex128
	for i := 0; i < fftN; i++ {
		buf[i] = complex(float64(s.ring[(s.pos+i)%fftN])*s.win[i], 0)
	}
	fft(buf[:])
	var raw [nBands]float64
	frame := 0.0
	for b := 0; b < nBands; b++ {
		m := 0.0
		for k := s.edges[b]; k < s.edges[b+1] && k <= fftN/2; k++ {
			if a := cmplx.Abs(buf[k]); a > m {
				m = a
			}
		}
		// lift higher frequencies a bit so the ring looks even
		m *= 1 + float64(b)/nBands*1.5
		raw[b] = m
		if m > frame {
			frame = m
		}
	}
	// automatic gain: follow the loudest band slowly
	if frame > s.peak {
		s.peak = s.peak*0.6 + frame*0.4
	} else {
		s.peak = math.Max(1e-3, s.peak*0.995)
	}
	silent := frame < 1e-4
	for b := 0; b < nBands; b++ {
		v := 0.0
		if !silent {
			v = math.Sqrt(raw[b] / s.peak)
		}
		if v > 1 {
			v = 1
		}
		if float32(v) > s.bands[b] {
			s.bands[b] = float32(v)
		} else {
			s.bands[b] = s.bands[b]*0.78 + float32(v)*0.22
		}
	}
	return s.bands, bassOf(s.bands), !silent
}

func bassOf(b [nBands]float32) float32 {
	var s float32
	for i := 0; i < 6; i++ {
		s += b[i]
	}
	return s / 6
}

// fft is an in-place iterative radix-2 FFT (len must be a power of two).
func fft(a []complex128) {
	n := len(a)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			a[i], a[j] = a[j], a[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		ang := -2 * math.Pi / float64(size)
		wl := complex(math.Cos(ang), math.Sin(ang))
		for i := 0; i < n; i += size {
			w := complex(1, 0)
			for k := 0; k < size/2; k++ {
				u := a[i+k]
				v := a[i+k+size/2] * w
				a[i+k] = u + v
				a[i+k+size/2] = u - v
				w *= wl
			}
		}
	}
}
