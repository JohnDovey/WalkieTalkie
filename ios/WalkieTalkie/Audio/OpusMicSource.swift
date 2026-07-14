import AVFoundation
import Foundation
import Core

/// Mic capture + Opus encode — implements gomobile `MediaAudioSource` protocol
/// (Swift imports the ObjC protocol as MediaAudioSourceProtocol; the class name is MediaAudioSource).
/// Matches Android OpusMicSource: 48 kHz mono, ~20 ms frames, 32 kbps.
final class OpusMicSource: NSObject, MediaAudioSourceProtocol {
    static let sampleRate: Int32 = 48_000
    static let channels: Int32 = 1
    static let frameDurationMs = 20
    static let samplesPerFrame = Int(sampleRate) * frameDurationMs / 1000 // 960
    static let bitrate = 32_000

    private let engine = AVAudioEngine()
    private let encodeQueue = DispatchQueue(label: "com.walkietalkie.opus.mic")
    private var encoder: WTOpusEncoderRef?
    private var pcmRing = Data()
    private let pcmLock = NSLock()
    private var frameCondition = NSCondition()
    private var pendingFrames: [Data] = []
    private var started = false
    private var stopped = false

    override init() {
        super.init()
        var err: Int32 = 0
        encoder = WTOpusEncoderCreate(Self.sampleRate, Self.channels, OPUS_APPLICATION_VOIP, &err)
        if let encoder {
            WTOpusEncoderSetBitrate(encoder, Int32(Self.bitrate))
        }
    }

    private func ensureStarted() throws {
        if started { return }
        stopped = false
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.playAndRecord, mode: .voiceChat, options: [.defaultToSpeaker, .allowBluetooth])
        try session.setActive(true)

        let input = engine.inputNode
        let format = AVAudioFormat(commonFormat: .pcmFormatInt16,
                                   sampleRate: Double(Self.sampleRate),
                                   channels: AVAudioChannelCount(Self.channels),
                                   interleaved: true)!
        // Convert from hardware format to 16-bit 48k mono via tap converter if needed.
        let hwFormat = input.outputFormat(forBus: 0)
        input.installTap(onBus: 0, bufferSize: AVAudioFrameCount(Self.samplesPerFrame), format: hwFormat) { [weak self] buffer, _ in
            self?.ingest(buffer: buffer, targetFormat: format)
        }
        try engine.start()
        started = true
    }

    private func ingest(buffer: AVAudioPCMBuffer, targetFormat: AVAudioFormat) {
        guard let converter = AVAudioConverter(from: buffer.format, to: targetFormat) else { return }
        let outFrames = AVAudioFrameCount(Self.samplesPerFrame * 4)
        guard let out = AVAudioPCMBuffer(pcmFormat: targetFormat, frameCapacity: outFrames) else { return }
        var error: NSError?
        let inputBlock: AVAudioConverterInputBlock = { _, outStatus in
            outStatus.pointee = .haveData
            return buffer
        }
        converter.convert(to: out, error: &error, withInputFrom: inputBlock)
        guard error == nil, let ch = out.int16ChannelData?[0] else { return }
        let byteCount = Int(out.frameLength) * MemoryLayout<Int16>.size
        let data = Data(bytes: ch, count: byteCount)
        pcmLock.lock()
        pcmRing.append(data)
        let need = Self.samplesPerFrame * MemoryLayout<Int16>.size
        while pcmRing.count >= need {
            let framePCM = pcmRing.prefix(need)
            pcmRing.removeFirst(need)
            encodeQueue.async { [weak self] in
                self?.encodeAndQueue(Data(framePCM))
            }
        }
        pcmLock.unlock()
    }

    private func encodeAndQueue(_ pcm: Data) {
        guard let encoder else { return }
        var out = [UInt8](repeating: 0, count: 4000)
        let samples = pcm.withUnsafeBytes { raw -> Int32 in
            guard let base = raw.bindMemory(to: Int16.self).baseAddress else { return 0 }
            return WTOpusEncode(encoder, base, Int32(Self.samplesPerFrame), &out, Int32(out.count))
        }
        guard samples > 0 else { return }
        let frame = Data(out.prefix(Int(samples)))
        frameCondition.lock()
        pendingFrames.append(frame)
        frameCondition.signal()
        frameCondition.unlock()
    }

    /// gomobile MediaAudioSource.readOpusFrame
    func readOpusFrame() throws -> Data {
        try ensureStarted()
        frameCondition.lock()
        defer { frameCondition.unlock() }
        let deadline = Date().addingTimeInterval(0.05)
        while pendingFrames.isEmpty && !stopped {
            if !frameCondition.wait(until: deadline) { break }
        }
        if pendingFrames.isEmpty { return Data() }
        return pendingFrames.removeFirst()
    }

    /// gomobile MediaAudioSource.stop — release mic between PTT sessions.
    func stop() throws {
        stopped = true
        if started {
            engine.inputNode.removeTap(onBus: 0)
            engine.stop()
            started = false
        }
        frameCondition.lock()
        pendingFrames.removeAll()
        frameCondition.broadcast()
        frameCondition.unlock()
        pcmLock.lock()
        pcmRing.removeAll()
        pcmLock.unlock()
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }

    func releaseResources() {
        try? stop()
        if let encoder {
            WTOpusEncoderDestroy(encoder)
            self.encoder = nil
        }
    }

    deinit {
        releaseResources()
    }
}

// Opus application constant from libopus (voip = 2048).
private let OPUS_APPLICATION_VOIP: Int32 = 2048
