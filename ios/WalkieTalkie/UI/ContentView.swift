import SwiftUI

struct ContentView: View {
    @EnvironmentObject var node: NodeController
    @State private var showChats = false

    var body: some View {
        TabView {
            DevicesView()
                .tabItem { Label("Devices", systemImage: "antenna.radiowaves.left.and.right") }
            SettingsView()
                .tabItem { Label("Settings", systemImage: "gear") }
            AboutView()
                .tabItem { Label("About", systemImage: "info.circle") }
        }
        .sheet(isPresented: $showChats) {
            ChatsDrawerView()
                .environmentObject(node)
        }
        .overlay(alignment: .topTrailing) {
            Button {
                showChats = true
            } label: {
                Image(systemName: "bubble.left.and.bubble.right")
                    .padding(12)
            }
            .padding(.top, 4)
            .padding(.trailing, 8)
        }
    }
}

struct DevicesView: View {
    @EnvironmentObject var node: NodeController
    @State private var devices: [DeviceRow] = []

    var body: some View {
        NavigationStack {
            VStack(spacing: 16) {
                if !node.statusMessage.isEmpty {
                    Text(node.statusMessage)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                List(devices) { device in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(device.name).font(.headline)
                        Text(device.id).font(.caption2).foregroundStyle(.secondary)
                        HStack {
                            Text(device.platform)
                            if device.discoveryMethods.contains("ble") {
                                Text("BLE").foregroundStyle(.orange)
                            }
                        }
                        .font(.caption)
                    }
                }
                HoldToTalkButton()
                    .padding(.bottom, 24)
            }
            .navigationTitle("Devices")
            .onAppear { reload() }
            .onChange(of: node.devicesJSON) { _ in reload() }
        }
    }

    private func reload() {
        devices = DeviceRow.parse(node.devicesJSON)
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
                    .onChanged { _ in
                        if !node.isTalking { node.startTalking() }
                    }
                    .onEnded { _ in
                        node.stopTalking()
                    }
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
    var body: some View {
        NavigationStack {
            VStack(alignment: .leading, spacing: 12) {
                Text("WalkieTalkie").font(.largeTitle.bold())
                Text("Version \(Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "?")")
                Text("LAN push-to-talk mesh. iOS shell over the shared Go core.")
                    .foregroundStyle(.secondary)
                Spacer()
            }
            .padding()
            .frame(maxWidth: .infinity, alignment: .leading)
            .navigationTitle("About")
        }
    }
}

struct ChatsDrawerView: View {
    @EnvironmentObject var node: NodeController
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                Section("Private channels") {
                    Text(node.listChannelsJSON())
                        .font(.caption.monospaced())
                }
                Section("Voice notes") {
                    Text(node.listVoiceNotesJSON())
                        .font(.caption.monospaced())
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

struct DeviceRow: Identifiable {
    let id: String
    let name: String
    let platform: String
    let discoveryMethods: [String]

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
                discoveryMethods: dict["discoveryMethods"] as? [String] ?? []
            )
        }
    }
}
