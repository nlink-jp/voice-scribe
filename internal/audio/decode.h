#ifndef VOICE_SCRIBE_DECODE_H
#define VOICE_SCRIBE_DECODE_H

#include <stdint.h>

// Sample rate and channel count whisper.cpp requires. Not configurable: the
// model's mel front-end is trained at 16 kHz mono, so anything else is wrong
// rather than merely different.
#define VS_SAMPLE_RATE 16000
#define VS_CHANNELS 1

// vs_decode reads any container AVFoundation understands and produces 16 kHz
// mono 32-bit float PCM.
//
// On success it returns 0, sets *out_samples to a malloc'd buffer the caller
// must free with vs_free, and sets *out_count to the number of float samples.
// On failure it returns non-zero and sets *out_error to a malloc'd,
// NUL-terminated message the caller must free with vs_free.
//
// Return codes distinguish the cases a caller can act on:
//   1  the file does not exist or cannot be opened
//   2  the file has no audio track (a silent video, say)
//   3  AVFoundation cannot decode this container or codec
//   4  decoding started but failed part way through
int vs_decode(const char *path, float **out_samples, int64_t *out_count, char **out_error);

// vs_free releases a buffer returned by vs_decode.
void vs_free(void *p);

#endif
