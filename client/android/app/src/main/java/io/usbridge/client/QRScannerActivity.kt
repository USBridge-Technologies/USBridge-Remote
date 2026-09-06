package io.usbridge.client

import android.content.Intent
import android.os.Bundle
import android.util.Log
import android.widget.Button
import androidx.appcompat.app.AppCompatActivity
import com.google.zxing.BarcodeFormat
import com.journeyapps.barcodescanner.BarcodeCallback
import com.journeyapps.barcodescanner.BarcodeResult
import com.journeyapps.barcodescanner.DecoratedBarcodeView
import com.journeyapps.barcodescanner.DefaultDecoderFactory
import java.util.Arrays

/**
 * A window with a live camera viewfinder — point it at a QR code, it scans automatically (like Telegram).
 * The result comes back via setResult -- MainActivity handles it in onActivityResult.
 */
class QRScannerActivity : AppCompatActivity() {

    companion object {
        private const val TAG = "QRScannerActivity"
        const val EXTRA_QR_CONTENTS = "qr_contents"
    }

    private lateinit var barcodeView: DecoratedBarcodeView
    private var lastText: String? = null

    private val barcodeCallback = BarcodeCallback { result: BarcodeResult ->
        val text = result.text ?: return@BarcodeCallback
        if (text == lastText) return@BarcodeCallback
        lastText = text

        Log.i(TAG, "✅ QR scanned: $text")
        // Callback from ZXing's background thread -- setResult and finish only on the UI thread
        runOnUiThread {
            barcodeView.pause()
            setResult(RESULT_OK, Intent().putExtra(EXTRA_QR_CONTENTS, text))
            finish()
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_qr_scanner)

        barcodeView = findViewById(R.id.barcode_scanner)
        barcodeView.setStatusText(getString(R.string.point_camera_at_qr))
        barcodeView.barcodeView.decoderFactory = DefaultDecoderFactory(
            Arrays.asList(BarcodeFormat.QR_CODE)
        )
        barcodeView.decodeContinuous(barcodeCallback)

        findViewById<Button>(R.id.btn_close).setOnClickListener {
            Log.i(TAG, "User closed the scanner")
            setResult(RESULT_CANCELED)
            finish()
        }
    }

    override fun onResume() {
        super.onResume()
        barcodeView.resume()
    }

    override fun onPause() {
        barcodeView.pause()
        super.onPause()
    }
}
