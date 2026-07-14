import AVFoundation
import CoreBluetooth
import CoreLocation
import Foundation

/// Requests mic / location / Bluetooth before the mesh starts — mirrors Android MainActivity.
@MainActor
final class PermissionCoordinator: NSObject, ObservableObject {
    static let shared = PermissionCoordinator()

    @Published private(set) var micGranted = false
    @Published private(set) var locationGranted = false
    @Published private(set) var bluetoothReady = false
    @Published private(set) var allGranted = false

    private let locationManager = CLLocationManager()
    private var central: CBCentralManager?
    private var continuation: CheckedContinuation<Void, Never>?

    private override init() {
        super.init()
        locationManager.delegate = self
    }

    func requestAll() async {
        await requestMic()
        await requestLocation()
        await requestBluetooth()
        refresh()
    }

    private func requestMic() async {
        let session = AVAudioSession.sharedInstance()
        do {
            try session.setCategory(.playAndRecord, mode: .voiceChat, options: [.defaultToSpeaker, .allowBluetooth])
            let granted = await withCheckedContinuation { (cont: CheckedContinuation<Bool, Never>) in
                session.requestRecordPermission { ok in cont.resume(returning: ok) }
            }
            micGranted = granted
        } catch {
            micGranted = false
        }
    }

    private func requestLocation() async {
        let status = locationManager.authorizationStatus
        if status == .notDetermined {
            await withCheckedContinuation { (cont: CheckedContinuation<Void, Never>) in
                continuation = cont
                locationManager.requestWhenInUseAuthorization()
            }
        }
        locationGranted = locationManager.authorizationStatus == .authorizedWhenInUse
            || locationManager.authorizationStatus == .authorizedAlways
    }

    private func requestBluetooth() async {
        // Instantiating CBCentralManager triggers the system Bluetooth permission prompt on iOS 13+.
        await withCheckedContinuation { (cont: CheckedContinuation<Void, Never>) in
            let manager = CBCentralManager(delegate: self, queue: .main)
            self.central = manager
            // Give the delegate one run-loop turn to report state.
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.4) {
                cont.resume()
            }
        }
        bluetoothReady = central?.state != .unauthorized
    }

    private func refresh() {
        allGranted = micGranted && locationGranted
    }
}

extension PermissionCoordinator: CLLocationManagerDelegate {
    nonisolated func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        Task { @MainActor in
            locationGranted = manager.authorizationStatus == .authorizedWhenInUse
                || manager.authorizationStatus == .authorizedAlways
            continuation?.resume()
            continuation = nil
            refresh()
        }
    }
}

extension PermissionCoordinator: CBCentralManagerDelegate {
    nonisolated func centralManagerDidUpdateState(_ central: CBCentralManager) {
        Task { @MainActor in
            bluetoothReady = central.state != .unauthorized
        }
    }
}
