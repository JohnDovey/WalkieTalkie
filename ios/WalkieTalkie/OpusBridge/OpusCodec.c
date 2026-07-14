#include "OpusCodec.h"
#include <opus/opus.h>
#include <stdlib.h>

WTOpusEncoderRef WTOpusEncoderCreate(int sampleRate, int channels, int application, int *error) {
    return (WTOpusEncoderRef)opus_encoder_create(sampleRate, channels, application, error);
}

void WTOpusEncoderDestroy(WTOpusEncoderRef enc) {
    if (enc) opus_encoder_destroy((OpusEncoder *)enc);
}

int WTOpusEncoderSetBitrate(WTOpusEncoderRef enc, int bitrate) {
    if (!enc) return OPUS_BAD_ARG;
    return opus_encoder_ctl((OpusEncoder *)enc, OPUS_SET_BITRATE(bitrate));
}

int WTOpusEncode(WTOpusEncoderRef enc, const int16_t *pcm, int frameSize,
                 unsigned char *out, int maxOutBytes) {
    if (!enc) return OPUS_BAD_ARG;
    return opus_encode((OpusEncoder *)enc, pcm, frameSize, out, maxOutBytes);
}

WTOpusDecoderRef WTOpusDecoderCreate(int sampleRate, int channels, int *error) {
    return (WTOpusDecoderRef)opus_decoder_create(sampleRate, channels, error);
}

void WTOpusDecoderDestroy(WTOpusDecoderRef dec) {
    if (dec) opus_decoder_destroy((OpusDecoder *)dec);
}

int WTOpusDecode(WTOpusDecoderRef dec, const unsigned char *data, int len,
                 int16_t *pcm, int frameSize, int decodeFEC) {
    if (!dec) return OPUS_BAD_ARG;
    return opus_decode((OpusDecoder *)dec, data, len, pcm, frameSize, decodeFEC);
}
