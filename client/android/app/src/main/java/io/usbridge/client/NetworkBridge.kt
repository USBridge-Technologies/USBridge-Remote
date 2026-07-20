package io.usbridge.client

/**
 * Bridge for network change notifications from Android to Go
 */
object NetworkBridge {
    /**
     * Called when network connectivity changes
     */
    @JvmStatic
    external fun onNetworkChanged()
}
