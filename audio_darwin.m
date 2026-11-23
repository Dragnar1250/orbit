// System audio for the visualizer and the lyrics' motion on the Mac: a Core Audio process tap
// (macOS 14.2+) on everything that plays, through a private aggregate device. The audio thread
// only copies mono samples into a ring; Go reads them ~30 times a second.
#import <Foundation/Foundation.h>
#import <CoreAudio/CoreAudio.h>
#import <AudioToolbox/AudioToolbox.h>
#if __has_include(<CoreAudio/AudioHardwareTapping.h>)
#import <CoreAudio/AudioHardwareTapping.h>
#import <CoreAudio/CATapDescription.h>
#define HAVE_TAPS 1
#endif
#include <stdatomic.h>
#include "audio_darwin.h"

#define RING 65536
static float ring[RING];
static _Atomic unsigned long wpos, rpos;
static double rate = 48000;
static AudioObjectID tapID, aggID;
static AudioDeviceIOProcID procID;
static int running;

double os_audio_rate(void) { return rate; }

int os_audio_read(float *out, int max) {
	unsigned long w = atomic_load(&wpos), r = atomic_load(&rpos);
	if (w - r > RING) r = w - RING; // fell behind: keep the newest
	int n = 0;
	while (r < w && n < max) out[n++] = ring[r++ % RING];
	atomic_store(&rpos, r);
	return n;
}

// os_audio_start: 0 ok, 1 not available on this macOS, else the error (e.g. permission refused)
int os_audio_start(void) {
	if (running) return 0;
#ifdef HAVE_TAPS
	if (@available(macOS 14.2, *)) {
		CATapDescription *d = [[CATapDescription alloc] initStereoGlobalTapButExcludeProcesses:@[]];
		d.name = @"Orbit";
		d.privateTap = YES;
		d.muteBehavior = CATapUnmuted;
		OSStatus st = AudioHardwareCreateProcessTap(d, &tapID);
		if (st != noErr) return (int)st;
		NSDictionary *agg = @{
			@kAudioAggregateDeviceNameKey: @"Orbit",
			@kAudioAggregateDeviceUIDKey: [NSUUID UUID].UUIDString,
			@kAudioAggregateDeviceIsPrivateKey: @YES,
			@kAudioAggregateDeviceIsStackedKey: @NO,
			@kAudioAggregateDeviceTapAutoStartKey: @YES,
			@kAudioAggregateDeviceTapListKey: @[@{@kAudioSubTapUIDKey: d.UUID.UUIDString, @kAudioSubTapDriftCompensationKey: @YES}],
		};
		st = AudioHardwareCreateAggregateDevice((__bridge CFDictionaryRef)agg, &aggID);
		if (st != noErr) { AudioHardwareDestroyProcessTap(tapID); tapID = 0; return (int)st; }
		AudioStreamBasicDescription fmt = {0};
		UInt32 sz = sizeof(fmt);
		AudioObjectPropertyAddress a = {kAudioTapPropertyFormat, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
		if (AudioObjectGetPropertyData(tapID, &a, 0, NULL, &sz, &fmt) == noErr && fmt.mSampleRate > 0) rate = fmt.mSampleRate;
		st = AudioDeviceCreateIOProcIDWithBlock(&procID, aggID, NULL, ^(const AudioTimeStamp *now, const AudioBufferList *in, const AudioTimeStamp *inTime, AudioBufferList *out, const AudioTimeStamp *outTime) {
			if (!in || in->mNumberBuffers == 0) return;
			unsigned long w = atomic_load(&wpos);
			if (in->mNumberBuffers == 1) { // interleaved
				const AudioBuffer *b = &in->mBuffers[0];
				int ch = b->mNumberChannels ? b->mNumberChannels : 1;
				int frames = b->mDataByteSize / (sizeof(float) * ch);
				const float *s = b->mData;
				for (int i = 0; i < frames; i++) {
					float v = 0;
					for (int c = 0; c < ch; c++) v += s[i * ch + c];
					ring[w++ % RING] = v / ch;
				}
			} else { // one buffer per channel
				int ch = in->mNumberBuffers;
				int frames = in->mBuffers[0].mDataByteSize / sizeof(float);
				for (int i = 0; i < frames; i++) {
					float v = 0;
					for (int c = 0; c < ch; c++) v += ((const float *)in->mBuffers[c].mData)[i];
					ring[w++ % RING] = v / ch;
				}
			}
			atomic_store(&wpos, w);
		});
		if (st != noErr) { os_audio_stop(); return (int)st; }
		st = AudioDeviceStart(aggID, procID);
		if (st != noErr) { os_audio_stop(); return (int)st; }
		running = 1;
		return 0;
	}
#endif
	return 1;
}

void os_audio_stop(void) {
#ifdef HAVE_TAPS
	if (@available(macOS 14.2, *)) {
		if (aggID && procID) { AudioDeviceStop(aggID, procID); AudioDeviceDestroyIOProcID(aggID, procID); }
		if (aggID) AudioHardwareDestroyAggregateDevice(aggID);
		if (tapID) AudioHardwareDestroyProcessTap(tapID);
	}
#endif
	procID = NULL;
	aggID = tapID = 0;
	running = 0;
}
