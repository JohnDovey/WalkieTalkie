package com.walkietalkie.audio

import android.content.Context
import android.media.MediaRecorder
import android.os.Build
import java.io.File

/**
 * Short-lived mic capture to an Opus/Ogg file for store-and-forward
 * voice notes (not live PTT). API 29+ supports OGG + OPUS natively.
 */
class ClipRecorder(private val context: Context) {

    private var recorder: MediaRecorder? = null
    private var outputFile: File? = null

    fun start(): File {
        stopQuietly()
        val file = File(context.cacheDir, "voice-clip-${System.currentTimeMillis()}.ogg")
        outputFile = file
        val rec = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            MediaRecorder(context)
        } else {
            @Suppress("DEPRECATION")
            MediaRecorder()
        }
        rec.setAudioSource(MediaRecorder.AudioSource.VOICE_COMMUNICATION)
        rec.setOutputFormat(MediaRecorder.OutputFormat.OGG)
        rec.setAudioEncoder(MediaRecorder.AudioEncoder.OPUS)
        rec.setAudioSamplingRate(16_000)
        rec.setAudioEncodingBitRate(32_000)
        rec.setOutputFile(file.absolutePath)
        rec.prepare()
        rec.start()
        recorder = rec
        return file
    }

    /** Stops recording and returns the bytes of the clip file. */
    fun stopAndRead(): ByteArray {
        val rec = recorder ?: throw IllegalStateException("not recording")
        try {
            rec.stop()
        } finally {
            rec.release()
            recorder = null
        }
        val file = outputFile ?: throw IllegalStateException("no output file")
        val bytes = file.readBytes()
        file.delete()
        outputFile = null
        return bytes
    }

    fun cancel() {
        stopQuietly()
        outputFile?.delete()
        outputFile = null
    }

    private fun stopQuietly() {
        try {
            recorder?.stop()
        } catch (_: Exception) {
        }
        try {
            recorder?.release()
        } catch (_: Exception) {
        }
        recorder = null
    }
}
