package com.usbridge.client

import android.util.Log
import android.view.Surface
import android.view.SurfaceHolder
import android.view.SurfaceView
import android.view.View
import android.widget.FrameLayout

/**
 * Manages a SurfaceView overlay for Vulkan video rendering.
 *
 * Flow:
 *   1. Go/native calls createOverlay(x, y, w, h) — creates a SurfaceView in the activity window.
 *   2. When the SurfaceView surface is ready, pendingSurface is set.
 *   3. Native code polls getPendingSurface() to obtain the Surface → ANativeWindow*.
 *   4. Native Vulkan renderer creates VkSurfaceKHR from the ANativeWindow.
 *   5. Go calls setRect(x, y, w, h) periodically to reposition the overlay.
 *   6. Go calls destroy() when streaming stops.
 */
object VulkanOverlayBridge {
    private const val TAG = "VulkanOverlayBridge"

    @Volatile private var surfaceView: SurfaceView? = null
    @Volatile private var pendingSurface: Surface? = null

    private val holderCallback = object : SurfaceHolder.Callback {
        override fun surfaceCreated(holder: SurfaceHolder) {
            Log.i(TAG, "surfaceCreated")
            pendingSurface = holder.surface
        }
        override fun surfaceChanged(holder: SurfaceHolder, format: Int, width: Int, height: Int) {
            Log.i(TAG, "surfaceChanged ${width}x${height}")
            // Re-expose the surface so the C render thread can recreate the Vulkan
            // swapchain at the correct dimensions on the next vk_recreate_swapchain call.
            // This handles SetFullScreen() transitions where surfaceCreated fires with a
            // 1x1 (or 0x0) surface and the real size arrives later via surfaceChanged.
            if (width > 1 && height > 1) {
                pendingSurface = holder.surface
            }
        }
        override fun surfaceDestroyed(holder: SurfaceHolder) {
            Log.i(TAG, "surfaceDestroyed")
            pendingSurface = null
        }
    }

    /** Create (or replace) the SurfaceView overlay at the given pixel rect. */
    @JvmStatic
    fun createOverlay(x: Int, y: Int, w: Int, h: Int) {
        val activity = MainActivity.getInstance() ?: run {
            Log.e(TAG, "createOverlay: MainActivity instance not available")
            return
        }
        activity.runOnUiThread {
            // Remove any previous overlay.
            surfaceView?.let { old ->
                (activity.window.decorView as? FrameLayout)?.removeView(old)
                old.holder.removeCallback(holderCallback)
            }
            pendingSurface = null

            val sv = SurfaceView(activity)
            sv.holder.addCallback(holderCallback)
            // Render on top of EVERYTHING. This ensures visibility even if the Fyne window is opaque.
            // We use hooks in Go to hide this overlay when Fyne menus or dialogs are shown.
            sv.setZOrderOnTop(true)
            sv.holder.setFormat(android.graphics.PixelFormat.OPAQUE)

            val lp = FrameLayout.LayoutParams(
                w.coerceAtLeast(1),
                h.coerceAtLeast(1)
            )
            lp.leftMargin = x
            lp.topMargin  = y

            val decorView = activity.window.decorView as? FrameLayout ?: run {
                Log.e(TAG, "createOverlay: decorView is not a FrameLayout")
                return@runOnUiThread
            }
            decorView.addView(sv, lp)
            surfaceView = sv
            Log.i(TAG, "createOverlay: SurfaceView added at ($x,$y) ${w}x${h}")
        }
    }

    /** Returns the pending Surface once it's ready, or null if not yet available. */
    @JvmStatic
    fun getPendingSurface(): Surface? = pendingSurface

    /** Clear the pending surface reference after native code has obtained the ANativeWindow. */
    @JvmStatic
    fun clearPendingSurface() {
        pendingSurface = null
    }

    /** Reposition and resize the SurfaceView overlay. */
    @JvmStatic
    fun setRect(x: Int, y: Int, w: Int, h: Int) {
        val sv = surfaceView ?: return
        val activity = MainActivity.getInstance() ?: return
        activity.runOnUiThread {
            val lp = sv.layoutParams as? FrameLayout.LayoutParams ?: return@runOnUiThread
            lp.leftMargin = x
            lp.topMargin  = y
            lp.width      = w.coerceAtLeast(1)
            lp.height     = h.coerceAtLeast(1)
            sv.layoutParams = lp
        }
    }

    /** Show or hide the SurfaceView overlay (e.g. when a Fyne menu opens). */
    @JvmStatic
    fun setVisible(visible: Boolean) {
        val sv = surfaceView ?: return
        val activity = MainActivity.getInstance() ?: return
        activity.runOnUiThread {
            // INVISIBLE keeps the surface alive (no surfaceDestroyed/surfaceCreated cycle).
        // GONE would destroy the ANativeWindow and break the Vulkan swapchain.
        sv.visibility = if (visible) View.VISIBLE else View.INVISIBLE
        }
    }

    /** Remove the SurfaceView overlay and reset state. */
    @JvmStatic
    fun destroy() {
        val activity = MainActivity.getInstance()
        pendingSurface = null
        val sv = surfaceView ?: return
        sv.holder.removeCallback(holderCallback)
        surfaceView = null
        activity?.runOnUiThread {
            (activity.window.decorView as? FrameLayout)?.removeView(sv)
            Log.i(TAG, "destroy: SurfaceView removed")
        }
    }
}
