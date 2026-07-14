import Foundation
import PushToTalk
import Core
import os.log
import UIKit

/// Maps system PushToTalk channel transmit begin/end to Go StartTalking/StopTalking.
/// Foreground Hold-to-Talk in the UI remains the Simulator / unsigned-build fallback.
@MainActor
final class PushToTalkController: NSObject {
    private var channelManager: PTChannelManager?
    private var channelDescriptor: PTChannelDescriptor?
    private weak var node: MobileNode?
    private weak var controller: NodeController?
    private let log = Logger(subsystem: "com.walkietalkie", category: "PTT")
    private let channelUUID = UUID(uuidString: "a1b2c3d4-e5f6-7890-abcd-ef1234567890")!

    func configure(node: MobileNode, controller: NodeController) {
        self.node = node
        self.controller = controller
        PTChannelManager.channelManager(delegate: self, restorationDelegate: self) { [weak self] manager, error in
            Task { @MainActor in
                if let error {
                    self?.log.error("PTChannelManager unavailable: \(error.localizedDescription) — use Hold to Talk")
                    return
                }
                self?.channelManager = manager
                self?.log.info("PTChannelManager ready")
            }
        }
    }

    func requestJoin() {
        guard let channelManager else { return }
        let name = "WalkieTalkie"
        let image = UIImage(systemName: "dot.radiowaves.left.and.right")
        let descriptor = PTChannelDescriptor(name: name, image: image)
        channelDescriptor = descriptor
        channelManager.requestJoinChannel(channelUUID: channelUUID, descriptor: descriptor)
    }

    func leave() {
        channelManager?.leaveChannel(channelUUID: channelUUID)
        channelManager = nil
    }

    /// Optional: show active remote transmitter in system UI when we know who is talking.
    func setActiveRemoteParticipant(name: String?) {
        guard let channelManager else { return }
        if let name {
            let participant = PTParticipant(name: name, image: nil)
            channelManager.setActiveRemoteParticipant(participant, channelUUID: channelUUID, completionHandler: nil)
        } else {
            channelManager.setActiveRemoteParticipant(nil, channelUUID: channelUUID, completionHandler: nil)
        }
    }
}

extension PushToTalkController: PTChannelManagerDelegate {
    nonisolated func channelManager(_ channelManager: PTChannelManager,
                                    didJoinChannel channelUUID: UUID,
                                    reason: PTChannelJoinReason) {
        Logger(subsystem: "com.walkietalkie", category: "PTT").info("joined PTT channel")
    }

    nonisolated func channelManager(_ channelManager: PTChannelManager,
                                    didLeaveChannel channelUUID: UUID,
                                    reason: PTChannelLeaveReason) {
        Logger(subsystem: "com.walkietalkie", category: "PTT").info("left PTT channel")
    }

    nonisolated func channelManager(_ channelManager: PTChannelManager,
                                    channelUUID: UUID,
                                    didBeginTransmittingFrom source: PTChannelTransmitRequestSource) {
        Task { @MainActor in
            controller?.startTalking()
        }
    }

    nonisolated func channelManager(_ channelManager: PTChannelManager,
                                    channelUUID: UUID,
                                    didEndTransmittingFrom source: PTChannelTransmitRequestSource) {
        Task { @MainActor in
            controller?.stopTalking()
        }
    }

    nonisolated func channelManager(_ channelManager: PTChannelManager,
                                    receivedEphemeralPushToken pushToken: Data) {
        // No Apple Push relay yet — mesh is LAN/WebRTC.
    }

    nonisolated func incomingPushResult(channelManager: PTChannelManager,
                                        channelUUID: UUID,
                                        pushPayload: [String: Any]) -> PTPushResult {
        .activeRemoteParticipant(PTParticipant(name: "Peer", image: nil))
    }

    nonisolated func channelManager(_ channelManager: PTChannelManager,
                                    didActivate audioSession: AVAudioSession) {}

    nonisolated func channelManager(_ channelManager: PTChannelManager,
                                    didDeactivate audioSession: AVAudioSession) {}
}

extension PushToTalkController: PTChannelRestorationDelegate {
    nonisolated func channelDescriptor(restoredChannelUUID channelUUID: UUID) -> PTChannelDescriptor {
        PTChannelDescriptor(name: "WalkieTalkie", image: UIImage(systemName: "dot.radiowaves.left.and.right"))
    }
}

import AVFoundation
