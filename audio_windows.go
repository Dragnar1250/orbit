package main

// System audio for the halo visualizer: WASAPI loopback capture of the default output device.

import (
	"math"
	"runtime"
	"sync/atomic"
	"time"
	"unsafe"
)

var (
	clsidMMDeviceEnumerator = GUID{0xBCDE0395, 0xE52F, 0x467C, [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = GUID{0xA95664D2, 0x9614, 0x4F35, [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioClient         = GUID{0x1CB9AD4C, 0xDBFA, 0x4C32, [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	iidIAudioCaptureClient  = GUID{0xC8ADBD64, 0xE71E, 0x48A0, [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}
)

type waveFormatEx struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	CbSize         uint16
}

type loopback struct {
	enum, dev, client, capture uintptr
	channels                   int
	bits                       int
	isFloat                    bool
	rate                       float64
}

func (l *loopback) close() {
	if l.client != 0 {
		comCall(l.client, 11) // Stop
	}
	comRelease(l.capture)
	comRelease(l.client)
	comRelease(l.dev)
	comRelease(l.enum)
	*l = loopback{}
}

func (l *loopback) open() bool {
	const clsctxAll = 0x17
	if failed(call(pCoCreateInstance, uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&l.enum)))) || l.enum == 0 {
		logf("audio: no device enumerator")
		return false
	}
	if failed(comCall(l.enum, 4, 0, 0, uintptr(unsafe.Pointer(&l.dev)))) || l.dev == 0 { // GetDefaultAudioEndpoint(eRender, eConsole)
		logf("audio: no default output device")
		l.close()
		return false
	}
	if failed(comCall(l.dev, 3, uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&l.client)))) || l.client == 0 { // Activate
		l.close()
		return false
	}
	var pwfx uintptr
	if failed(comCall(l.client, 8, uintptr(unsafe.Pointer(&pwfx)))) || pwfx == 0 { // GetMixFormat
		l.close()
		return false
	}
	defer call(pCoTaskMemFree, pwfx)
	wf := (*waveFormatEx)(unsafe.Pointer(pwfx))
	l.channels, l.bits, l.rate = int(wf.Channels), int(wf.BitsPerSample), float64(wf.SamplesPerSec)
	l.isFloat = wf.FormatTag == 3
	if wf.FormatTag == 0xFFFE && wf.CbSize >= 22 { // WAVEFORMATEXTENSIBLE: SubFormat GUID at offset 24
		sub := *(*uint32)(unsafe.Pointer(pwfx + 24))
		l.isFloat = sub == 3
	}
	const loopbackFlag = 0x00020000
	if failed(comCall(l.client, 3, 0, loopbackFlag, 2000000, 0, pwfx, 0)) { // Initialize (shared, 200 ms buffer)
		logf("audio: Initialize failed")
		l.close()
		return false
	}
	if failed(comCall(l.client, 14, uintptr(unsafe.Pointer(&iidIAudioCaptureClient)), uintptr(unsafe.Pointer(&l.capture)))) || l.capture == 0 { // GetService
		l.close()
		return false
	}
	if failed(comCall(l.client, 10)) { // Start
		l.close()
		return false
	}
	logf("audio: loopback %d Hz, %d ch, %d bit, float=%v", int(l.rate), l.channels, l.bits, l.isFloat)
	return true
}

// read drains pending packets; returns false if the device went away.
func (l *loopback) read(mono []float32) ([]float32, bool) {
	mono = mono[:0]
	for i := 0; i < 64; i++ {
		var n uint32
		if failed(comCall(l.capture, 5, uintptr(unsafe.Pointer(&n)))) { // GetNextPacketSize
			return mono, false
		}
		if n == 0 {
			break
		}
		var data uintptr
		var frames, flags uint32
		if failed(comCall(l.capture, 3, uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&frames)), uintptr(unsafe.Pointer(&flags)), 0, 0)) { // GetBuffer
			return mono, false
		}
		ch := l.channels
		if ch < 1 {
			ch = 1
		}
		silent := flags&2 != 0
		bps := l.bits / 8
		for f := 0; f < int(frames); f++ {
			var sum float32
			if !silent && data != 0 {
				for c := 0; c < ch; c++ {
					p := data + uintptr((f*ch+c)*bps)
					switch {
					case l.isFloat && bps == 4:
						sum += *(*float32)(unsafe.Pointer(p))
					case bps == 2:
						sum += float32(*(*int16)(unsafe.Pointer(p))) / 32768
					case bps == 4:
						sum += float32(*(*int32)(unsafe.Pointer(p))) / 2147483648
					case bps == 3:
						b := (*[3]byte)(unsafe.Pointer(p))
						v := int32(b[0])<<8 | int32(b[1])<<16 | int32(b[2])<<24
						sum += float32(v) / 2147483648
					}
				}
			}
			mono = append(mono, sum/float32(ch))
		}
		comCall(l.capture, 4, uintptr(frames)) // ReleaseBuffer
	}
	return mono, true
}

// bandsWanted: 1 while something shows the music's levels (set by the UI thread every frame).
var bandsWanted int32 = 1

func audioLoop() {
	runtime.LockOSThread()
	defer func() {
		if r := recover(); r != nil {
			logf("audio panic: %v (visualizer disabled)", r)
		}
	}()
	call(pCoInitializeEx, 0, 0)
	var l loopback
	buf := make([]float32, 0, 8192)
	lastData := time.Now()
	unwantedSince := time.Time{}
	for {
		// nothing on screen reacts to the music: stop listening (closing the capture after a
		// while also lets Windows' audio engine idle)
		if atomic.LoadInt32(&bandsWanted) == 0 {
			if unwantedSince.IsZero() {
				unwantedSince = time.Now()
			}
			if l.client != 0 && time.Since(unwantedSince) > 10*time.Second {
				l.close()
			}
			time.Sleep(250 * time.Millisecond)
			continue
		}
		unwantedSince = time.Time{}
		if l.client == 0 {
			if !l.open() {
				time.Sleep(5 * time.Second)
				continue
			}
			lastData = time.Now()
		}
		var ok bool
		buf, ok = l.read(buf)
		if !ok {
			logf("audio: device changed, reopening")
			l.close()
			time.Sleep(1 * time.Second)
			continue
		}
		if len(buf) > 0 {
			lastData = time.Now()
			spectrum.Feed(buf, l.rate)
		} else if time.Since(lastData) > 15*time.Second {
			// no packets for a while — maybe the default output device changed; reopen cheaply
			l.close()
			continue
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// rms helper for the probe
func rms(b []float32) float64 {
	s := 0.0
	for _, v := range b {
		s += float64(v * v)
	}
	if len(b) == 0 {
		return 0
	}
	return math.Sqrt(s / float64(len(b)))
}
