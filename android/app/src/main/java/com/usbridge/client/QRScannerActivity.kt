package com.usbridge.client

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
 * Окно с live viewfinder камеры — наводишь на QR, считывает автоматически (как в Telegram).
 * Результат возвращается через setResult — MainActivity обрабатывает в onActivityResult.
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

        Log.i(TAG, "✅ QR отсканирован: $text")
        // Callback из фонового потока ZXing — setResult и finish только на UI
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
            Log.i(TAG, "Пользователь закрыл сканер")
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
