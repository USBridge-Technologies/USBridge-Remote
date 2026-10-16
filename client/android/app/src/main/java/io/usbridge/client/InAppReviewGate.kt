package io.usbridge.client

import android.content.SharedPreferences
import java.time.LocalDate

/**
 * Decides when Play in-app review may be requested.
 *
 * All of these have to be true: 7 days since the first open, at least 3
 * separate days with 2 minutes or more in the foreground, 20 minutes of
 * counted time in total, and 7 days since the last request. At most 30
 * minutes of one calendar day is counted, so a window left open overnight
 * cannot fill the total by itself.
 */
internal class InAppReviewGate(private val prefs: SharedPreferences) {
    private var sessionStartElapsed = 0L

    fun onForeground(nowElapsed: Long) {
        if (sessionStartElapsed == 0L) {
            sessionStartElapsed = nowElapsed
        }
        if (prefs.getLong(KEY_FIRST, 0L) == 0L) {
            prefs.edit().putLong(KEY_FIRST, System.currentTimeMillis()).commit()
        }
    }

    fun onBackground(nowElapsed: Long) {
        val start = sessionStartElapsed
        sessionStartElapsed = 0L
        if (start == 0L) {
            return
        }
        val delta = nowElapsed - start
        if (delta <= 0L) {
            return
        }
        addForeground(delta)
    }

    fun shouldAsk(now: Long = System.currentTimeMillis()): Boolean {
        val first = prefs.getLong(KEY_FIRST, 0L)
        if (first == 0L || now - first < WEEK_MS) {
            return false
        }
        if (prefs.getInt(KEY_DAYS, 0) < ACTIVE_DAYS) {
            return false
        }
        if (prefs.getLong(KEY_TOTAL_MS, 0L) < TOTAL_MIN_MS) {
            return false
        }
        val asked = prefs.getLong(KEY_ASKED, 0L)
        return asked == 0L || now - asked >= WEEK_MS
    }

    fun markAsked(now: Long = System.currentTimeMillis()) {
        prefs.edit().putLong(KEY_ASKED, now).commit()
    }

    fun status(now: Long = System.currentTimeMillis()): String {
        val first = prefs.getLong(KEY_FIRST, 0L)
        val ageDays = if (first == 0L) 0 else (now - first) / DAY_MS
        val asked = prefs.getLong(KEY_ASKED, 0L)
        val sinceAskDays = if (asked == 0L) -1 else (now - asked) / DAY_MS
        val minutes = prefs.getLong(KEY_TOTAL_MS, 0L) / 60_000L
        return "activeDays=${prefs.getInt(KEY_DAYS, 0)} minutes=$minutes ageDays=$ageDays sinceAskDays=$sinceAskDays"
    }

    private fun addForeground(deltaMs: Long) {
        val today = LocalDate.now().toString()
        val storedDay = prefs.getString(KEY_TODAY, "") ?: ""
        var todayMs = if (storedDay == today) prefs.getLong(KEY_TODAY_MS, 0L) else 0L
        var days = prefs.getInt(KEY_DAYS, 0)
        var total = prefs.getLong(KEY_TOTAL_MS, 0L)
        val room = (DAY_CAP_MS - todayMs).coerceAtLeast(0L)
        val add = deltaMs.coerceAtMost(room)
        val alreadyQualified = todayMs >= DAY_MIN_MS
        todayMs += add
        total += add
        if (!alreadyQualified && todayMs >= DAY_MIN_MS) {
            days += 1
        }
        prefs.edit()
            .putString(KEY_TODAY, today)
            .putLong(KEY_TODAY_MS, todayMs)
            .putLong(KEY_TOTAL_MS, total)
            .putInt(KEY_DAYS, days)
            .commit()
    }

    private companion object {
        const val KEY_FIRST = "review_first_at"
        const val KEY_ASKED = "review_asked_at"
        const val KEY_TODAY = "review_today"
        const val KEY_TODAY_MS = "review_today_ms"
        const val KEY_TOTAL_MS = "review_total_ms"
        const val KEY_DAYS = "review_active_days"

        const val DAY_MS = 24L * 60L * 60L * 1000L
        const val WEEK_MS = 7L * DAY_MS
        const val DAY_MIN_MS = 2L * 60L * 1000L
        const val DAY_CAP_MS = 30L * 60L * 1000L
        const val TOTAL_MIN_MS = 20L * 60L * 1000L
        const val ACTIVE_DAYS = 3
    }
}
