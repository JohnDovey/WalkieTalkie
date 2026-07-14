import CoreBluetooth
import Foundation
import Core
import os.log

/// Off-LAN presence via BLE — mirrors Android BlePresenceBridge.
/// Fixed service UUID in advertisement; device UUID in manufacturer data (0xFFFF).
final class BlePresenceBridge: NSObject {
    static let serviceUUID = CBUUID(string: "6e7a1b2c-6f6f-4a1a-9f2a-8f3b0d6c9a11")
    static let manufacturerID: UInt16 = 0xFFFF

    private let node: MobileNode
    private var peripheralManager: CBPeripheralManager?
    private var centralManager: CBCentralManager?
    private let log = Logger(subsystem: "com.walkietalkie", category: "BLE")

    init(node: MobileNode) {
        self.node = node
        super.init()
    }

    func start() {
        peripheralManager = CBPeripheralManager(delegate: self, queue: .main)
        centralManager = CBCentralManager(delegate: self, queue: .main)
    }

    func stop() {
        peripheralManager?.stopAdvertising()
        centralManager?.stopScan()
        peripheralManager = nil
        centralManager = nil
    }

    private func startAdvertisingIfReady() {
        guard let pm = peripheralManager, pm.state == .poweredOn else { return }
        let selfID = node.selfID()
        guard let uuid = UUID(uuidString: selfID) else { return }
        var bytes = uuidToBytes(uuid)
        var payload = Data()
        var company = Self.manufacturerID.littleEndian
        withUnsafeBytes(of: &company) { payload.append(contentsOf: $0) }
        payload.append(bytes)

        pm.startAdvertising([
            CBAdvertisementDataServiceUUIDsKey: [Self.serviceUUID],
            CBAdvertisementDataManufacturerDataKey: payload,
        ])
        log.info("BLE advertising started")
    }

    private func startScanningIfReady() {
        guard let cm = centralManager, cm.state == .poweredOn else { return }
        cm.scanForPeripherals(withServices: [Self.serviceUUID], options: [
            CBCentralManagerScanOptionAllowDuplicatesKey: true,
        ])
        log.info("BLE scanning started")
    }

    private func uuidToBytes(_ uuid: UUID) -> Data {
        var u = uuid.uuid
        return withUnsafeBytes(of: &u) { Data($0) }
    }

    private func bytesToUUID(_ data: Data) -> UUID? {
        guard data.count >= 16 else { return nil }
        var bytes = [UInt8](repeating: 0, count: 16)
        data.prefix(16).copyBytes(to: &bytes, count: 16)
        return NSUUID(uuidBytes: bytes) as UUID
    }
}

extension BlePresenceBridge: CBPeripheralManagerDelegate {
    func peripheralManagerDidUpdateState(_ peripheral: CBPeripheralManager) {
        if peripheral.state == .poweredOn {
            startAdvertisingIfReady()
        }
    }
}

extension BlePresenceBridge: CBCentralManagerDelegate {
    func centralManagerDidUpdateState(_ central: CBCentralManager) {
        if central.state == .poweredOn {
            startScanningIfReady()
        }
    }

    func centralManager(_ central: CBCentralManager,
                        didDiscover peripheral: CBPeripheral,
                        advertisementData: [String: Any],
                        rssi RSSI: NSNumber) {
        guard let mfg = advertisementData[CBAdvertisementDataManufacturerDataKey] as? Data,
              mfg.count >= 18 else { return }
        // First 2 bytes = company ID (little-endian)
        let company = UInt16(mfg[0]) | (UInt16(mfg[1]) << 8)
        guard company == Self.manufacturerID else { return }
        guard let peerUUID = bytesToUUID(mfg.dropFirst(2)) else { return }
        let peerID = peerUUID.uuidString.lowercased()
        if peerID == node.selfID().lowercased() { return }
        do {
            try node.reportBLESighting(peerID, peerName: "", peerPlatform: "unknown", rssi: RSSI.intValue)
        } catch {
            log.error("reportBLESighting failed: \(error.localizedDescription)")
        }
    }
}
