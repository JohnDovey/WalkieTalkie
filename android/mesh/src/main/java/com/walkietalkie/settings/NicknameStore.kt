package com.walkietalkie.settings

import android.content.Context

/**
 * Persists the user-set nickname/display name across app restarts. When
 * empty, [PTTService][com.walkietalkie.ptt.PTTService] falls back to the
 * device's manufacturer+model as the display name.
 */
object NicknameStore {
    private const val PREFS_NAME = "walkietalkie_settings"
    private const val KEY_NICKNAME = "nickname"

    fun get(context: Context): String {
        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        return prefs.getString(KEY_NICKNAME, "") ?: ""
    }

    fun set(context: Context, nickname: String) {
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .putString(KEY_NICKNAME, nickname)
            .apply()
    }
}
