import Foundation

struct DeviceRow: Identifiable {
    let id: String
    let name: String
    let platform: String
    let status: String
    let discoveryMethods: [String]

    var isConnected: Bool { status == "connected" }

    static func parse(_ json: String) -> [DeviceRow] {
        guard let data = json.data(using: .utf8),
              let arr = try? JSONSerialization.jsonObject(with: data) as? [[String: Any]] else {
            return []
        }
        return arr.compactMap { dict in
            guard let id = dict["id"] as? String else { return nil }
            return DeviceRow(
                id: id,
                name: dict["name"] as? String ?? id,
                platform: dict["platform"] as? String ?? "?",
                status: dict["status"] as? String ?? "",
                discoveryMethods: dict["discoveryMethods"] as? [String] ?? []
            )
        }
        .sorted { a, b in
            if a.isConnected != b.isConnected { return a.isConnected && !b.isConnected }
            return a.name.localizedCaseInsensitiveCompare(b.name) == .orderedAscending
        }
    }
}

struct ChannelPeer: Identifiable {
    let id: String
    let name: String
}

struct ChannelRow: Identifiable {
    let id: String
    let peerId: String
    let peerName: String
    let status: String
    let unread: Int
    let peers: [ChannelPeer]

    var displayName: String {
        if peers.count > 1 {
            return peers.map { $0.name.isEmpty ? $0.id : $0.name }.joined(separator: ", ")
        }
        return peerName.isEmpty ? peerId : peerName
    }

    static func parse(_ json: String) -> [ChannelRow] {
        guard let data = json.data(using: .utf8),
              let arr = try? JSONSerialization.jsonObject(with: data) as? [[String: Any]] else {
            return []
        }
        return arr.compactMap { dict in
            guard let id = dict["id"] as? String else { return nil }
            var peers: [ChannelPeer] = []
            if let rawPeers = dict["peers"] as? [[String: Any]] {
                peers = rawPeers.compactMap { p in
                    guard let pid = p["id"] as? String else { return nil }
                    return ChannelPeer(id: pid, name: p["name"] as? String ?? pid)
                }
            }
            return ChannelRow(
                id: id,
                peerId: dict["peerId"] as? String ?? "",
                peerName: dict["peerName"] as? String ?? "",
                status: dict["status"] as? String ?? "",
                unread: dict["unreadFor"] as? Int ?? 0,
                peers: peers
            )
        }
    }

    /// Whether peerID appears in the channel's focused set (or legacy focusedBy).
    static func peerFocused(in json: String, channelID: String, peerID: String) -> Bool {
        guard !peerID.isEmpty, !channelID.isEmpty,
              let data = json.data(using: .utf8),
              let arr = try? JSONSerialization.jsonObject(with: data) as? [[String: Any]] else {
            return false
        }
        for dict in arr {
            guard (dict["id"] as? String) == channelID else { continue }
            if let focused = dict["focused"] as? [String], focused.contains(peerID) {
                return true
            }
            if (dict["focusedBy"] as? String) == peerID {
                return true
            }
        }
        return false
    }
}

struct VoiceNoteRow: Identifiable {
    let id: String
    let fromId: String
    let toId: String
    let status: String
    let channelId: String

    static func parse(_ json: String) -> [VoiceNoteRow] {
        guard let data = json.data(using: .utf8),
              let arr = try? JSONSerialization.jsonObject(with: data) as? [[String: Any]] else {
            return []
        }
        return arr.compactMap { dict in
            guard let id = dict["id"] as? String else { return nil }
            return VoiceNoteRow(
                id: id,
                fromId: dict["fromId"] as? String ?? "",
                toId: dict["toId"] as? String ?? "",
                status: dict["status"] as? String ?? "",
                channelId: dict["channelId"] as? String ?? ""
            )
        }
    }

    /// Count queued 1:1 inbox notes for this device, grouped by sender.
    static func countQueuedInbox(_ json: String, selfId: String) -> [String: Int] {
        var counts: [String: Int] = [:]
        for n in parse(json) {
            guard n.status == "queued", n.channelId.isEmpty, n.toId == selfId else { continue }
            counts[n.fromId, default: 0] += 1
        }
        return counts
    }
}
