package io.usbridge.client

import android.view.MotionEvent
import kotlin.math.abs
import kotlin.math.sqrt

/**
 * Stateful two-finger gesture tracker (RustDesk-style canvas grab).
 *
 * After the second finger lands, the gesture locks into either:
 *   - PAN: centroid drag moves the video; scale is forced to 1 so finger-distance
 *     noise cannot accumulate zoom and re-clamp pan back to center
 *   - PINCH: distance change drives zoom (centroid pan still forwarded)
 *
 * Locking the sub-mode once prevents mid-gesture flip-flops that made the
 * picture jerk and snap to center while fingers were still down.
 */
class TwoFingerGestureTracker(
    /**
     * Kept for call-site compatibility; unused (two-finger is always canvas pan/zoom).
     */
    @Suppress("UNUSED_PARAMETER")
    private val panZoomThresholdPx: Float,
    /** Called with true when a two-finger gesture begins, false when it ends. */
    private val onActiveChanged: (Boolean) -> Unit,
    /** Called every frame with (scaleFactor, focusX, focusY, panDx, panDy). */
    private val onPanZoom: (scale: Float, focusX: Float, focusY: Float, dx: Float, dy: Float) -> Unit,
    /**
     * Unused: previously fired for close-finger vertical drag as a scroll wheel.
     * Retained so MainActivity wiring stays stable.
     */
    @Suppress("UNUSED_PARAMETER")
    private val onScroll: (scrollDy: Float) -> Unit,
) {
    private enum class Mode { NONE, ACTIVE }
    private enum class SubMode { UNDECIDED, PAN, PINCH }

    private var mode = Mode.NONE
    private var subMode = SubMode.UNDECIDED
    private var lastDist = 0f
    private var lastCentX = 0f
    private var lastCentY = 0f
    private var active = false

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
        lastDist = dist(ev)
        lastCentX = centX(ev)
        lastCentY = centY(ev)
        mode = Mode.ACTIVE
        subMode = SubMode.UNDECIDED
        if (!active) {
            active = true
            onActiveChanged(true)
        }
    }

    private fun update(ev: MotionEvent) {
        if (mode == Mode.NONE) {
            activate(ev)
            return
        }

        val curDist = dist(ev)
        val curCentX = centX(ev)
        val curCentY = centY(ev)
        val scale = if (lastDist > 0f && curDist > 0f) curDist / lastDist else 1f
        val dx = curCentX - lastCentX
        val dy = curCentY - lastCentY
        val distDelta = abs(curDist - lastDist)
        val panMag = sqrt(dx * dx + dy * dy)

        // Decide once the fingers have moved enough to tell pan from pinch.
        if (subMode == SubMode.UNDECIDED && (panMag > 10f || distDelta > 10f)) {
            subMode = if (panMag >= distDelta * 1.2f) SubMode.PAN else SubMode.PINCH
        }

        val outScale = when (subMode) {
            SubMode.PAN -> 1f // ignore distance noise during grab-and-drag
            SubMode.PINCH, SubMode.UNDECIDED -> scale
        }
        onPanZoom(outScale, curCentX, curCentY, dx, dy)

        lastDist = curDist
        lastCentX = curCentX
        lastCentY = curCentY
    }

    private fun deactivate() {
        mode = Mode.NONE
        subMode = SubMode.UNDECIDED
        lastDist = 0f
        if (active) {
            active = false
            onActiveChanged(false)
        }
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
