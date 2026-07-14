package com.walkietalkie.wear

import android.app.Application
import com.walkietalkie.mesh.MeshIdentity

class WearApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        MeshIdentity.platform = "wear"
        MeshIdentity.appVersion = BuildConfig.VERSION_NAME
    }
}
