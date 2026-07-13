package com.walkietalkie.audio

import android.media.AudioFormat
import android.media.AudioRecord
import android.media.MediaCodec
import android.media.MediaFormat
import android.media.MediaRecorder
import media.AudioSource

/**
 * Captures the mic via [AudioRecord] and encodes it to Opus via
 * [MediaCodec]'s hardware/software "audio/opus" encoder — no cgo, unlike
 * the desktop's pion/mediadevices+hraban/opus path (see core/media's
 * doc comment on the codec/capture split).
 *
 * Android's MediaCodec Opus **encoder** (as opposed to the decoder, which
 * is older) was only added in Android 10 (API 29) — this is why this
 * app's minSdk is 29, not 26 like the ClonesApp precedent.
 *
 * Not yet verified against real hardware in this dev environment (no
 * attached Android device/emulator with a working mic) — the
 * dequeueOutputBuffer retry loop in particular (MediaCodec doesn't
 * guarantee 1 output per 1 input call, especially for the first few
 * frames) should be watched closely on first real-device testing.
 */
class OpusMicSource : AudioSource {

    private val minBufferSize = AudioRecord.getMinBufferSize(
        SAMPLE_RATE, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT,
    )

    private val audioRecord = AudioRecord(
        MediaRecorder.AudioSource.VOICE_COMMUNICATION,
        SAMPLE_RATE,
        AudioFormat.CHANNEL_IN_MONO,
        AudioFormat.ENCODING_PCM_16BIT,
        maxOf(minBufferSize, BYTES_PER_FRAME * 4),
    )

    private val codec = MediaCodec.createEncoderByType("audio/opus").apply {
        val format = MediaFormat.createAudioFormat("audio/opus", SAMPLE_RATE, CHANNEL_COUNT).apply {
            setInteger(MediaFormat.KEY_BIT_RATE, 32_000)
        }
        configure(format, null, null, MediaCodec.CONFIGURE_FLAG_ENCODE)
    }

    private val pcmBuffer = ByteArray(BYTES_PER_FRAME)
    private var started = false

    private fun ensureStarted() {
        if (started) return
        audioRecord.startRecording()
        codec.start()
        started = true
    }

    override fun readOpusFrame(): ByteArray {
        ensureStarted()

        var offset = 0
        while (offset < pcmBuffer.size) {
            val n = audioRecord.read(pcmBuffer, offset, pcmBuffer.size - offset)
            if (n <= 0) break
            offset += n
        }

        val inputIndex = codec.dequeueInputBuffer(TIMEOUT_US)
        if (inputIndex >= 0) {
            val inputBuffer = codec.getInputBuffer(inputIndex)
                ?: throw IllegalStateException("MediaCodec returned no input buffer")
            inputBuffer.clear()
            inputBuffer.put(pcmBuffer)
            codec.queueInputBuffer(inputIndex, 0, pcmBuffer.size, System.nanoTime() / 1000, 0)
        }

        val info = MediaCodec.BufferInfo()
        repeat(MAX_DEQUEUE_ATTEMPTS) {
            val outputIndex = codec.dequeueOutputBuffer(info, TIMEOUT_US)
            if (outputIndex >= 0) {
                val outputBuffer = codec.getOutputBuffer(outputIndex)
                    ?: throw IllegalStateException("MediaCodec returned no output buffer")
                val frame = ByteArray(info.size)
                outputBuffer.position(info.offset)
                outputBuffer.get(frame)
                codec.releaseOutputBuffer(outputIndex, false)
                return frame
            }
        }
        // No encoded frame ready yet (codec pipeline lookahead) — core/media's
        // talk loop calls this in a tight cycle, so returning empty here just
        // means "nothing to send this cycle," not an error.
        return ByteArray(0)
    }

    fun release() {
        started = false
        runCatching { audioRecord.stop() }
        audioRecord.release()
        runCatching { codec.stop() }
        codec.release()
    }

    companion object {
        const val SAMPLE_RATE = 48_000
        const val CHANNEL_COUNT = 1
        private const val FRAME_DURATION_MS = 20
        private const val SAMPLES_PER_FRAME = SAMPLE_RATE * FRAME_DURATION_MS / 1000 // 960
        private const val BYTES_PER_SAMPLE = 2 // 16-bit PCM
        const val BYTES_PER_FRAME = SAMPLES_PER_FRAME * BYTES_PER_SAMPLE // 1920
        private const val TIMEOUT_US = 10_000L
        private const val MAX_DEQUEUE_ATTEMPTS = 5
    }
}
