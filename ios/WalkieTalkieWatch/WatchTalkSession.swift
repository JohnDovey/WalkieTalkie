import Foundation
import WatchConnectivity
import Combine

/// Thin WatchConnectivity client — phone owns the mesh node.
@MainActor
final class WatchTalkSession: NSObject, ObservableObject {
    static let shared = WatchTalkSession()

    @Published var statusLine: String = "Open iPhone app"

    private override init() {
        super.init()
        guard WCSession.isSupported() else {
            statusLine = "WC unavailable"
            return
        }
        WCSession.default.delegate = self
        WCSession.default.activate()
    }

    func startTalking() {
        send(action: "startTalking")
    }

    func stopTalking() {
        send(action: "stopTalking")
    }

    private func send(action: String) {
        guard WCSession.default.isReachable else {
            statusLine = "iPhone unreachable"
            return
        }
        WCSession.default.sendMessage(["action": action], replyHandler: nil) { [weak self] error in
            Task { @MainActor in
                self?.statusLine = error.localizedDescription
            }
        }
    }
}

extension WatchTalkSession: WCSessionDelegate {
    nonisolated func session(_ session: WCSession,
                             activationDidCompleteWith activationState: WCSessionActivationState,
                             error: Error?) {
        Task { @MainActor in
            if activationState == .activated {
                statusLine = session.isCompanionAppInstalled ? "Ready" : "Install phone app"
            } else if let error {
                statusLine = error.localizedDescription
            }
        }
    }

    nonisolated func session(_ session: WCSession, didReceiveMessage message: [String: Any]) {
        Task { @MainActor in
            if let kind = message["kind"] as? String, kind == "status" {
                let talking = message["talking"] as? Bool ?? false
                let bs = message["baseStation"] as? String ?? ""
                statusLine = talking ? "Phone transmitting" : (bs.isEmpty ? "Mesh up" : "BS linked")
            }
        }
    }
}
