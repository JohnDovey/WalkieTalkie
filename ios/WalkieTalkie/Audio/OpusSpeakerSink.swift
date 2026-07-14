import AVFoundation
import Foundation
import Core

/// Opus decode + speaker playback — implements gomobile `MediaAudioSink` protocol
/// (Swift: MediaAudioSinkProtocol). One decoder per peer ID, shared playback engine.
final class OpusSpeakerSink: NSObject, MediaAudioSinkProtocol {
    private let engine = AVAudioEngine()
    private let player = AVAudioPlayerNode()
    private var decoders: [String: WTOpusDecoderRef] = [:]
    private let lock = NSLock()
    private var started = false

    private let format = AVAudioFormat(
        commonFormat: .pcmFormatInt16,
        sampleRate: Double(OpusMicSource.sampleRate),
        channels: AVAudioChannelCount(OpusMicSource.channels),
        interleaved: true
    )!

    override init() {
        super.init()
        engine.attach(player)
        engine.connect(player, to: engine.mainMixerNode, format: format)
    }

    private func ensureStarted() throws {
        if started { return }
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.playAndRecord, mode: .voiceChat, options: [.defaultToSpeaker, .allowBluetooth])
        try session.setActive(true)
        try engine.start()
        player.play()
        started = true
    }

    /// gomobile MediaAudioSink.writeOpusFrame
    func writeOpusFrame(_ peerID: String?, frame: Data?) throws {
        guard let peerID, let frame, !frame.isEmpty else { return }
        try ensureStarted()

        lock.lock()
        let decoder: WTOpusDecoderRef = {
            if let existing = decoders[peerID] { return existing }
            var err: Int32 = 0
            let created = WTOpusDecoderCreate(OpusMicSource.sampleRate, OpusMicSource.channels, &err)!
            decoders[peerID] = created
            return created
        }()
        lock.unlock()

        var pcm = [Int16](repeating: 0, count: OpusMicSource.samplesPerFrame)
        let n = frame.withUnsafeBytes { raw -> Int32 in
            guard let base = raw.bindMemory(to: UInt8.self).baseAddress else { return 0 }
            return WTOpusDecode(decoder, base, Int32(frame.count), &pcm, Int32(OpusMicSource.samplesPerFrame), 0)
        }
        guard n > 0 else { return }

        guard let buffer = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: AVAudioFrameCount(n)) else { return }
        buffer.frameLength = AVAudioFrameCount(n)
        if let dest = buffer.int16ChannelData?[0] {
            pcm.withUnsafeBufferPointer { src in
                dest.update(from: src.baseAddress!, count: Int(n))
            }
        }
        player.scheduleBuffer(buffer, completionHandler: nil)
    }

    func releaseResources() {
        lock.lock()
        for (_, dec) in decoders {
            WTOpusDecoderDestroy(dec)
        }
        decoders.removeAll()
        lock.unlock()
        if started {
            player.stop()
            engine.stop()
            started = false
        }
    }

    deinit {
        releaseResources()
    }
}
