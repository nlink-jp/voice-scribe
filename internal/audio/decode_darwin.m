// Objective-C half of the audio decoder. The _darwin suffix is what limits it
// to macOS builds; a //go:build line here would be an inert comment.
#import <AVFoundation/AVFoundation.h>
#import <Foundation/Foundation.h>

#include <stdlib.h>
#include <string.h>

#include "decode.h"

static char *vs_copy_error(NSString *message) {
    const char *utf8 = [message UTF8String];
    if (utf8 == NULL) {
        utf8 = "unknown error";
    }
    size_t len = strlen(utf8);
    char *copy = malloc(len + 1);
    if (copy == NULL) {
        return NULL;
    }
    memcpy(copy, utf8, len + 1);
    return copy;
}

void vs_free(void *p) {
    free(p);
}

int vs_decode(const char *path, float **out_samples, int64_t *out_count, char **out_error) {
    *out_samples = NULL;
    *out_count = 0;
    *out_error = NULL;

    @autoreleasepool {
        NSString *nsPath = [NSString stringWithUTF8String:path];
        if (nsPath == nil) {
            *out_error = vs_copy_error(@"path is not valid UTF-8");
            return 1;
        }
        if (![[NSFileManager defaultManager] fileExistsAtPath:nsPath]) {
            *out_error = vs_copy_error([NSString stringWithFormat:@"no such file: %@", nsPath]);
            return 1;
        }

        NSURL *url = [NSURL fileURLWithPath:nsPath];
        AVURLAsset *asset = [AVURLAsset URLAssetWithURL:url options:nil];

        // The synchronous -tracksWithMediaType: was deprecated in macOS 15. The
        // replacement is asynchronous, and the completion handler runs on an
        // AVFoundation-owned queue rather than this thread, so blocking here on
        // a semaphore is safe: the caller is a goroutine that is waiting for
        // this result anyway.
        __block NSArray<AVAssetTrack *> *tracks = nil;
        __block NSError *loadErr = nil;
        dispatch_semaphore_t loaded = dispatch_semaphore_create(0);
        [asset loadTracksWithMediaType:AVMediaTypeAudio
                     completionHandler:^(NSArray<AVAssetTrack *> *result, NSError *error) {
            tracks = result;
            loadErr = error;
            dispatch_semaphore_signal(loaded);
        }];
        dispatch_semaphore_wait(loaded, DISPATCH_TIME_FOREVER);

        if (loadErr != nil) {
            *out_error = vs_copy_error([NSString stringWithFormat:@"cannot read this container: %@",
                                        loadErr.localizedDescription]);
            return 3;
        }
        if (tracks.count == 0) {
            *out_error = vs_copy_error(@"file has no audio track");
            return 2;
        }

        // The autoreleasing class method rather than alloc/init: this file is
        // compiled without ARC, so an owned reference would have to be released
        // on every one of the failure paths below.
        NSError *err = nil;
        AVAssetReader *reader = [AVAssetReader assetReaderWithAsset:asset error:&err];
        if (reader == nil) {
            *out_error = vs_copy_error(err != nil
                ? [NSString stringWithFormat:@"cannot read this container: %@", err.localizedDescription]
                : @"cannot read this container");
            return 3;
        }

        // Ask AVFoundation for exactly what whisper.cpp wants, and let it do
        // the resampling and downmixing: an audio mix output accepts every
        // track at once and hands back a single interleaved stream.
        NSDictionary *settings = @{
            AVFormatIDKey            : @(kAudioFormatLinearPCM),
            AVSampleRateKey          : @(VS_SAMPLE_RATE),
            AVNumberOfChannelsKey    : @(VS_CHANNELS),
            AVLinearPCMBitDepthKey   : @32,
            AVLinearPCMIsFloatKey    : @YES,
            AVLinearPCMIsBigEndianKey: @NO,
            AVLinearPCMIsNonInterleaved: @NO,
        };

        AVAssetReaderAudioMixOutput *output =
            [AVAssetReaderAudioMixOutput assetReaderAudioMixOutputWithAudioTracks:tracks
                                                                    audioSettings:settings];
        if (![reader canAddOutput:output]) {
            *out_error = vs_copy_error(@"cannot decode this audio codec to 16 kHz mono PCM");
            return 3;
        }
        [reader addOutput:output];

        if (![reader startReading]) {
            *out_error = vs_copy_error([NSString stringWithFormat:@"cannot start reading: %@",
                                        reader.error.localizedDescription]);
            return 3;
        }

        size_t capacity = VS_SAMPLE_RATE * 60; // grows as needed; one minute to start
        float *samples = malloc(capacity * sizeof(float));
        if (samples == NULL) {
            [reader cancelReading];
            *out_error = vs_copy_error(@"out of memory");
            return 4;
        }
        size_t count = 0;

        CMSampleBufferRef buffer = NULL;
        while ((buffer = [output copyNextSampleBuffer]) != NULL) {
            CMBlockBufferRef block = CMSampleBufferGetDataBuffer(buffer);
            if (block == NULL) {
                CFRelease(buffer);
                continue;
            }

            size_t length = CMBlockBufferGetDataLength(block);
            size_t incoming = length / sizeof(float);
            if (incoming > 0) {
                if (count + incoming > capacity) {
                    while (count + incoming > capacity) {
                        capacity *= 2;
                    }
                    float *grown = realloc(samples, capacity * sizeof(float));
                    if (grown == NULL) {
                        free(samples);
                        CFRelease(buffer);
                        [reader cancelReading];
                        *out_error = vs_copy_error(@"out of memory while decoding");
                        return 4;
                    }
                    samples = grown;
                }

                OSStatus status = CMBlockBufferCopyDataBytes(block, 0, length, samples + count);
                if (status != kCMBlockBufferNoErr) {
                    free(samples);
                    CFRelease(buffer);
                    [reader cancelReading];
                    *out_error = vs_copy_error(@"failed to copy decoded audio");
                    return 4;
                }
                count += incoming;
            }
            CFRelease(buffer);
        }

        // A reader that stopped for any reason other than running out of input
        // has produced a truncated result. Returning it as a success would hand
        // back a transcript of the first half of a file with no indication that
        // the rest is missing.
        if (reader.status != AVAssetReaderStatusCompleted) {
            NSString *why = reader.error != nil ? reader.error.localizedDescription : @"reading did not complete";
            free(samples);
            *out_error = vs_copy_error([NSString stringWithFormat:@"decoding failed: %@", why]);
            return 4;
        }

        if (count == 0) {
            free(samples);
            *out_error = vs_copy_error(@"audio track decoded to zero samples");
            return 4;
        }

        *out_samples = samples;
        *out_count = (int64_t)count;
        return 0;
    }
}
