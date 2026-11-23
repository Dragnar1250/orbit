//go:build darwin

package main

/*
#cgo LDFLAGS: -framework CoreAudio -framework AudioToolbox -framework Foundation
#include "audio_darwin.h"
*/
import "C"

import (
	"math"
	"sync/atomic"
	"time"
)

// bandsWanted: 1 while something on screen moves with the music (set every frame).
var bandsWanted int32

// audioLoop listens to the system audio only while it is wanted (the visualizer, a motion effect,
// the glow …) — no permission prompt and no CPU for people who don't use those.
func audioLoop() {
	buf := make([]float32, 8192)
	on, failedAt := false, time.Time{}
	lat, latAt := -1.0, time.Time{}
	for {
		want := atomic.LoadInt32(&bandsWanted) == 1
		switch {
		case want && !on && time.Since(failedAt) > time.Minute:
			if st := C.os_audio_start(); st == 0 {
				on = true
				logf("audio: listening to the system audio (%.0f Hz)", float64(C.os_audio_rate()))
			} else {
				failedAt = time.Now()
				if st == 1 {
					logf("audio: this macOS can't share its audio (needs macOS 14.2 or newer) — the visualizer moves without it")
				} else {
					logf("audio: couldn't listen (%d) — allow Orbit in System Settings → Privacy & Security → Screen & System Audio Recording", int(st))
				}
			}
		case !want && on:
			C.os_audio_stop()
			on = false
		}
		if on {
			if time.Since(latAt) > 3*time.Second { // headphones come and go
				latAt = time.Now()
				d := float64(C.os_audio_out_latency())
				if math.Abs(d-lat) > 0.005 {
					lat = d
					logf("audio: the output plays %.0f ms after the tap hears it — visuals wait for it", d*1000)
				}
				C.os_audio_set_delay(C.double(lat))
			}
			n := int(C.os_audio_read((*C.float)(&buf[0]), C.int(len(buf))))
			if n > 0 {
				spectrum.Feed(buf[:n], float64(C.os_audio_rate()))
			}
			time.Sleep(8 * time.Millisecond) // fresh samples for every frame at 60 fps and more
		} else {
			time.Sleep(500 * time.Millisecond)
		}
	}
}
