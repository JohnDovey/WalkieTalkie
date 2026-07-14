import Foundation
import WatchConnectivity
import os.log

/// Relays Apple Watch Hold-to-Talk to the phone-hosted Go mesh.
/// Watch does not embed Core.xcframework — see docs/2026-07-14-phase5-wearables.md.
@MainActor
final class WatchConnectivityBridge: NSObject, ObservableObject {
    static let shared = WatchConnectivityBridge()

    @Published private(set) var lastWatchEvent: String = ""

    private let log = Logger(subsystem: "com.walkietalkie", category: "WatchBridge")
    private weak var node: NodeController?

    private override init() {
        super.init()
    }

    func activate(node: NodeController) {
        self.node = node
        guard WCSession.isSupported() else {
            log.info("WatchConnectivity unsupported on this device")
            return
        }
        let session = WCSession.default
        session.delegate = self
        session.activate()
    }

    func pushStatusToWatch() {
        guard WCSession.isSupported(), WCSession.default.isReachable else { return }
        let payload: [String: Any] = [
            "kind": "status",
            "peers": node?.devicesJSON ?? "[]",
            "baseStation": node?.baseStationURL ?? "",
            "talking": node?.isTalking ?? false,
        ]
        WCSession.default.sendMessage(payload, replyHandler: nil) { [weak self] error in
            Task { @MainActor in
                self?.log.error("status push failed: \(error.localizedDescription)")
            }
        }
    }
}

extension WatchConnectivityBridge: WCSessionDelegate {
    nonisolated func session(_ session: WCSession,
                             activationDidCompleteWith activationState: WCSessionActivationState,
                             error: Error?) {
        if let error {
            Logger(subsystem: "com.walkietalkie", category: "WatchBridge")
                .error("activation: \(error.localizedDescription)")
        }
    }

#if os(iOS)
    nonisolated func sessionDidBecomeInactive(_ session: WCSession) {}
    nonisolated func sessionDidDeactivate(_ session: WCSession) {
        WCSession.default.activate()
    }
#endif

    nonisolated func session(_ session: WCSession, didReceiveMessage message: [String: Any]) {
        let action = message["action"] as? String
        Task { @MainActor in
            lastWatchEvent = action ?? "unknown"
            switch action {
            case "startTalking":
                node?.startTalking()
            case "stopTalking":
                node?.stopTalking()
            default:
                log.info("ignored watch message: \(String(describing: action))")
            }
        }
    }
}
