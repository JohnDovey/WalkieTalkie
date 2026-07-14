#ifndef WalkieTalkie_OpusCodec_h
#define WalkieTalkie_OpusCodec_h

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/// Opaque encoder/decoder refs (void*) so Swift can import them without incomplete-struct issues.
typedef void *WTOpusEncoderRef;
typedef void *WTOpusDecoderRef;

WTOpusEncoderRef WTOpusEncoderCreate(int sampleRate, int channels, int application, int *error);
void WTOpusEncoderDestroy(WTOpusEncoderRef enc);
int WTOpusEncoderSetBitrate(WTOpusEncoderRef enc, int bitrate);
int WTOpusEncode(WTOpusEncoderRef enc, const int16_t *pcm, int frameSize,
                 unsigned char *out, int maxOutBytes);

WTOpusDecoderRef WTOpusDecoderCreate(int sampleRate, int channels, int *error);
void WTOpusDecoderDestroy(WTOpusDecoderRef dec);
int WTOpusDecode(WTOpusDecoderRef dec, const unsigned char *data, int len,
                 int16_t *pcm, int frameSize, int decodeFEC);

#ifdef __cplusplus
}
#endif

#endif
