import Foundation
import Combine
import UIKit
import Core

/// Lifecycle host for the Go `Mobile.startNode` mesh — mirrors Android's PTTService.
@MainActor
final class NodeController: ObservableObject {
    static let shared = NodeController()

    @Published private(set) var devicesJSON: String = "[]"
    @Published private(set) var baseStationURL: String = ""
    @Published private(set) var selfID: String = ""
    @Published private(set) var isTalking: Bool = false
    @Published private(set) var started: Bool = false
    @Published var nickname: String = NicknameStore.get()
    @Published var statusMessage: String = ""

    private var node: MobileNode?
    private var micSource: OpusMicSource?
    private var speakerSink: OpusSpeakerSink?
    private var locationUpdater: LocationUpdater?
    private var bleBridge: BlePresenceBridge?
    private var pollTimer: Timer?
    private let ptt = PushToTalkController()

    private init() {}

    func startIfNeeded() {
        guard !started, node == nil else { return }
        started = true

        let dataDir = FileManager.default
            .urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("walkietalkie", isDirectory: true)
        try? FileManager.default.createDirectory(at: dataDir, withIntermediateDirectories: true)

        let name = nickname.isEmpty ? UIDevice.current.name : nickname
        let version = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "0.1.0"

        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            guard let self else { return }
            let source = OpusMicSource()
            let sink = OpusSpeakerSink()
            var error: NSError?
            let startedNode = MobileStartNode(
                dataDir.path,
                name,
                "ios",
                version,
                source,
                sink,
                &error
            )
            Task { @MainActor in
                if let error {
                    self.started = false
                    self.statusMessage = "Failed to start: \(error.localizedDescription)"
                    return
                }
                guard let startedNode else {
                    self.started = false
                    self.statusMessage = "Failed to start: nil node"
                    return
                }
                self.micSource = source
                self.speakerSink = sink
                self.node = startedNode
                self.selfID = startedNode.selfID()
                self.baseStationURL = startedNode.baseStationURL()
                self.locationUpdater = LocationUpdater(node: startedNode)
                self.locationUpdater?.start()
                self.bleBridge = BlePresenceBridge(node: startedNode)
                self.bleBridge?.start()
                self.ptt.configure(node: startedNode, controller: self)
                self.ptt.requestJoin()
                self.startPolling()
                self.statusMessage = "Mesh started"
            }
        }
    }

    func startTalking() {
        node?.startTalking()
        isTalking = true
    }

    func stopTalking() {
        node?.stopTalking()
        isTalking = false
    }

    func updateNickname(_ name: String) {
        nickname = name
        NicknameStore.set(name)
        do {
            try node?.updateName(name)
        } catch {
            // best-effort
        }
    }

    func listChannelsJSON() -> String {
        var error: NSError?
        return node?.listChannelsJSON(&error) ?? "[]"
    }

    func listVoiceNotesJSON(withPeerID: String = "") -> String {
        var error: NSError?
        return node?.listVoiceNotesJSON(withPeerID, error: &error) ?? "[]"
    }

    /// Active remote talker name for PushToTalk system UI (best-effort from device list).
    func displayName(forPeerID peerID: String) -> String? {
        guard let data = devicesJSON.data(using: .utf8),
              let arr = try? JSONSerialization.jsonObject(with: data) as? [[String: Any]] else {
            return nil
        }
        return arr.first(where: { ($0["id"] as? String) == peerID })?["name"] as? String
    }

    private func startPolling() {
        pollTimer?.invalidate()
        pollTimer = Timer.scheduledTimer(withTimeInterval: 2.0, repeats: true) { [weak self] _ in
            Task { @MainActor in
                self?.refresh()
            }
        }
        refresh()
    }

    private func refresh() {
        guard let node else { return }
        var error: NSError?
        devicesJSON = node.listDevicesJSON(&error)
        baseStationURL = node.baseStationURL()
        selfID = node.selfID()
    }

    func stop() {
        pollTimer?.invalidate()
        pollTimer = nil
        locationUpdater?.stop()
        bleBridge?.stop()
        ptt.leave()
        do {
            try node?.stop()
        } catch {
            // best-effort teardown
        }
        node = nil
        micSource?.releaseResources()
        speakerSink?.releaseResources()
        micSource = nil
        speakerSink = nil
        started = false
    }
}

enum NicknameStore {
    private static let key = "walkietalkie.nickname"

    static func get() -> String {
        UserDefaults.standard.string(forKey: key) ?? ""
    }

    static func set(_ value: String) {
        UserDefaults.standard.set(value, forKey: key)
    }
}
