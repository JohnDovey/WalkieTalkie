import Foundation
import Combine
import Network
import UIKit
import Core

/// Lifecycle host for the Go `Mobile.startNode` mesh — mirrors Android's PTTService.
@MainActor
final class NodeController: ObservableObject {
    static let shared = NodeController()

    @Published private(set) var devicesJSON: String = "[]"
    @Published private(set) var channelsJSON: String = "[]"
    @Published private(set) var inboxJSON: String = "[]"
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
    private let pathMonitor = NWPathMonitor()
    private let pathQueue = DispatchQueue(label: "com.walkietalkie.path")
    private var pathStarted = false
    private var hadWifi = false
    private var everLostWifi = false
    private var restartWorkItem: DispatchWorkItem?

    private init() {}

    func startIfNeeded() {
        guard !started, node == nil else { return }
        started = true
        startPathMonitorIfNeeded()
        bootNode()
    }

    private func bootNode() {
        let dataDir = FileManager.default
            .urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("walkietalkie", isDirectory: true)
        try? FileManager.default.createDirectory(at: dataDir, withIntermediateDirectories: true)

        let name = nickname.isEmpty ? UIDevice.current.name : nickname
        let version = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "0.2.0"

        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            guard let self else { return }
            let source = OpusMicSource()
            let sink = OpusSpeakerSink()
            var error: NSError?
            let startedNode = MobileStartNode(
                dataDir.path, name, "ios", version, source, sink, &error
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

    /// Soft restart after Wi-Fi returns — same rationale as Android PTTService.
    private func restartNode() {
        statusMessage = "Wi-Fi restored — restarting mesh…"
        stopNodeOnly()
        started = true
        bootNode()
    }

    private func startPathMonitorIfNeeded() {
        guard !pathStarted else { return }
        pathStarted = true
        pathMonitor.pathUpdateHandler = { [weak self] path in
            let wifi = path.status == .satisfied && path.usesInterfaceType(.wifi)
            Task { @MainActor in
                guard let self else { return }
                if wifi {
                    if self.everLostWifi && !self.hadWifi && self.node != nil {
                        self.restartWorkItem?.cancel()
                        let work = DispatchWorkItem { [weak self] in self?.restartNode() }
                        self.restartWorkItem = work
                        DispatchQueue.main.asyncAfter(deadline: .now() + 2, execute: work)
                    }
                    self.hadWifi = true
                } else {
                    if self.hadWifi { self.everLostWifi = true }
                    self.hadWifi = false
                }
            }
        }
        pathMonitor.start(queue: pathQueue)
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
        try? node?.updateName(name)
    }

    // MARK: - Voice notes / private channels

    /// Returns nil on success, error message on failure (Android-style).
    func sendVoiceNote(toDeviceID: String, audio: Data) -> String? {
        do {
            try node?.sendVoiceNote(toDeviceID, audio: audio)
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    func sendChannelClip(channelID: String, audio: Data) -> String? {
        do {
            try node?.sendChannelClip(channelID, audio: audio)
            return nil
        } catch {
            return error.localizedDescription
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

    func listChannelNotesJSON(channelID: String) -> String {
        var error: NSError?
        return node?.listChannelNotesJSON(channelID, error: &error) ?? "[]"
    }

    func downloadVoiceNote(noteID: String) -> Data? {
        try? node?.downloadVoiceNote(noteID)
    }

    func ackVoiceNote(noteID: String) {
        try? node?.ackVoiceNote(noteID)
    }

    func deleteVoiceNote(noteID: String) {
        try? node?.deleteVoiceNote(noteID)
    }

    func inviteChannel(toDeviceID: String) -> String? {
        var error: NSError?
        let json = node?.inviteChannel(toDeviceID, error: &error)
        return (error == nil) ? json : nil
    }

    func acceptChannel(channelID: String) {
        try? node?.acceptChannel(channelID)
    }

    func focusChannel(channelID: String) {
        try? node?.focusChannel(channelID)
    }

    func blurChannel(channelID: String) {
        try? node?.blurChannel(channelID)
    }

    func closeChannel(channelID: String) {
        try? node?.closeChannel(channelID)
    }

    func displayName(forPeerID peerID: String) -> String? {
        DeviceRow.parse(devicesJSON).first(where: { $0.id == peerID })?.name
    }

    private func startPolling() {
        pollTimer?.invalidate()
        pollTimer = Timer.scheduledTimer(withTimeInterval: 2.0, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.refresh() }
        }
        refresh()
    }

    private func refresh() {
        guard let node else { return }
        var error: NSError?
        devicesJSON = node.listDevicesJSON(&error)
        channelsJSON = node.listChannelsJSON(&error)
        inboxJSON = node.listVoiceNotesJSON("", error: &error)
        baseStationURL = node.baseStationURL()
        selfID = node.selfID()
    }

    private func stopNodeOnly() {
        pollTimer?.invalidate()
        pollTimer = nil
        locationUpdater?.stop()
        bleBridge?.stop()
        ptt.leave()
        try? node?.stop()
        node = nil
        micSource?.releaseResources()
        speakerSink?.releaseResources()
        micSource = nil
        speakerSink = nil
    }

    func stop() {
        restartWorkItem?.cancel()
        stopNodeOnly()
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
