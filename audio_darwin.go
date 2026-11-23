//go:build darwin

package main

/*
#cgo LDFLAGS: -framework CoreAudio -framework AudioToolbox -framework Foundation
#include "audio_darwin.h"
*/
import "C"

import (
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
			n := int(C.os_audio_read((*C.float)(&buf[0]), C.int(len(buf))))
			if n > 0 {
				spectrum.Feed(buf[:n], float64(C.os_audio_rate()))
			}
			time.Sleep(30 * time.Millisecond)
		} else {
			time.Sleep(500 * time.Millisecond)
		}
	}
}
