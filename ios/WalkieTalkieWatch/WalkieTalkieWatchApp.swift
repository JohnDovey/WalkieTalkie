import SwiftUI
import WatchConnectivity

@main
struct WalkieTalkieWatchApp: App {
    @StateObject private var session = WatchTalkSession.shared

    var body: some Scene {
        WindowGroup {
            WatchContentView()
                .environmentObject(session)
        }
    }
}
