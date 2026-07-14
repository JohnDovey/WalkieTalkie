import CoreLocation
import Foundation
import Core
import os.log

/// Feeds GPS into Go Node — mirrors Android LocationUpdater.
final class LocationUpdater: NSObject, CLLocationManagerDelegate {
    private let node: MobileNode
    private let manager = CLLocationManager()
    private let log = Logger(subsystem: "com.walkietalkie", category: "Location")

    init(node: MobileNode) {
        self.node = node
        super.init()
        manager.delegate = self
        manager.desiredAccuracy = kCLLocationAccuracyHundredMeters
        manager.distanceFilter = 25
    }

    func start() {
        switch manager.authorizationStatus {
        case .notDetermined:
            manager.requestWhenInUseAuthorization()
        case .authorizedAlways, .authorizedWhenInUse:
            manager.startUpdatingLocation()
        default:
            log.warning("location permission denied")
        }
    }

    func stop() {
        manager.stopUpdatingLocation()
    }

    func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        if manager.authorizationStatus == .authorizedWhenInUse ||
            manager.authorizationStatus == .authorizedAlways {
            manager.startUpdatingLocation()
        }
    }

    func locationManager(_ manager: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
        guard let loc = locations.last else { return }
        do {
            try node.updateLocation(loc.coordinate.latitude, lon: loc.coordinate.longitude, accuracy: loc.horizontalAccuracy)
        } catch {
            log.error("updateLocation failed: \(error.localizedDescription)")
        }
    }
}
