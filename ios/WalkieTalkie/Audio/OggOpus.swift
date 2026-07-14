import Foundation

/// Minimal single-stream Ogg Opus mux/demux for voice-note files that interoperate with Android's OGG Opus clips.
enum OggOpus {
    static func mux(packets: [Data], sampleRate: Int = 48_000, channels: Int = 1) -> Data {
        var out = Data()
        out.append(page(packets: [opusHead(sampleRate: sampleRate, channels: channels)], streamSerial: 0x574F5053, pageSequence: 0, headerType: 0x02))
        out.append(page(packets: [opusTags()], streamSerial: 0x574F5053, pageSequence: 1, headerType: 0x00))
        var seq: UInt32 = 2
        var gran: UInt64 = 0
        let frameSamples: UInt64 = 960 // 20ms @ 48k
        for (i, packet) in packets.enumerated() {
            gran += frameSamples
            let last = i == packets.count - 1
            out.append(page(packets: [packet], streamSerial: 0x574F5053, pageSequence: seq, headerType: last ? 0x04 : 0x00, granule: gran))
            seq &+= 1
        }
        return out
    }

    /// Extract raw Opus packets from an Ogg Opus bitstream (skips OpusHead/OpusTags).
    static func demux(_ data: Data) -> [Data] {
        var packets: [Data] = []
        var offset = 0
        while offset + 27 <= data.count {
            guard data[offset] == 0x4F, data[offset + 1] == 0x67, data[offset + 2] == 0x67, data[offset + 3] == 0x53 else { break }
            let pageSegments = Int(data[offset + 26])
            let segmentTableStart = offset + 27
            guard segmentTableStart + pageSegments <= data.count else { break }
            var bodySize = 0
            var lengths: [Int] = []
            var i = 0
            while i < pageSegments {
                var len = 0
                while i < pageSegments {
                    let seg = Int(data[segmentTableStart + i])
                    i += 1
                    len += seg
                    if seg < 255 { break }
                }
                lengths.append(len)
                bodySize += len
            }
            let bodyStart = segmentTableStart + pageSegments
            guard bodyStart + bodySize <= data.count else { break }
            var bodyOff = bodyStart
            for len in lengths {
                let packet = data.subdata(in: bodyOff..<(bodyOff + len))
                bodyOff += len
                if packet.starts(with: [0x4F, 0x70, 0x75, 0x73]) { continue } // OpusHead / OpusTags
                if !packet.isEmpty { packets.append(packet) }
            }
            offset = bodyStart + bodySize
        }
        return packets
    }

    private static func opusHead(sampleRate: Int, channels: Int) -> Data {
        var d = Data("OpusHead".utf8)
        d.append(1) // version
        d.append(UInt8(channels))
        d.append(contentsOf: UInt16(0).littleEndianBytes) // pre-skip
        d.append(contentsOf: UInt32(sampleRate).littleEndianBytes)
        d.append(contentsOf: Int16(0).littleEndianBytes) // output gain
        d.append(0) // channel mapping family
        return d
    }

    private static func opusTags() -> Data {
        var d = Data("OpusTags".utf8)
        let vendor = Array("WalkieTalkie".utf8)
        d.append(contentsOf: UInt32(vendor.count).littleEndianBytes)
        d.append(contentsOf: vendor)
        d.append(contentsOf: UInt32(0).littleEndianBytes) // user comment list count
        return d
    }

    private static func page(packets: [Data], streamSerial: UInt32, pageSequence: UInt32, headerType: UInt8, granule: UInt64 = 0) -> Data {
        var segmentTable = Data()
        var body = Data()
        for packet in packets {
            body.append(packet)
            var remaining = packet.count
            while remaining >= 255 {
                segmentTable.append(255)
                remaining -= 255
            }
            segmentTable.append(UInt8(remaining))
        }
        var header = Data()
        header.append(contentsOf: [0x4F, 0x67, 0x67, 0x53]) // OggS
        header.append(0) // version
        header.append(headerType)
        header.append(contentsOf: granule.littleEndianBytes)
        header.append(contentsOf: streamSerial.littleEndianBytes)
        header.append(contentsOf: pageSequence.littleEndianBytes)
        header.append(contentsOf: UInt32(0).littleEndianBytes) // CRC placeholder
        header.append(UInt8(segmentTable.count))
        header.append(segmentTable)
        header.append(body)
        let crc = oggCRC(header)
        header.replaceSubrange(22..<26, with: crc.littleEndianBytes)
        return header
    }

    private static func oggCRC(_ data: Data) -> UInt32 {
        var crc: UInt32 = 0
        for byte in data {
            crc = (crc << 8) ^ oggCRCTable[Int(((crc >> 24) & 0xff) ^ UInt32(byte))]
        }
        return crc
    }

    private static let oggCRCTable: [UInt32] = {
        (0..<256).map { i -> UInt32 in
            var r = UInt32(i) << 24
            for _ in 0..<8 {
                if r & 0x8000_0000 != 0 {
                    r = (r << 1) ^ 0x04c1_1db7
                } else {
                    r <<= 1
                }
            }
            return r
        }
    }()
}

private extension FixedWidthInteger {
    var littleEndianBytes: [UInt8] {
        withUnsafeBytes(of: self.littleEndian) { Array($0) }
    }
}
