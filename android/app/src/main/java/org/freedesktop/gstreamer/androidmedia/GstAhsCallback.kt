package org.freedesktop.gstreamer.androidmedia

import android.hardware.Sensor
import android.hardware.SensorEvent
import android.hardware.SensorEventListener

class GstAhsCallback(sensorCallback: Long, accuracyCallback: Long, userData: Long) :
    SensorEventListener {

    var mUserData: Long = userData
    var mSensorCallback: Long = sensorCallback
    var mAccuracyCallback: Long = accuracyCallback

    override fun onSensorChanged(event: SensorEvent) {
        gst_ah_sensor_on_sensor_changed(event, mSensorCallback, mUserData)
    }

    override fun onAccuracyChanged(sensor: Sensor, accuracy: Int) {
        gst_ah_sensor_on_accuracy_changed(sensor, accuracy, mAccuracyCallback, mUserData)
    }

    companion object {
        @JvmStatic
        external fun gst_ah_sensor_on_sensor_changed(
            event: SensorEvent,
            callback: Long,
            userData: Long,
        )

        @JvmStatic
        external fun gst_ah_sensor_on_accuracy_changed(
            sensor: Sensor,
            accuracy: Int,
            callback: Long,
            userData: Long,
        )
    }
}
