import AVFoundation
import Foundation

/// Plays voice-note bytes: AVAudioPlayer for containers it understands, else Ogg Opus → PCM via libopus.
final class VoiceNotePlayer {
    private var avPlayer: AVAudioPlayer?
    private let engine = AVAudioEngine()
    private let playerNode = AVAudioPlayerNode()
    private var attached = false

    func stop() {
        avPlayer?.stop()
        avPlayer = nil
        if attached {
            playerNode.stop()
            engine.stop()
        }
    }

    func play(_ data: Data) throws {
        stop()
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.playAndRecord, mode: .voiceChat, options: [.defaultToSpeaker, .allowBluetooth])
        try session.setActive(true)

        if data.count >= 4, String(data: data.prefix(4), encoding: .ascii) == "OggS" {
            try playOggOpus(data)
            return
        }

        // CAF / AAC / etc.
        let tmp = FileManager.default.temporaryDirectory.appendingPathComponent("vn-\(UUID().uuidString).bin")
        try data.write(to: tmp)
        defer { try? FileManager.default.removeItem(at: tmp) }
        let player = try AVAudioPlayer(contentsOf: tmp)
        player.prepareToPlay()
        player.play()
        avPlayer = player
    }

    private func playOggOpus(_ data: Data) throws {
        let packets = OggOpus.demux(data)
        guard !packets.isEmpty else { throw PlayError.noPackets }

        var err: Int32 = 0
        guard let decoder = WTOpusDecoderCreate(48_000, 1, &err) else { throw PlayError.decoder }
        defer { WTOpusDecoderDestroy(decoder) }

        let format = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: 48_000, channels: 1, interleaved: true)!
        if !attached {
            engine.attach(playerNode)
            engine.connect(playerNode, to: engine.mainMixerNode, format: format)
            attached = true
        }
        try engine.start()
        playerNode.play()

        for packet in packets {
            var pcm = [Int16](repeating: 0, count: 960 * 2)
            let n = packet.withUnsafeBytes { raw -> Int32 in
                guard let base = raw.bindMemory(to: UInt8.self).baseAddress else { return 0 }
                return WTOpusDecode(decoder, base, Int32(packet.count), &pcm, 960, 0)
            }
            guard n > 0, let buffer = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: AVAudioFrameCount(n)) else { continue }
            buffer.frameLength = AVAudioFrameCount(n)
            if let dest = buffer.int16ChannelData?[0] {
                pcm.withUnsafeBufferPointer { src in
                    dest.update(from: src.baseAddress!, count: Int(n))
                }
            }
            playerNode.scheduleBuffer(buffer, completionHandler: nil)
        }
    }

    enum PlayError: Error {
        case noPackets, decoder
    }
}
