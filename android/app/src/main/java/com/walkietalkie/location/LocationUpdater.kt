package com.walkietalkie.location

import android.annotation.SuppressLint
import android.content.Context
import android.os.Looper
import android.util.Log
import com.google.android.gms.location.LocationCallback
import com.google.android.gms.location.LocationRequest
import com.google.android.gms.location.LocationResult
import com.google.android.gms.location.LocationServices
import com.google.android.gms.location.Priority
import mobile.Node

/**
 * Feeds this device's GPS fix into the shared core's Node on a regular
 * interval, per the spec's "devices must update their GPS location on a
 * regular interval" requirement. Node.updateLocation both records it
 * locally and re-announces it over mDNS (see core/mobile's design) so
 * peers pick up the new fix without a separate push API.
 */
class LocationUpdater(private val context: Context, private val node: Node) {

    private val client = LocationServices.getFusedLocationProviderClient(context)
    private var callback: LocationCallback? = null

    @SuppressLint("MissingPermission") // caller (MainActivity) gates this on permission grant
    fun start(intervalMillis: Long = 30_000) {
        stop()

        val request = LocationRequest.Builder(Priority.PRIORITY_BALANCED_POWER_ACCURACY, intervalMillis)
            .setMinUpdateIntervalMillis(intervalMillis / 2)
            .build()

        val cb = object : LocationCallback() {
            override fun onLocationResult(result: LocationResult) {
                val location = result.lastLocation ?: return
                try {
                    node.updateLocation(location.latitude, location.longitude, location.accuracy.toDouble())
                } catch (e: Exception) {
                    Log.e(TAG, "failed to push location update", e)
                }
            }
        }
        callback = cb
        client.requestLocationUpdates(request, cb, Looper.getMainLooper())
    }

    fun stop() {
        callback?.let { client.removeLocationUpdates(it) }
        callback = null
    }

    companion object {
        private const val TAG = "LocationUpdater"
    }
}
