import SwiftUI
import Core

@main
struct WalkieTalkieApp: App {
    @StateObject private var node = NodeController.shared

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(node)
                .onAppear {
                    node.startIfNeeded()
                }
        }
    }
}
