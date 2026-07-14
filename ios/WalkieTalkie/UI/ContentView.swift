import SwiftUI
import AVFoundation

enum AppScreen: Equatable, Hashable {
    case devices
    case settings
    case about
    case voiceThread(peerId: String, peerName: String)
    case recordVoice(peerId: String, peerName: String, channelId: String)
    case privateChannel(channelId: String, peerId: String, peerName: String, status: String)
}

struct ContentView: View {
    @EnvironmentObject var node: NodeController
    @StateObject private var permissions = PermissionCoordinator.shared
    @State private var screen: AppScreen = .devices
    @State private var showChats = false
    @State private var permissionsDone = false

    private var channels: [ChannelRow] { ChannelRow.parse(node.channelsJSON) }
    private var inboxByFrom: [String: Int] { VoiceNoteRow.countQueuedInbox(node.inboxJSON, selfId: node.selfID) }
    private var drawerUnread: Int { channels.reduce(0) { $0 + $1.unread } + inboxByFrom.values.reduce(0, +) }

    var body: some View {
        Group {
            if !permissionsDone {
                PermissionGateView {
                    permissionsDone = true
                    node.startIfNeeded()
                }
            } else {
                mainTabs
            }
        }
        .task {
            await permissions.requestAll()
        }
    }

    private var mainTabs: some View {
        TabView(selection: tabBinding) {
            devicesStack
                .tabItem { Label("Devices", systemImage: "antenna.radiowaves.left.and.right") }
                .tag(AppScreen.devices)
            SettingsView()
                .tabItem { Label("Settings", systemImage: "gear") }
                .tag(AppScreen.settings)
            AboutView()
                .tabItem { Label("About", systemImage: "info.circle") }
                .tag(AppScreen.about)
        }
        .safeAreaInset(edge: .top, spacing: 0) {
            HStack {
                Button {
                    showChats = true
                } label: {
                    Text(drawerUnread > 0 ? "Chats (\(drawerUnread))" : "Chats")
                }
                Spacer()
            }
            .padding(.horizontal)
            .padding(.vertical, 6)
            .background(.bar)
        }
        .sheet(isPresented: $showChats) {
            ChatsDrawerView(
                channels: channels,
                inboxByFrom: inboxByFrom,
                devices: DeviceRow.parse(node.devicesJSON),
                onOpenChannel: { ch in
                    showChats = false
                    screen = .privateChannel(channelId: ch.id, peerId: ch.peerId, peerName: ch.peerName, status: ch.status)
                },
                onOpenThread: { id, name in
                    showChats = false
                    screen = .voiceThread(peerId: id, peerName: name)
                }
            )
            .environmentObject(node)
        }
    }

    private var tabBinding: Binding<AppScreen> {
        Binding(
            get: {
                switch screen {
                case .settings: return .settings
                case .about: return .about
                default: return .devices
                }
            },
            set: { screen = $0 }
        )
    }

    @ViewBuilder
    private var devicesStack: some View {
        NavigationStack {
            switch screen {
            case .devices, .settings, .about:
                DevicesView(
                    onVoiceMessage: { id, name in screen = .recordVoice(peerId: id, peerName: name, channelId: "") },
                    onInvite: { id, name in
                        Task.detached {
                            let json = await MainActor.run { node.inviteChannel(toDeviceID: id) }
                            guard let json, let data = json.data(using: .utf8),
                                  let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                                  let chId = obj["id"] as? String else { return }
                            let status = obj["status"] as? String ?? "pending"
                            await MainActor.run {
                                screen = .privateChannel(channelId: chId, peerId: id, peerName: name, status: status)
                            }
                        }
                    },
                    onOpenThread: { id, name in screen = .voiceThread(peerId: id, peerName: name) }
                )
            case .voiceThread(let peerId, let peerName):
                VoiceThreadView(peerId: peerId, peerName: peerName, onBack: { screen = .devices }, onReply: {
                    screen = .recordVoice(peerId: peerId, peerName: peerName, channelId: "")
                })
            case .recordVoice(let peerId, let peerName, let channelId):
                RecordVoiceView(peerId: peerId, peerName: peerName, channelId: channelId, onDone: {
                    if channelId.isEmpty {
                        screen = .voiceThread(peerId: peerId, peerName: peerName)
                    } else {
                        screen = .privateChannel(channelId: channelId, peerId: peerId, peerName: peerName, status: "active")
                    }
                }, onCancel: { screen = .devices })
            case .privateChannel(let channelId, let peerId, let peerName, let status):
                PrivateChannelView(channelId: channelId, peerId: peerId, peerName: peerName, status: status, onBack: { screen = .devices })
            }
        }
    }
}

struct PermissionGateView: View {
    @StateObject private var permissions = PermissionCoordinator.shared
    let onReady: () -> Void

    var body: some View {
        VStack(spacing: 16) {
            Text("WalkieTalkie").font(.largeTitle.bold())
            Text("Mic, location, and Bluetooth are needed for Talk, GPS, and nearby presence.")
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: 8) {
                Label(permissions.micGranted ? "Microphone OK" : "Microphone…", systemImage: permissions.micGranted ? "checkmark.circle.fill" : "circle")
                Label(permissions.locationGranted ? "Location OK" : "Location…", systemImage: permissions.locationGranted ? "checkmark.circle.fill" : "circle")
                Label(permissions.bluetoothReady ? "Bluetooth OK" : "Bluetooth…", systemImage: permissions.bluetoothReady ? "checkmark.circle.fill" : "circle")
            }
            Button("Continue") { onReady() }
                .buttonStyle(.borderedProminent)
                .disabled(!permissions.micGranted)
            if permissions.micGranted && !permissions.locationGranted {
                Text("Location optional but recommended for the map.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .padding()
    }
}

struct DevicesView: View {
    @EnvironmentObject var node: NodeController
    var onVoiceMessage: (String, String) -> Void
    var onInvite: (String, String) -> Void
    var onOpenThread: (String, String) -> Void

    private var devices: [DeviceRow] { DeviceRow.parse(node.devicesJSON) }
    private var inboxByFrom: [String: Int] { VoiceNoteRow.countQueuedInbox(node.inboxJSON, selfId: node.selfID) }

    var body: some View {
        VStack(spacing: 12) {
            if !node.statusMessage.isEmpty {
                Text(node.statusMessage).font(.caption).foregroundStyle(.secondary)
            }
            List(devices.filter { $0.id != node.selfID }) { device in
                    HStack {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(device.name)
                                .font(.headline)
                                .foregroundStyle(device.isConnected ? Color.green : Color.secondary)
                            Text(device.status).font(.caption).foregroundStyle(.secondary)
                            HStack {
                                Text(device.platform)
                                if device.discoveryMethods.contains("ble") {
                                    Text("BLE").foregroundStyle(.orange)
                                }
                                if let n = inboxByFrom[device.id], n > 0 {
                                    Text("VM \(n)").foregroundStyle(.red)
                                }
                            }
                            .font(.caption)
                        }
                        Spacer()
                        Button("VM") { onVoiceMessage(device.id, device.name) }
                            .buttonStyle(.bordered)
                        if device.isConnected {
                            Button("Chat") { onInvite(device.id, device.name) }
                                .buttonStyle(.bordered)
                        }
                        if let n = inboxByFrom[device.id], n > 0 {
                            Button("Inbox") { onOpenThread(device.id, device.name) }
                                .buttonStyle(.borderedProminent)
                        }
                    }
            }
            HoldToTalkButton()
                .padding(.bottom, 16)
        }
        .navigationTitle("Devices")
    }
}

struct HoldToTalkButton: View {
    @EnvironmentObject var node: NodeController

    var body: some View {
        Text(node.isTalking ? "Talking…" : "Hold to Talk")
            .font(.headline)
            .foregroundStyle(.white)
            .frame(maxWidth: .infinity)
            .padding(.vertical, 20)
            .background(node.isTalking ? Color.red : Color.accentColor)
            .clipShape(RoundedRectangle(cornerRadius: 12))
            .padding(.horizontal)
            .gesture(
                DragGesture(minimumDistance: 0)
                    .onChanged { _ in if !node.isTalking { node.startTalking() } }
                    .onEnded { _ in node.stopTalking() }
            )
            .accessibilityLabel("Hold to Talk")
    }
}

struct SettingsView: View {
    @EnvironmentObject var node: NodeController
    @State private var draft = ""

    var body: some View {
        NavigationStack {
            Form {
                Section("Nickname") {
                    TextField("Display name", text: $draft)
                    Button("Save") {
                        node.updateNickname(draft.trimmingCharacters(in: .whitespacesAndNewlines))
                    }
                }
                Section("Mesh") {
                    LabeledContent("Self ID", value: node.selfID)
                    LabeledContent("Base Station", value: node.baseStationURL.isEmpty ? "—" : node.baseStationURL)
                }
            }
            .navigationTitle("Settings")
            .onAppear { draft = node.nickname }
        }
    }
}

struct AboutView: View {
    @EnvironmentObject var node: NodeController

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    Text("WalkieTalkie").font(.largeTitle.bold())
                    Text("Version \(Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "?")")
                    Text("Platform: ios")
                    Text("Device ID: \(node.selfID)").font(.caption.monospaced())
                    if let url = URL(string: node.baseStationURL), !node.baseStationURL.isEmpty {
                        Link("Base Station: \(node.baseStationURL)", destination: url)
                            .font(.caption)
                    } else {
                        Text("Base Station: not discovered yet")
                            .font(.caption)
                    }

                    Text("What it is")
                        .font(.headline)
                        .padding(.top, 8)
                    Text(
                        "WalkieTalkie is a LAN push-to-talk mesh. Hold Talk and other devices on the " +
                        "same Wi‑Fi hear you live — no accounts, no manual pairing. This iPhone app " +
                        "joins the same mesh as Android, Wear OS, Apple Watch (relayed through this " +
                        "phone), and a desktop Base Station."
                    )
                    .foregroundStyle(.secondary)

                    Text("How discovery works")
                        .font(.headline)
                        .padding(.top, 4)
                    Text(
                        "Peers find each other with mDNS over Wi‑Fi. Off the LAN, nearby phones and " +
                        "watches can still appear over Bluetooth LE as presence-only (id and name, " +
                        "no live audio until you're back on Wi‑Fi together)."
                    )
                    .foregroundStyle(.secondary)

                    Text("The Base Station")
                        .font(.headline)
                        .padding(.top, 4)
                    Text(
                        "The Base Station is the desktop companion: web dashboard, mesh hub, and " +
                        "store-and-forward for voice notes and private channels. Tap the Base Station " +
                        "URL above to open its web UI in Safari. Group Hold-to-talk works peer-to-peer " +
                        "without one; voice messages and private chats need a Base Station on the LAN."
                    )
                    .foregroundStyle(.secondary)
                }
                .padding()
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .navigationTitle("About")
        }
    }
}

struct ChatsDrawerView: View {
    @EnvironmentObject var node: NodeController
    @Environment(\.dismiss) private var dismiss
    let channels: [ChannelRow]
    let inboxByFrom: [String: Int]
    let devices: [DeviceRow]
    var onOpenChannel: (ChannelRow) -> Void
    var onOpenThread: (String, String) -> Void

    var body: some View {
        NavigationStack {
            List {
                Section("Private channels") {
                    if channels.isEmpty {
                        Text("No private channels").foregroundStyle(.secondary)
                    } else {
                        ForEach(channels) { ch in
                            Button {
                                onOpenChannel(ch)
                                dismiss()
                            } label: {
                                HStack {
                                    Text(ch.peerName.isEmpty ? ch.peerId : ch.peerName)
                                    if ch.status == "pending" { Text("(pending)").foregroundStyle(.secondary) }
                                    Spacer()
                                    if ch.unread > 0 {
                                        Text("\(ch.unread)")
                                            .padding(.horizontal, 8)
                                            .padding(.vertical, 2)
                                            .background(Circle().fill(Color.red))
                                            .foregroundStyle(.white)
                                    }
                                }
                            }
                        }
                    }
                }
                Section("Voice messages") {
                    if inboxByFrom.isEmpty {
                        Text("No new voice messages").foregroundStyle(.secondary)
                    } else {
                        ForEach(Array(inboxByFrom.keys), id: \.self) { fromId in
                            let name = devices.first(where: { $0.id == fromId })?.name ?? fromId
                            let count = inboxByFrom[fromId] ?? 0
                            Button {
                                onOpenThread(fromId, name)
                                dismiss()
                            } label: {
                                HStack {
                                    Text(name)
                                    Spacer()
                                    Text("\(count)")
                                        .padding(.horizontal, 8)
                                        .padding(.vertical, 2)
                                        .background(Circle().fill(Color.red))
                                        .foregroundStyle(.white)
                                }
                            }
                        }
                    }
                }
            }
            .navigationTitle("Chats")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
    }
}

struct VoiceThreadView: View {
    @EnvironmentObject var node: NodeController
    let peerId: String
    let peerName: String
    var onBack: () -> Void
    var onReply: () -> Void
    @State private var notesJSON = "[]"
    @State private var status = ""
    private let player = VoiceNotePlayer()

    var body: some View {
        VStack {
            List(VoiceNoteRow.parse(notesJSON)) { n in
                HStack {
                    Text("\(n.fromId == node.selfID ? "You" : peerName) · \(n.status)")
                    Spacer()
                    Button("Play") {
                        Task {
                            guard let bytes = node.downloadVoiceNote(noteID: n.id) else {
                                status = "Download failed"
                                return
                            }
                            node.ackVoiceNote(noteID: n.id)
                            do {
                                try player.play(bytes)
                                status = "Playing"
                            } catch {
                                status = "Play failed: \(error.localizedDescription)"
                            }
                        }
                    }
                    Button("Del") {
                        node.deleteVoiceNote(noteID: n.id)
                        notesJSON = node.listVoiceNotesJSON(withPeerID: peerId)
                    }
                }
            }
            if !status.isEmpty { Text(status).font(.caption).foregroundStyle(.secondary) }
            Button("Reply", action: onReply).buttonStyle(.borderedProminent).padding()
        }
        .navigationTitle(peerName)
        .navigationBarBackButtonHidden(true)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) { Button("← Devices") { onBack() } }
        }
        .task {
            while !Task.isCancelled {
                notesJSON = node.listVoiceNotesJSON(withPeerID: peerId)
                try? await Task.sleep(nanoseconds: 2_000_000_000)
            }
        }
        .onDisappear { player.stop() }
    }
}

struct RecordVoiceView: View {
    @EnvironmentObject var node: NodeController
    let peerId: String
    let peerName: String
    let channelId: String
    var onDone: () -> Void
    var onCancel: () -> Void

    @State private var recording = false
    @State private var status = "Press Start to record."
    private let recorder = ClipRecorder()

    var body: some View {
        VStack(spacing: 20) {
            Text("Voice Message").font(.title2.bold())
            Text("To: \(peerName)")
            Text(status).foregroundStyle(.secondary)
            HStack(spacing: 12) {
                Button("Start") {
                    do {
                        try recorder.start()
                        recording = true
                        status = "Recording…"
                    } catch {
                        status = "Mic error: \(error.localizedDescription)"
                    }
                }
                .disabled(recording)
                Button("Stop & Send") {
                    Task {
                        do {
                            let bytes = try recorder.stopAndRead()
                            recording = false
                            status = "Sending…"
                            let err = channelId.isEmpty
                                ? node.sendVoiceNote(toDeviceID: peerId, audio: bytes)
                                : node.sendChannelClip(channelID: channelId, audio: bytes)
                            if let err {
                                status = "Send failed: \(err)"
                            } else {
                                status = "Sent."
                                onDone()
                            }
                        } catch {
                            recording = false
                            status = "Error: \(error.localizedDescription)"
                        }
                    }
                }
                .disabled(!recording)
                Button("Cancel") {
                    recorder.cancel()
                    recording = false
                    onCancel()
                }
            }
        }
        .padding()
        .navigationBarBackButtonHidden(true)
        .onDisappear { recorder.cancel() }
    }
}

struct PrivateChannelView: View {
    @EnvironmentObject var node: NodeController
    let channelId: String
    let peerId: String
    let peerName: String
    let status: String
    var onBack: () -> Void

    @State private var notesJSON = "[]"
    @State private var localStatus = ""
    @State private var liveMesh = false
    @State private var peerFocused = false
    private let recorder = ClipRecorder()
    private let player = VoiceNotePlayer()
    @State private var recording = false

    private var modeLabel: String {
        if liveMesh && peerFocused { return "Mode: live mesh (peer is here)" }
        if liveMesh { return "Mode: live mesh" }
        return "Mode: clip via Base Station"
    }

    var body: some View {
        VStack {
            Text("Private: \(peerName)").font(.headline)
            Text(modeLabel)
                .font(.caption)
                .foregroundStyle(liveMesh ? Color.green : .secondary)
            List(VoiceNoteRow.parse(notesJSON)) { n in
                HStack {
                    Text("\(n.fromId == node.selfID ? "You" : peerName) · \(n.status)")
                    Spacer()
                    Button("Play") {
                        if let bytes = node.downloadVoiceNote(noteID: n.id) {
                            node.ackVoiceNote(noteID: n.id)
                            try? player.play(bytes)
                        }
                    }
                }
            }
            Text(recording || node.isTalking
                 ? (liveMesh ? "Live…" : "Recording clip…")
                 : (liveMesh ? "Hold for live talk" : "Hold to record clip"))
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(recording || node.isTalking ? "Talking…" : "Hold to Talk (channel)")
                .font(.headline)
                .foregroundStyle(.white)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 28)
                .background((recording || node.isTalking) ? Color.red : Color.green)
                .clipShape(Circle())
                .padding(24)
                .gesture(
                    DragGesture(minimumDistance: 0)
                        .onChanged { _ in
                            if liveMesh {
                                guard !node.isTalking else { return }
                                node.startTalkingTo(peerID: peerId)
                                return
                            }
                            guard !recording else { return }
                            do {
                                try recorder.start()
                                recording = true
                            } catch {
                                localStatus = error.localizedDescription
                            }
                        }
                        .onEnded { _ in
                            if node.isTalking {
                                node.stopTalking()
                                return
                            }
                            guard recording else { return }
                            recording = false
                            Task {
                                do {
                                    let bytes = try recorder.stopAndRead()
                                    if let err = node.sendChannelClip(channelID: channelId, audio: bytes) {
                                        localStatus = err
                                    }
                                } catch {
                                    localStatus = error.localizedDescription
                                    recorder.cancel()
                                }
                            }
                        }
                )
            if !localStatus.isEmpty {
                Text(localStatus).font(.caption).foregroundStyle(.red)
            }
        }
        .navigationBarBackButtonHidden(true)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) { Button("← Devices") { onBack() } }
        }
        .onAppear {
            if status == "pending" { node.acceptChannel(channelID: channelId) }
            node.focusChannel(channelID: channelId)
        }
        .onDisappear {
            node.stopTalking()
            node.blurChannel(channelID: channelId)
            recorder.cancel()
            player.stop()
        }
        .task {
            while !Task.isCancelled {
                notesJSON = node.listChannelNotesJSON(channelID: channelId)
                liveMesh = node.isDirectlyConnected(peerID: peerId)
                peerFocused = ChannelRow.peerFocused(
                    in: node.channelsJSON,
                    channelID: channelId,
                    peerID: peerId
                )
                try? await Task.sleep(nanoseconds: 1_500_000_000)
            }
        }
    }
}
