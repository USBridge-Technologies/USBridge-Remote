package io.usbridge.client

import android.os.Handler
import android.os.Looper
import android.view.Choreographer

/**
 * Phase lock: tells the native side when this display refreshes, while a stream is on screen.
 *
 * Every Choreographer frame hands over its vsync time and the refresh period (the median
 * gap of the last few frames, so a late frame doesn't read as a slow display); the decoder's
 * output thread turns that into "this frame has N ms to the next refresh" for the host
 * (phase_vsync_android.c). Runs only between [start] and [stop], so an idle UI does not hold
 * the display at its top refresh rate.
 */
object PhaseBridge {
    @JvmStatic
    external fun onVsync(frameTimeNanos: Long, periodNanos: Long): Boolean

    private val main = Handler(Looper.getMainLooper())
    private var running = false
    private var startedAt = 0L
    private var last = 0L
    private val gaps = LongArray(16)
    private var gapCount = 0

    private val callback = object : Choreographer.FrameCallback {
        override fun doFrame(frameTimeNanos: Long) {
            if (!running) return
            if (last > 0) {
                val gap = frameTimeNanos - last
                if (gap in 2_000_000L..60_000_000L) {
                    gaps[gapCount++ % gaps.size] = gap
                    // Median, not the minimum: timestamp noise only ever makes a gap shorter, and a period
                    // 1% short makes the host send the stream 1% too fast for this display.
                    val sorted = gaps.take(minOf(gapCount, gaps.size)).sorted()
                    val period = sorted[sorted.size / 2]
                    try {
                        // Stops itself once the decoder has been idle (the stream ended without a stop()).
                        if (!onVsync(frameTimeNanos, period) && frameTimeNanos - startedAt > 5_000_000_000L) {
                            running = false
                            return
                        }
                    } catch (_: UnsatisfiedLinkError) {
                        running = false
                        return
                    }
                }
            }
            last = frameTimeNanos
            Choreographer.getInstance().postFrameCallback(this)
        }
    }

    @JvmStatic
    fun start() {
        main.post {
            if (running) return@post
            running = true
            startedAt = System.nanoTime()
            last = 0
            gapCount = 0
            Choreographer.getInstance().postFrameCallback(callback)
        }
    }

    @JvmStatic
    fun stop() {
        main.post {
            running = false
            Choreographer.getInstance().removeFrameCallback(callback)
        }
    }
}
