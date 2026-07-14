package com.walkietalkie.ble

import android.annotation.SuppressLint
import android.bluetooth.BluetoothManager
import android.bluetooth.le.AdvertiseCallback
import android.bluetooth.le.AdvertiseData
import android.bluetooth.le.AdvertiseSettings
import android.bluetooth.le.BluetoothLeAdvertiser
import android.bluetooth.le.BluetoothLeScanner
import android.bluetooth.le.ScanCallback
import android.bluetooth.le.ScanFilter
import android.bluetooth.le.ScanResult
import android.bluetooth.le.ScanSettings
import android.content.Context
import android.os.ParcelUuid
import android.util.Log
import mobile.Node
import java.nio.ByteBuffer
import java.util.UUID

/**
 * Off-LAN presence detection via Bluetooth LE, per the plan's confirmed
 * "BLE presence-only fallback" decision: this can tell a peer is nearby
 * (id + RSSI) even with no shared LAN, but never carries GPS or audio —
 * BLE advertisement payloads are too small, and the plan explicitly scopes
 * BLE to presence-only for v1.
 *
 * Design (a real BLE protocol constraint, not arbitrary): the 31-byte
 * primary advertisement packet only has room for flags + one 128-bit
 * service UUID, so it carries a single **fixed** service UUID identifying
 * "this is a WalkieTalkie peer" (used as the scan filter). The variable
 * per-install device ID (16 raw bytes) goes in the **scan response**
 * packet's manufacturer-specific data instead, since that's a separate
 * ~31-byte packet only sent to active scanners.
 *
 * MANUFACTURER_ID uses a private/unassigned value (0xFFFF is reserved for
 * testing) since this is closed, local-only communication between our own
 * app instances, not a certified product broadcasting to arbitrary
 * Bluetooth SIG member devices.
 */
class BlePresenceBridge(context: Context, private val node: Node) {

    private val bluetoothManager = context.getSystemService(Context.BLUETOOTH_SERVICE) as BluetoothManager
    private var advertiser: BluetoothLeAdvertiser? = null
    private var scanner: BluetoothLeScanner? = null
    private var scanCallback: ScanCallback? = null

    @SuppressLint("MissingPermission") // caller (MainActivity) gates this on BLUETOOTH_SCAN/ADVERTISE grant
    fun start() {
        val adapter = bluetoothManager.adapter ?: run {
            Log.w(TAG, "no Bluetooth adapter available")
            return
        }
        if (!adapter.isEnabled) {
            Log.w(TAG, "Bluetooth disabled, skipping BLE presence")
            return
        }

        startAdvertising(adapter.bluetoothLeAdvertiser)
        startScanning(adapter.bluetoothLeScanner)
    }

    @SuppressLint("MissingPermission")
    private fun startAdvertising(adv: BluetoothLeAdvertiser?) {
        if (adv == null) return
        advertiser = adv

        val settings = AdvertiseSettings.Builder()
            .setAdvertiseMode(AdvertiseSettings.ADVERTISE_MODE_BALANCED)
            .setTxPowerLevel(AdvertiseSettings.ADVERTISE_TX_POWER_MEDIUM)
            .setConnectable(false)
            .build()

        val advertiseData = AdvertiseData.Builder()
            .setIncludeDeviceName(false)
            .addServiceUuid(ParcelUuid(SERVICE_UUID))
            .build()

        val scanResponse = AdvertiseData.Builder()
            .addManufacturerData(MANUFACTURER_ID, uuidToBytes(UUID.fromString(node.selfID())))
            .build()

        adv.startAdvertising(settings, advertiseData, scanResponse, object : AdvertiseCallback() {
            override fun onStartFailure(errorCode: Int) {
                Log.w(TAG, "advertise start failed: $errorCode")
            }
        })
    }

    @SuppressLint("MissingPermission")
    private fun startScanning(scan: BluetoothLeScanner?) {
        if (scan == null) return
        scanner = scan

        val filter = ScanFilter.Builder()
            .setServiceUuid(ParcelUuid(SERVICE_UUID))
            .build()
        val settings = ScanSettings.Builder()
            .setScanMode(ScanSettings.SCAN_MODE_LOW_POWER)
            .build()

        val callback = object : ScanCallback() {
            override fun onScanResult(callbackType: Int, result: ScanResult) {
                val peerID = extractPeerID(result) ?: return
                if (peerID == node.selfID()) return
                try {
                    // gomobile maps Go's `int rssi` param to a Java `long`.
                    node.reportBLESighting(peerID, "", "unknown", result.rssi.toLong())
                } catch (e: Exception) {
                    Log.e(TAG, "failed to report BLE sighting for $peerID", e)
                }
            }

            override fun onScanFailed(errorCode: Int) {
                Log.w(TAG, "BLE scan failed: $errorCode")
            }
        }
        scanCallback = callback
        scan.startScan(listOf(filter), settings, callback)
    }

    private fun extractPeerID(result: ScanResult): String? {
        val bytes = result.scanRecord?.getManufacturerSpecificData(MANUFACTURER_ID) ?: return null
        if (bytes.size < 16) return null
        return try {
            bytesToUuid(bytes).toString()
        } catch (e: Exception) {
            null
        }
    }

    private fun uuidToBytes(uuid: UUID): ByteArray {
        val buffer = ByteBuffer.allocate(16)
        buffer.putLong(uuid.mostSignificantBits)
        buffer.putLong(uuid.leastSignificantBits)
        return buffer.array()
    }

    private fun bytesToUuid(bytes: ByteArray): UUID {
        val buffer = ByteBuffer.wrap(bytes)
        return UUID(buffer.long, buffer.long)
    }

    @SuppressLint("MissingPermission")
    fun stop() {
        scanCallback?.let { scanner?.stopScan(it) }
        scanCallback = null
        advertiser?.stopAdvertising(object : AdvertiseCallback() {})
    }

    companion object {
        private const val TAG = "BlePresenceBridge"

        // Fixed UUID identifying "this advertisement is a WalkieTalkie peer" —
        // same for every install, used only as the scan filter. Generated
        // once for this project; not a per-device identifier.
        private val SERVICE_UUID = UUID.fromString("6e7a1b2c-6f6f-4a1a-9f2a-8f3b0d6c9a11")

        // Reserved-for-testing manufacturer ID (0xFFFF) — see class doc.
        private const val MANUFACTURER_ID = 0xFFFF
    }
}
