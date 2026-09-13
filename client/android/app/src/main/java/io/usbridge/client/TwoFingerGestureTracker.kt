package io.usbridge.client

import android.view.MotionEvent
import kotlin.math.sqrt

/**
 * Stateful two-finger gesture tracker with mode locked at gesture start.
 *
 * Mode is decided ONCE when the second finger touches down, from initial
 * finger distance (no mid-gesture reclassify — that caused scroll↔zoom slips
 * and jerky zoom):
 *
 *   PINCH (fingers far apart, >= panZoomThresholdPx)
 *     → pinch zoom only (no scroll, no canvas pan).
 *
 *   SCROLL (fingers close, < panZoomThresholdPx)
 *     → vertical scroll wheel only (no zoom).
 *
 * Canvas grab-pan is the Control footer move button (one finger).
 */
class TwoFingerGestureTracker(
    private val panZoomThresholdPx: Float,
    private val onActiveChanged: (Boolean) -> Unit,
    private val onPanZoom: (scale: Float, focusX: Float, focusY: Float, dx: Float, dy: Float) -> Unit,
    private val onScroll: (scrollDy: Float) -> Unit,
) {
    private enum class Mode { NONE, PINCH, SCROLL }

    private var mode = Mode.NONE
    private var lastDist = 0f
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

    fun cancel() = deactivate()

    private fun activate(ev: MotionEvent) {
        lastDist = dist(ev)
        lastCentY = centY(ev)
        // Lock once from spacing at second-finger down — exclusive for life of gesture.
        mode = if (lastDist >= panZoomThresholdPx) Mode.PINCH else Mode.SCROLL
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
        val dy = curCentY - lastCentY

        when (mode) {
            Mode.PINCH ->
                // Zoom only. Forward every frame; Go applyViewportGesture has its
                // own ~2% deadzone so spacing noise does not accumulate zoom.
                onPanZoom(scale, curCentX, curCentY, 0f, 0f)

            Mode.SCROLL ->
                onScroll(dy)

            Mode.NONE -> {}
        }

        lastDist = curDist
        lastCentY = curCentY
    }

    private fun deactivate() {
        mode = Mode.NONE
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
