package com.walkietalkie.mesh

/**
 * Host apps (:app / :wear) set these before starting [com.walkietalkie.ptt.PTTService]
 * so the shared Go node announces the correct platform/version.
 */
object MeshIdentity {
    @Volatile
    var platform: String = "android"

    @Volatile
    var appVersion: String = "0.0.0"
}
