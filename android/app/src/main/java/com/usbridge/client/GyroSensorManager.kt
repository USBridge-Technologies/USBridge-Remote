package com.usbridge.client

import android.content.Context
import android.hardware.Sensor
import android.hardware.SensorEvent
import android.hardware.SensorEventListener
import android.hardware.SensorManager
import android.util.Log
import kotlin.math.sqrt

class GyroSensorManager(context: Context) {

    companion object {
        private const val TAG = "GyroSensorManager"
        // Minimum total acceleration magnitude to detect a shake (m/s²).
        // Earth gravity is ~9.8 m/s²; a shake registers at ~20 m/s² (~2G).
        private const val SHAKE_THRESHOLD = 20f
        // Minimum interval between consecutive shake callbacks (ms).
        private const val SHAKE_COOLDOWN_MS = 500L
    }

    private val sensorManager = context.getSystemService(Context.SENSOR_SERVICE) as SensorManager
    private val gyroSensor: Sensor? = sensorManager.getDefaultSensor(Sensor.TYPE_GYROSCOPE)
    private val accelSensor: Sensor? = sensorManager.getDefaultSensor(Sensor.TYPE_ACCELEROMETER)

    private var lastGyroTimestampNs: Long = 0
    private var lastShakeMs: Long = 0

    private val listener = object : SensorEventListener {
        override fun onSensorChanged(event: SensorEvent) {
            if (!GyroBridge.isGyroMouseModeActive()) return

            when (event.sensor.type) {
                Sensor.TYPE_GYROSCOPE -> {
                    val now = event.timestamp
                    val dtMs = if (lastGyroTimestampNs == 0L) {
                        10f // assume 100 Hz on first sample
                    } else {
                        (now - lastGyroTimestampNs) / 1_000_000f
                    }
                    lastGyroTimestampNs = now

                    if (dtMs in 1f..100f) {
                        GyroBridge.onGyroEvent(event.values[0], event.values[1], event.values[2], dtMs)
                    }
                }

                Sensor.TYPE_ACCELEROMETER -> {
                    val ax = event.values[0]
                    val ay = event.values[1]
                    val az = event.values[2]
                    val mag = sqrt(ax * ax + ay * ay + az * az)
                    val nowMs = System.currentTimeMillis()
                    if (mag > SHAKE_THRESHOLD && nowMs - lastShakeMs > SHAKE_COOLDOWN_MS) {
                        lastShakeMs = nowMs
                        Log.d(TAG, "Shake detected: mag=%.1f".format(mag))
                        GyroBridge.onShakeGesture()
                    }
                }
            }
        }

        override fun onAccuracyChanged(sensor: Sensor, accuracy: Int) {}
    }

    fun start() {
        lastGyroTimestampNs = 0
        gyroSensor?.let {
            sensorManager.registerListener(listener, it, SensorManager.SENSOR_DELAY_GAME)
            Log.d(TAG, "Gyroscope listener registered")
        } ?: Log.w(TAG, "No gyroscope sensor available")

        accelSensor?.let {
            sensorManager.registerListener(listener, it, SensorManager.SENSOR_DELAY_GAME)
            Log.d(TAG, "Accelerometer listener registered")
        }
    }

    fun stop() {
        sensorManager.unregisterListener(listener)
        lastGyroTimestampNs = 0
        Log.d(TAG, "Sensor listeners unregistered")
    }
}
