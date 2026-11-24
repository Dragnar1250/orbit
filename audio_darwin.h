#pragma once
int os_audio_start(void);
void os_audio_stop(void);
int os_audio_read(float *out, int max);
double os_audio_rate(void);
double os_audio_out_latency(void);
void os_audio_set_delay(double secs);
