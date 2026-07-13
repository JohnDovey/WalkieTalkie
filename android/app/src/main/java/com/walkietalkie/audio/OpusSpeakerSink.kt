package com.walkietalkie.audio

import android.media.AudioAttributes
import android.media.AudioFormat
import android.media.AudioTrack
import android.media.MediaCodec
import android.media.MediaFormat
import android.util.Log
import media.AudioSink
import java.nio.ByteOrder
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit

/**
 * Decodes incoming Opus frames (via MediaCodec's "audio/opus" decoder,
 * available since API 21) and plays them out the default speaker via
 * [AudioTrack].
 *
 * Opus decoding is stateful per stream, so — mirroring the desktop
 * server/audio/playback.go design — one decoder is kept per peer ID, not
 * shared. Decoded PCM from every peer is queued FIFO into one playback
 * buffer drained by a single writer thread; concurrent talkers are not
 * mixed (acceptable for half-duplex PTT, same simplification as desktop).
 *
 * Not yet verified against real hardware in this dev environment.
 */
class OpusSpeakerSink : AudioSink {

    private val decoders = mutableMapOf<String, MediaCodec>()
    private val pcmQueue = LinkedBlockingQueue<ShortArray>(64)

    private val minBufferSize = AudioTrack.getMinBufferSize(
        OpusMicSource.SAMPLE_RATE, AudioFormat.CHANNEL_OUT_MONO, AudioFormat.ENCODING_PCM_16BIT,
    )

    private val audioTrack = AudioTrack.Builder()
        .setAudioAttributes(
            AudioAttributes.Builder()
                .setUsage(AudioAttributes.USAGE_VOICE_COMMUNICATION)
                .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH)
                .build(),
        )
        .setAudioFormat(
            AudioFormat.Builder()
                .setSampleRate(OpusMicSource.SAMPLE_RATE)
                .setEncoding(AudioFormat.ENCODING_PCM_16BIT)
                .setChannelMask(AudioFormat.CHANNEL_OUT_MONO)
                .build(),
        )
        .setBufferSizeInBytes(maxOf(minBufferSize, OpusMicSource.BYTES_PER_FRAME * 4))
        .setTransferMode(AudioTrack.MODE_STREAM)
        .build()

    @Volatile private var running = false
    private var playbackThread: Thread? = null

    private fun ensureStarted() {
        if (running) return
        running = true
        audioTrack.play()
        playbackThread = Thread({
            val silence = ShortArray(SAMPLES_PER_FRAME)
            while (running) {
                val frame = pcmQueue.poll(20, TimeUnit.MILLISECONDS) ?: silence
                audioTrack.write(frame, 0, frame.size)
            }
        }, "opus-playback").apply { start() }
    }

    override fun writeOpusFrame(peerID: String, frame: ByteArray) {
        ensureStarted()

        val codec = decoders.getOrPut(peerID) {
            MediaCodec.createDecoderByType("audio/opus").apply {
                val format = MediaFormat.createAudioFormat(
                    "audio/opus", OpusMicSource.SAMPLE_RATE, OpusMicSource.CHANNEL_COUNT,
                )
                configure(format, null, null, 0)
                start()
            }
        }

        try {
            val inputIndex = codec.dequeueInputBuffer(TIMEOUT_US)
            if (inputIndex >= 0) {
                val inputBuffer = codec.getInputBuffer(inputIndex)
                    ?: throw IllegalStateException("MediaCodec returned no input buffer")
                inputBuffer.clear()
                inputBuffer.put(frame)
                codec.queueInputBuffer(inputIndex, 0, frame.size, System.nanoTime() / 1000, 0)
            }

            val info = MediaCodec.BufferInfo()
            repeat(MAX_DEQUEUE_ATTEMPTS) {
                val outputIndex = codec.dequeueOutputBuffer(info, TIMEOUT_US)
                if (outputIndex >= 0) {
                    val outputBuffer = codec.getOutputBuffer(outputIndex)
                        ?: throw IllegalStateException("MediaCodec returned no output buffer")
                    outputBuffer.order(ByteOrder.LITTLE_ENDIAN)
                    outputBuffer.position(info.offset)
                    outputBuffer.limit(info.offset + info.size)
                    val shorts = ShortArray(info.size / 2)
                    outputBuffer.asShortBuffer().get(shorts)
                    codec.releaseOutputBuffer(outputIndex, false)
                    pcmQueue.offer(shorts) // drop rather than block if the playback queue is full
                    return
                }
            }
        } catch (e: Exception) {
            Log.e(TAG, "failed to decode Opus frame from $peerID", e)
        }
    }

    fun release() {
        running = false
        playbackThread?.join(200)
        decoders.values.forEach { runCatching { it.stop(); it.release() } }
        decoders.clear()
        runCatching { audioTrack.stop() }
        audioTrack.release()
    }

    companion object {
        private const val TAG = "OpusSpeakerSink"
        private const val TIMEOUT_US = 10_000L
        private const val MAX_DEQUEUE_ATTEMPTS = 5
        private const val SAMPLES_PER_FRAME = OpusMicSource.BYTES_PER_FRAME / 2
    }
}
