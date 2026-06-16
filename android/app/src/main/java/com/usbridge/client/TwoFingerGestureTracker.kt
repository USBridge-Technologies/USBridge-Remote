package com.usbridge.client

import android.view.MotionEvent
import kotlin.math.abs
import kotlin.math.sqrt

/**
 * Stateful two-finger gesture tracker with mode locked at gesture start.
 *
 * Mode is decided ONCE when the second finger touches down, based on initial finger distance:
 *
 *   PAN_ZOOM   (fingers far apart, >= panZoomThresholdPx)
 *     → centroid pan  + pinch zoom, delivered together every frame.
 *
 *   SCROLL_ZOOM (fingers close, < panZoomThresholdPx)
 *     → vertical scroll wheel + pinch zoom, delivered as independent channels every frame.
 *       Zoom and scroll never block each other.
 *
 * The mode never changes mid-gesture, eliminating the per-frame flip-flopping that caused
 * scroll/zoom/pan to conflict with each other.
 */
class TwoFingerGestureTracker(
    /** Distance threshold (physical pixels) that separates SCROLL_ZOOM from PAN_ZOOM. */
    private val panZoomThresholdPx: Float,
    /** Called with true when a two-finger gesture begins, false when it ends. */
    private val onActiveChanged: (Boolean) -> Unit,
    /** Called every frame in PAN_ZOOM mode with (scaleFactor, focusX, focusY, panDx, panDy). */
    private val onPanZoom: (scale: Float, focusX: Float, focusY: Float, dx: Float, dy: Float) -> Unit,
    /**
     * Called every frame in SCROLL_ZOOM mode with (scrollDy, scaleFactor, focusX, focusY).
     * Both channels are always delivered — implement them independently.
     * scrollDy: raw centroid-Y delta (positive = fingers moved down).
     * scaleFactor: pinch ratio (1.0 = no change).
     */
    private val onScrollZoom: (scrollDy: Float, scale: Float, focusX: Float, focusY: Float) -> Unit,
) {
    private enum class Mode { NONE, PAN_ZOOM, SCROLL_ZOOM }

    private var mode = Mode.NONE
    private var lastDist  = 0f
    private var lastCentX = 0f
    private var lastCentY = 0f
    private var active    = false

    fun onTouchEvent(ev: MotionEvent) {
        when (ev.actionMasked) {
            MotionEvent.ACTION_DOWN,
            MotionEvent.ACTION_UP,
            MotionEvent.ACTION_CANCEL -> if (ev.pointerCount < 2) deactivate()

            MotionEvent.ACTION_POINTER_DOWN ->
                if (ev.pointerCount >= 2) activate(ev)

            MotionEvent.ACTION_POINTER_UP ->
                if (ev.pointerCount <= 2) deactivate() else activate(ev)

            MotionEvent.ACTION_MOVE ->
                if (ev.pointerCount >= 2) update(ev) else deactivate()
        }
    }

    /** Force-end any active gesture (call from onDestroy / focus loss). */
    fun cancel() = deactivate()

    // ── private ──────────────────────────────────────────────────────────────

    private fun activate(ev: MotionEvent) {
        lastDist  = dist(ev)
        lastCentX = centX(ev)
        lastCentY = centY(ev)
        mode = if (lastDist >= panZoomThresholdPx) Mode.PAN_ZOOM else Mode.SCROLL_ZOOM
        if (!active) { active = true; onActiveChanged(true) }
    }

    private fun update(ev: MotionEvent) {
        if (mode == Mode.NONE) { activate(ev); return }

        val curDist  = dist(ev)
        val curCentX = centX(ev)
        val curCentY = centY(ev)
        val scale    = if (lastDist > 0f && curDist > 0f) curDist / lastDist else 1f
        val dx = curCentX - lastCentX
        val dy = curCentY - lastCentY

        when (mode) {
            Mode.PAN_ZOOM ->
                onPanZoom(scale, curCentX, curCentY, dx, dy)

            Mode.SCROLL_ZOOM ->
                // Deliver scroll and zoom as independent channels — neither blocks the other.
                onScrollZoom(dy, scale, curCentX, curCentY)

            Mode.NONE -> {}
        }

        lastDist  = curDist
        lastCentX = curCentX
        lastCentY = curCentY
    }

    private fun deactivate() {
        mode = Mode.NONE
        lastDist = 0f
        if (active) { active = false; onActiveChanged(false) }
    }

    private fun dist(ev: MotionEvent): Float {
        if (ev.pointerCount < 2) return 0f
        val dx = ev.getX(1) - ev.getX(0)
        val dy = ev.getY(1) - ev.getY(0)
        return sqrt(dx * dx + dy * dy)
    }

    private fun centX(ev: MotionEvent) =
        if (ev.pointerCount >= 2) (ev.getX(0) + ev.getX(1)) / 2f else ev.x

    private fun centY(ev: MotionEvent) =
        if (ev.pointerCount >= 2) (ev.getY(0) + ev.getY(1)) / 2f else ev.y
}
