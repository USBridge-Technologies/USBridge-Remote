package io.usbridge.client

import android.content.ContentResolver
import android.content.Intent
import android.content.res.AssetFileDescriptor
import android.net.Uri
import android.os.ParcelFileDescriptor
import android.provider.DocumentsContract
import android.util.Log
import androidx.documentfile.provider.DocumentFile
import androidbridge.Androidbridge

/**
 * Bridge between Android SAF and the Go side (file picking, persistent permissions, fd extraction)
 * Handles file picking, persistent permissions, and fd extraction
 */
object SafBridge {
    private const val TAG = "SafBridge"

    // Static result fields (for reliable access via JNI)
    @JvmStatic @Volatile var lastUri: String = ""
    @JvmStatic @Volatile var lastFileName: String = ""
    @JvmStatic @Volatile var lastFd: Int = -1
    @JvmStatic @Volatile var lastSize: Long = 0
    @JvmStatic @Volatile var hasNewResult: Boolean = false

    @JvmStatic
    fun clearSAFResult() {
        hasNewResult = false
        lastUri = ""
        lastFileName = ""
        lastFd = -1
        lastSize = 0
    }

    // No callbacks needed - Go functions are called directly through the Androidbridge package

    /**
     * Start SAF file picker
     * Called from Go
     */
    @JvmStatic
    fun startSAFPicker() {
        val activity = MainActivity.getInstance()
        if (activity == null) {
            Log.e(TAG, "MainActivity instance is null!")
            onSAFPickerError("Activity not available")
            return
        }

        Log.i(TAG, "Starting SAF picker...")
        activity.startSAFPicker()
    }

    /**
     * Handle SAF picker result - called from MainActivity.onActivityResult
     * IMPORTANT: all file operations run on a background thread to avoid blocking the UI
     */
    fun handleActivityResult(
        requestCode: Int,
        resultCode: Int,
        data: Intent?,
        contentResolver: ContentResolver
    ) {
        val startTime = System.currentTimeMillis()
        val threadName = Thread.currentThread().name
        val threadId = Thread.currentThread().id

        Log.i(TAG, "═══════════════════════════════════════════════════════════════")
        Log.i(TAG, "🎯 [SAF-START] handleActivityResult called")
        Log.i(TAG, "📍 [SAF-START] Thread: $threadName (ID: $threadId)")
        Log.i(TAG, "📍 [SAF-START] Request code: $requestCode")
        Log.i(TAG, "📍 [SAF-START] Result code: $resultCode")
        Log.i(TAG, "═══════════════════════════════════════════════════════════════")

        if (requestCode != MainActivity.SAF_PICK_REQUEST_CODE) {
            Log.d(TAG, "⏭️ [SAF] Ignoring non-SAF request code: $requestCode")
            return
        }

        if (resultCode != android.app.Activity.RESULT_OK) {
            val elapsed = System.currentTimeMillis() - startTime
            Log.w(TAG, "❌ [SAF-CANCEL] SAF picker cancelled or failed (resultCode=$resultCode) in ${elapsed}ms")
            onSAFPickerError("File picker cancelled")
            return
        }

        val uri = data?.data
        if (uri == null) {
            val elapsed = System.currentTimeMillis() - startTime
            Log.e(TAG, "❌ [SAF-ERROR] No URI returned from SAF picker after ${elapsed}ms")
            onSAFPickerError("No file selected")
            return
        }

        Log.i(TAG, "✅ [SAF-URI] File selected: $uri")
        Log.i(TAG, "📍 [SAF-URI] URI scheme: ${uri.scheme}")
        Log.i(TAG, "📍 [SAF-URI] URI authority: ${uri.authority}")
        Log.i(TAG, "📍 [SAF-URI] URI path: ${uri.path}")

        // Detect file source for debugging
        val source = when {
            uri.authority?.contains("com.google.android.apps.docs.storage") == true -> "Google Drive"
            uri.authority?.contains("downloads") == true -> "Downloads"
            uri.authority?.contains("media") == true -> "Media Storage"
            uri.authority?.contains("externalstorage") == true -> "External Storage"
            else -> uri.authority ?: "Unknown"
        }
        Log.i(TAG, "📂 [SAF-SOURCE] File source detected: $source")

        // CRITICAL: move ALL file operations to a background thread
        // to avoid blocking the UI thread when working with Google Drive
        Log.i(TAG, "🧵 [SAF-THREAD] Launching background thread for file operations...")
        Thread {
            val bgThreadName = Thread.currentThread().name
            val bgThreadId = Thread.currentThread().id
            Log.i(TAG, "🧵 [SAF-BG-START] Background thread started: $bgThreadName (ID: $bgThreadId)")

            try {
                // Step 1: Take persistable URI permission
                Log.i(TAG, "📍 [SAF-STEP-1] Taking persistable URI permission...")
                val permStartTime = System.currentTimeMillis()
                val takeFlags = Intent.FLAG_GRANT_READ_URI_PERMISSION or
                        Intent.FLAG_GRANT_WRITE_URI_PERMISSION

                try {
                    contentResolver.takePersistableUriPermission(uri, takeFlags)
                    val permElapsed = System.currentTimeMillis() - permStartTime
                    Log.i(TAG, "✅ [SAF-STEP-1] Persistable permission granted in ${permElapsed}ms")
                } catch (e: Exception) {
                    val permElapsed = System.currentTimeMillis() - permStartTime
                    Log.w(TAG, "⚠️ [SAF-STEP-1] Could not take persistable permission (${permElapsed}ms): $e")
                    // Continue anyway - permission might still work
                }

                // Step 2: Open file descriptor
                Log.i(TAG, "📍 [SAF-STEP-2] Opening file descriptor...")
                val fdStartTime = System.currentTimeMillis()
                val pfd = contentResolver.openFileDescriptor(uri, "r")
                val fdElapsed = System.currentTimeMillis() - fdStartTime

                if (pfd == null) {
                    Log.e(TAG, "❌ [SAF-STEP-2] Failed to open ParcelFileDescriptor after ${fdElapsed}ms")
                    onSAFPickerError("Cannot open file")
                    return@Thread
                }
                Log.i(TAG, "✅ [SAF-STEP-2] ParcelFileDescriptor opened in ${fdElapsed}ms")

                // Step 3: Detach fd
                Log.i(TAG, "📍 [SAF-STEP-3] Detaching file descriptor...")
                val detachStartTime = System.currentTimeMillis()
                val fd = pfd.detachFd()
                val detachElapsed = System.currentTimeMillis() - detachStartTime
                Log.i(TAG, "✅ [SAF-STEP-3] Detached fd: $fd in ${detachElapsed}ms")

                // Step 4: Get file size and name
                Log.i(TAG, "📍 [SAF-STEP-4] Getting file details...")
                val size = getFileSize(uri, contentResolver)
                val fileName = getFileName(uri, contentResolver)
                Log.i(TAG, "📍 [SAF-STEP-4] File: $fileName, size: $size bytes")

                if (size <= 0) {
                    Log.e(TAG, "❌ [SAF-STEP-4] Invalid file size: $size")
                    // Close fd since we can't use it
                    try {
                        ParcelFileDescriptor.adoptFd(fd).close()
                        Log.i(TAG, "🗑️ [SAF-CLEANUP] Closed invalid fd: $fd")
                    } catch (e: Exception) {
                        Log.e(TAG, "❌ [SAF-CLEANUP] Error closing fd: $e")
                    }
                    onSAFPickerError("Cannot determine file size")
                    return@Thread
                }

                // Step 5: Success! Store for polling and call callback
                lastUri = uri.toString()
                lastFileName = fileName
                lastFd = fd
                lastSize = size
                hasNewResult = true

                Log.i(TAG, "═══════════════════════════════════════════════════════════════")
                Log.i(TAG, "✅ [SAF-SUCCESS] Result stored in Java for polling")
                Log.i(TAG, "📦 [SAF-RESULT] URI: $lastUri, FD: $lastFd, Size: $lastSize")
                Log.i(TAG, "═══════════════════════════════════════════════════════════════")

                try {
                    Log.i(TAG, "📞 [SAF-CALLBACK] Calling Go callback as fallback...")
                    Androidbridge.onSAFSuccess(lastUri, lastFd.toLong(), lastSize)
                } catch (e: Exception) {
                    Log.e(TAG, "❌ [SAF-CALLBACK] Error calling Go callback: $e")
                }
            } catch (e: Exception) {
                val totalElapsed = System.currentTimeMillis() - startTime
                Log.e(TAG, "═══════════════════════════════════════════════════════════════")
                Log.e(TAG, "❌ [SAF-ERROR] Exception in background thread after ${totalElapsed}ms")
                Log.e(TAG, "❌ [SAF-ERROR] Exception type: ${e.javaClass.simpleName}")
                Log.e(TAG, "❌ [SAF-ERROR] Exception message: ${e.message}")
                Log.e(TAG, "═══════════════════════════════════════════════════════════════")
                e.printStackTrace()
                onSAFPickerError("Error: ${e.message}")
            }
        }.start()

        val handlerElapsed = System.currentTimeMillis() - startTime
        Log.i(TAG, "🧵 [SAF-THREAD] Background thread launched in ${handlerElapsed}ms, returning to UI thread")
    }

    /**
     * Called when SAF picker encounters an error
     * IMPORTANT: call Go functions from a separate thread to avoid a Go
     * runtime "unknown caller pc" error when called from the UI thread
     */
    fun onSAFPickerError(message: String) {
        Log.e(TAG, "SAF picker error: $message")
        val messageCopy = String(message.toCharArray())
        Thread {
            try {
                Androidbridge.onSAFError(messageCopy)
            } catch (e: Exception) {
                Log.e(TAG, "Error calling Go error callback: $e", e)
            }
        }.start()
    }

    /**
     * Get filename from URI
     */
    private fun getFileName(uri: Uri, contentResolver: ContentResolver): String {
        try {
            contentResolver.query(uri, null, null, null, null)?.use { cursor ->
                if (cursor.moveToFirst()) {
                    val nameIndex = cursor.getColumnIndex(android.provider.OpenableColumns.DISPLAY_NAME)
                    if (nameIndex >= 0) {
                        return cursor.getString(nameIndex)
                    }
                }
            }
        } catch (e: Exception) {
            Log.w(TAG, "Failed to get filename: ${e.message}")
        }
        return uri.path?.substringAfterLast('/') ?: "unknown"
    }

    /**
     * Get file size from URI
     * Tries multiple methods with detailed logging
     */
    private fun getFileSize(uri: Uri, contentResolver: ContentResolver): Long {
        Log.i(TAG, "📏 [SIZE] Starting size detection for URI: $uri")

        // Try 1: AssetFileDescriptor
        Log.i(TAG, "📏 [SIZE-TRY-1] Attempting AssetFileDescriptor method...")
        try {
            val afdStartTime = System.currentTimeMillis()
            contentResolver.openAssetFileDescriptor(uri, "r")?.use { afd ->
                val size = afd.length
                val afdElapsed = System.currentTimeMillis() - afdStartTime
                Log.i(TAG, "📏 [SIZE-TRY-1] AFD opened in ${afdElapsed}ms, size: $size")

                if (size != AssetFileDescriptor.UNKNOWN_LENGTH && size > 0) {
                    Log.i(TAG, "✅ [SIZE-TRY-1] SUCCESS via AssetFileDescriptor: $size bytes")
                    return size
                } else {
                    Log.w(TAG, "⚠️ [SIZE-TRY-1] AFD returned invalid size: $size")
                }
            }
        } catch (e: Exception) {
            Log.w(TAG, "❌ [SIZE-TRY-1] AFD size detection failed: ${e.javaClass.simpleName}: ${e.message}")
        }

        // Try 2: DocumentFile
        Log.i(TAG, "📏 [SIZE-TRY-2] Attempting DocumentFile method...")
        try {
            val docStartTime = System.currentTimeMillis()
            val docFile = DocumentFile.fromSingleUri(contentResolver.context, uri)
            val docElapsed = System.currentTimeMillis() - docStartTime
            Log.i(TAG, "📏 [SIZE-TRY-2] DocumentFile created in ${docElapsed}ms")

            if (docFile != null) {
                Log.i(TAG, "📏 [SIZE-TRY-2] DocumentFile exists: ${docFile.exists()}")
                Log.i(TAG, "📏 [SIZE-TRY-2] DocumentFile name: ${docFile.name}")
                Log.i(TAG, "📏 [SIZE-TRY-2] DocumentFile type: ${docFile.type}")

                if (docFile.exists()) {
                    val sizeStartTime = System.currentTimeMillis()
                    val size = docFile.length()
                    val sizeElapsed = System.currentTimeMillis() - sizeStartTime
                    Log.i(TAG, "📏 [SIZE-TRY-2] Size retrieved in ${sizeElapsed}ms: $size")

                    if (size > 0) {
                        Log.i(TAG, "✅ [SIZE-TRY-2] SUCCESS via DocumentFile: $size bytes")
                        return size
                    } else {
                        Log.w(TAG, "⚠️ [SIZE-TRY-2] DocumentFile returned invalid size: $size")
                    }
                }
            } else {
                Log.w(TAG, "⚠️ [SIZE-TRY-2] DocumentFile is null")
            }
        } catch (e: Exception) {
            Log.w(TAG, "❌ [SIZE-TRY-2] DocumentFile size detection failed: ${e.javaClass.simpleName}: ${e.message}")
            e.printStackTrace()
        }

        // Try 3: Query SIZE column via Cursor
        Log.i(TAG, "📏 [SIZE-TRY-3] Attempting Cursor query method...")
        try {
            val queryStartTime = System.currentTimeMillis()
            contentResolver.query(
                uri,
                arrayOf(DocumentsContract.Document.COLUMN_SIZE),
                null,
                null,
                null
            )?.use { cursor ->
                val queryElapsed = System.currentTimeMillis() - queryStartTime
                Log.i(TAG, "📏 [SIZE-TRY-3] Query completed in ${queryElapsed}ms")
                Log.i(TAG, "📏 [SIZE-TRY-3] Cursor count: ${cursor.count}")

                if (cursor.moveToFirst()) {
                    Log.i(TAG, "📏 [SIZE-TRY-3] Cursor moved to first row")

                    val sizeIndex = cursor.getColumnIndex(DocumentsContract.Document.COLUMN_SIZE)
                    Log.i(TAG, "📏 [SIZE-TRY-3] Size column index: $sizeIndex")

                    if (sizeIndex >= 0) {
                        val size = cursor.getLong(sizeIndex)
                        Log.i(TAG, "📏 [SIZE-TRY-3] Size from cursor: $size")

                        if (size > 0) {
                            Log.i(TAG, "✅ [SIZE-TRY-3] SUCCESS via Cursor query: $size bytes")
                            return size
                        } else {
                            Log.w(TAG, "⚠️ [SIZE-TRY-3] Cursor returned invalid size: $size")
                        }
                    } else {
                        Log.w(TAG, "⚠️ [SIZE-TRY-3] Size column not found in cursor")
                    }
                } else {
                    Log.w(TAG, "⚠️ [SIZE-TRY-3] Cursor is empty")
                }
            } ?: run {
                Log.w(TAG, "⚠️ [SIZE-TRY-3] Query returned null cursor")
            }
        } catch (e: Exception) {
            Log.w(TAG, "❌ [SIZE-TRY-3] Cursor size detection failed: ${e.javaClass.simpleName}: ${e.message}")
            e.printStackTrace()
        }

        Log.e(TAG, "❌ [SIZE] ALL SIZE DETECTION METHODS FAILED!")
        return -1
    }

}

// Extension to get context from ContentResolver
private val ContentResolver.context: android.content.Context
    get() = MainActivity.getInstance()
        ?: throw IllegalStateException("Cannot get context")
