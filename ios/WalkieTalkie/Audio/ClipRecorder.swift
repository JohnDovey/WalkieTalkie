import AVFoundation
import Foundation

/// Short-lived mic capture to an Ogg Opus file for store-and-forward voice notes (mirrors Android ClipRecorder).
final class ClipRecorder {
    private let engine = AVAudioEngine()
    private var encoder: WTOpusEncoderRef?
    private var packets: [Data] = []
    private var pcmPending = Data()
    private let lock = NSLock()
    private var started = false

    private static let sampleRate: Int32 = 48_000
    private static let channels: Int32 = 1
    private static let samplesPerFrame = 960 // 20ms
    private static let bitrate = 32_000

    func start() throws {
        cancel()
        var err: Int32 = 0
        encoder = WTOpusEncoderCreate(Self.sampleRate, Self.channels, 2048 /* OPUS_APPLICATION_VOIP */, &err)
        guard let encoder else { throw ClipError.encoder }
        WTOpusEncoderSetBitrate(encoder, Int32(Self.bitrate))

        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.playAndRecord, mode: .voiceChat, options: [.defaultToSpeaker, .allowBluetooth])
        try session.setActive(true)

        let input = engine.inputNode
        let hw = input.outputFormat(forBus: 0)
        let target = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: Double(Self.sampleRate),
                                   channels: AVAudioChannelCount(Self.channels), interleaved: true)!
        input.installTap(onBus: 0, bufferSize: AVAudioFrameCount(Self.samplesPerFrame), format: hw) { [weak self] buffer, _ in
            self?.ingest(buffer: buffer, target: target)
        }
        try engine.start()
        started = true
    }

    func stopAndRead() throws -> Data {
        guard started else { throw ClipError.notRecording }
        engine.inputNode.removeTap(onBus: 0)
        engine.stop()
        started = false
        lock.lock()
        let packets = self.packets
        self.packets = []
        pcmPending = Data()
        lock.unlock()
        if let encoder {
            WTOpusEncoderDestroy(encoder)
            self.encoder = nil
        }
        guard !packets.isEmpty else { throw ClipError.empty }
        return OggOpus.mux(packets: packets, sampleRate: Int(Self.sampleRate), channels: Int(Self.channels))
    }

    func cancel() {
        if started {
            engine.inputNode.removeTap(onBus: 0)
            engine.stop()
            started = false
        }
        lock.lock()
        packets = []
        pcmPending = Data()
        lock.unlock()
        if let encoder {
            WTOpusEncoderDestroy(encoder)
            self.encoder = nil
        }
    }

    private func ingest(buffer: AVAudioPCMBuffer, target: AVAudioFormat) {
        guard let converter = AVAudioConverter(from: buffer.format, to: target),
              let out = AVAudioPCMBuffer(pcmFormat: target, frameCapacity: AVAudioFrameCount(Self.samplesPerFrame * 4)) else { return }
        var error: NSError?
        let inputBlock: AVAudioConverterInputBlock = { _, status in
            status.pointee = .haveData
            return buffer
        }
        converter.convert(to: out, error: &error, withInputFrom: inputBlock)
        guard error == nil, let ch = out.int16ChannelData?[0] else { return }
        let bytes = Data(bytes: ch, count: Int(out.frameLength) * MemoryLayout<Int16>.size)
        lock.lock()
        pcmPending.append(bytes)
        let need = Self.samplesPerFrame * MemoryLayout<Int16>.size
        while pcmPending.count >= need {
            let frame = pcmPending.prefix(need)
            pcmPending.removeFirst(need)
            if let packet = encode(Data(frame)) {
                packets.append(packet)
            }
        }
        lock.unlock()
    }

    private func encode(_ pcm: Data) -> Data? {
        guard let encoder else { return nil }
        var out = [UInt8](repeating: 0, count: 4000)
        let n = pcm.withUnsafeBytes { raw -> Int32 in
            guard let base = raw.bindMemory(to: Int16.self).baseAddress else { return 0 }
            return WTOpusEncode(encoder, base, Int32(Self.samplesPerFrame), &out, Int32(out.count))
        }
        guard n > 0 else { return nil }
        return Data(out.prefix(Int(n)))
    }

    enum ClipError: Error {
        case encoder, notRecording, empty
    }
}
