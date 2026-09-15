package io.usbridge.client

/**
 * System Back (gesture or 3-button) → Go.
 * Returns true when Go consumed the event (exit fullscreen / dismiss keyboard).
 */
object BackBridge {
    @JvmStatic
    external fun onSystemBack(): Boolean
}
