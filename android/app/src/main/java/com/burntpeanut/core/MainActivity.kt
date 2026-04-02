package com.burntpeanut.core

import android.Manifest
import android.annotation.SuppressLint
import android.content.ContentValues
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Bundle
import android.os.Build
import android.os.Environment
import android.graphics.Color
import android.provider.MediaStore
import android.provider.OpenableColumns
import android.webkit.MimeTypeMap
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import androidx.core.content.FileProvider
import android.view.View
import android.widget.LinearLayout
import android.widget.ProgressBar
import java.io.File
import java.io.FileOutputStream
import java.security.MessageDigest
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

class MainActivity : AppCompatActivity() {
    private var nodeHandle: Long = 0L
    private val events = ArrayDeque<String>()
    private val clock = SimpleDateFormat("HH:mm:ss", Locale.US)
    private var lastSharedHashHex: String = ""
    private val blePermReqCode = 2001
    private lateinit var statusView: TextView
    private lateinit var logView: TextView
    private lateinit var inputHashView: EditText
    private lateinit var inputChunkCountView: EditText
    private lateinit var transferCard: LinearLayout
    private lateinit var transferTitle: TextView
    private lateinit var transferProgress: ProgressBar
    private lateinit var transferDetail: TextView
    private var receivedChunkCount = 0
    private var totalExpectedChunks = 0
    private val filePickerLauncher = registerForActivityResult(ActivityResultContracts.OpenDocument()) { uri ->
        if (uri == null) return@registerForActivityResult
        if (nodeHandle == 0L) {
            pushEvent(logView, statusView, "Create node first")
            return@registerForActivityResult
        }
        val fileName = resolveDisplayName(uri) ?: "selected_file.bin"
        val payload = try {
            contentResolver.openInputStream(uri)?.use { it.readBytes() }
        } catch (_: Exception) {
            null
        }
        if (payload == null || payload.isEmpty()) {
            pushEvent(logView, statusView, "Failed to read selected file")
            return@registerForActivityResult
        }
        val rc = CoreBridge.nativeShareFile(nodeHandle, payload, fileName)
        if (rc != 0) {
            val space = filesDir.usableSpace
            pushEvent(
                logView,
                statusView,
                "Share failed code=$rc (${errorName(rc)}), bytes=${payload.size}, free=${space}B. Check Logcat tags: ml_jni, NativeHooks"
            )
            return@registerForActivityResult
        }
        val chunkCount = updateSharedFileFields(payload)
        totalExpectedChunks = chunkCount
        transferCard.visibility = View.VISIBLE
        transferCard.setBackgroundColor(Color.parseColor("#FFF3E0"))
        transferTitle.text = getString(R.string.transfer_sending)
        transferProgress.progress = 0
        transferDetail.text = "Ready to send \"$fileName\" ($chunkCount chunks)"
        pushEvent(logView, statusView, "Shared file \"$fileName\". Hash=$lastSharedHashHex chunks=$chunkCount")
    }

    private fun pushEvent(logView: TextView, statusView: TextView, message: String) {
        val stamp = clock.format(Date())
        val line = "[$stamp] $message"
        if (events.size >= 20) events.removeFirst()
        events.addLast(line)
        logView.text = events.joinToString("\n")
        statusView.text = message
        val lower = message.lowercase(Locale.US)
        val isError = lower.contains("failed") || lower.contains("error") || lower.contains("invalid") || lower.contains("no payload")
        statusView.setTextColor(if (isError) Color.parseColor("#B00020") else Color.parseColor("#1B5E20"))
    }

    private fun errorName(code: Int): String = when (code) {
        0 -> "ML_OK"
        1 -> "ML_ERR_INVALID_ARG"
        2 -> "ML_ERR_NOT_FOUND"
        3 -> "ML_ERR_DB"
        4 -> "ML_ERR_CRYPTO"
        5 -> "ML_ERR_EXISTS"
        6 -> "ML_ERR_OVERFLOW"
        7 -> "ML_ERR_INTERNAL"
        else -> "UNKNOWN"
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)
        CoreBridge.initRuntime(filesDir, getExternalFilesDir(null))
        ensureBlePermissions()
        BleTransportManager.start(this)
        statusView = findViewById(R.id.status)
        logView = findViewById(R.id.event_log)
        inputHashView = findViewById(R.id.input_file_hash)
        inputChunkCountView = findViewById(R.id.input_chunk_count)
        val inputPeerAddress = findViewById<EditText>(R.id.input_peer_address)
        val peerStateView = findViewById<TextView>(R.id.peer_state)
        val peerListView = findViewById<TextView>(R.id.peer_list)
        transferCard = findViewById(R.id.transfer_status_card)
        transferTitle = findViewById(R.id.transfer_title)
        transferProgress = findViewById(R.id.transfer_progress)
        transferDetail = findViewById(R.id.transfer_detail)

        NativeHooks.setTransferEventsListener(object : NativeHooks.TransferEventsListener {
            override fun onChunkReceived(fileHashHex: String, chunkIndex: Int, byteCount: Int) {
                runOnUiThread {
                    receivedChunkCount++
                    transferCard.visibility = View.VISIBLE
                    transferCard.setBackgroundColor(Color.parseColor("#E3F2FD"))
                    transferTitle.text = getString(R.string.transfer_incoming)
                    val pct = if (totalExpectedChunks > 0) receivedChunkCount * 100 / totalExpectedChunks else 0
                    transferProgress.progress = pct
                    transferDetail.text = "Chunk $receivedChunkCount received (${byteCount}B) — ${fileHashHex.take(12)}…"
                }
            }

            override fun onProgress(peerId: Long, percent: Int) {
                runOnUiThread {
                    transferCard.visibility = View.VISIBLE
                    transferProgress.progress = percent
                    val direction = if (percent > 0 && receivedChunkCount == 0) "Sending" else "Receiving"
                    transferDetail.text = "$direction… $percent%"
                    if (receivedChunkCount == 0) {
                        transferCard.setBackgroundColor(Color.parseColor("#FFF3E0"))
                        transferTitle.text = getString(R.string.transfer_sending)
                    }
                }
            }

            override fun onComplete(peerId: Long, fileHashHex: String) {
                runOnUiThread {
                    transferCard.visibility = View.VISIBLE
                    transferCard.setBackgroundColor(Color.parseColor("#E8F5E9"))
                    transferProgress.progress = 100
                    transferTitle.text = "Transfer Complete"
                    transferDetail.text = "File received ($receivedChunkCount chunks) — ${fileHashHex.take(12)}…"
                    pushEvent(logView, statusView, "Transfer complete! hash=${fileHashHex.take(16)}… chunks=$receivedChunkCount")
                    receivedChunkCount = 0
                }
            }

            override fun onFailed(peerId: Long, errorCode: Int) {
                runOnUiThread {
                    transferCard.visibility = View.VISIBLE
                    transferCard.setBackgroundColor(Color.parseColor("#FFEBEE"))
                    transferTitle.text = "Transfer Failed"
                    transferDetail.text = "Error code $errorCode (${errorName(errorCode)})"
                    pushEvent(logView, statusView, "Transfer failed error=$errorCode (${errorName(errorCode)})")
                    receivedChunkCount = 0
                }
            }
        })

        fun renderPeers() {
            val ids = BleTransportManager.connectedPeerIds()
            val connected = ids.isNotEmpty()
            peerStateView.text = if (connected) getString(R.string.peer_state_connected) else getString(R.string.peer_state_disconnected)
            peerListView.text = if (!connected) {
                getString(R.string.peer_list_empty)
            } else {
                ids.joinToString(separator = "\n", prefix = "Connected:\n") { id ->
                    val addr = BleTransportManager.peerAddress(id) ?: "unknown"
                    "id=$id addr=$addr"
                }
            }
        }
        BleTransportManager.setPeerEventsListener(object : BleTransportManager.PeerEventsListener {
            override fun onPeersChanged() {
                runOnUiThread { renderPeers() }
            }
        })
        renderPeers()

        findViewById<Button>(R.id.btn_create_node).setOnClickListener {
            val db = File(filesDir, "meshledger.db").absolutePath
            nodeHandle = CoreBridge.createNode(db)
            if (nodeHandle == 0L) {
                pushEvent(logView, statusView, "nativeCreateNode failed (handle 0)")
            } else {
                BleTransportManager.rehydrateCorePeerLifecycle(nodeHandle)
                pushEvent(logView, statusView, "Node created (handle=$nodeHandle)")
            }
        }

        findViewById<Button>(R.id.btn_connect_peer).setOnClickListener {
            if (nodeHandle == 0L) {
                pushEvent(logView, statusView, "Create node first")
                return@setOnClickListener
            }
            BleTransportManager.start(this)
            renderPeers()
            pushEvent(logView, statusView, "Refreshed BLE scan; waiting for real peer")
        }

        findViewById<Button>(R.id.btn_connect_mac).setOnClickListener {
            if (nodeHandle == 0L) {
                pushEvent(logView, statusView, "Create node first")
                return@setOnClickListener
            }
            val address = inputPeerAddress.text.toString().trim()
            if (address.isEmpty()) {
                pushEvent(logView, statusView, "Enter peer MAC first (AA:BB:CC:DD:EE:FF)")
                return@setOnClickListener
            }
            val ok = BleTransportManager.connectToAddress(address)
            if (ok) {
                pushEvent(logView, statusView, "Manual connect requested for $address")
            } else {
                pushEvent(logView, statusView, "Manual connect failed for $address")
            }
        }

        findViewById<Button>(R.id.btn_share_file).setOnClickListener {
            if (nodeHandle == 0L) {
                pushEvent(logView, statusView, "Create node first")
                return@setOnClickListener
            }
            filePickerLauncher.launch(arrayOf("*/*"))
        }

        findViewById<Button>(R.id.btn_share_sample).setOnClickListener {
            if (nodeHandle == 0L) {
                pushEvent(logView, statusView, "Create node first")
                return@setOnClickListener
            }
            val payload = "sample file from ${Build.MODEL} @ ${System.currentTimeMillis()}".toByteArray()
            val rc = CoreBridge.nativeShareFile(nodeHandle, payload, "sample.txt")
            if (rc != 0) {
                val space = filesDir.usableSpace
                pushEvent(
                    logView,
                    statusView,
                    "Share failed code=$rc (${errorName(rc)}), bytes=${payload.size}, free=${space}B. Check Logcat tags: ml_jni, NativeHooks"
                )
                return@setOnClickListener
            }
            val chunkCount = updateSharedFileFields(payload)
            pushEvent(logView, statusView, "Shared sample file. Hash=$lastSharedHashHex chunks=$chunkCount")
        }

        findViewById<Button>(R.id.btn_request_file).setOnClickListener {
            if (nodeHandle == 0L) {
                pushEvent(logView, statusView, "Create node first")
                return@setOnClickListener
            }
            val hashHex = inputHashView.text.toString().trim().ifEmpty { lastSharedHashHex }
            if (hashHex.isEmpty()) {
                pushEvent(logView, statusView, "Enter/paste a file hash first")
                return@setOnClickListener
            }
            val hashBytes = hexToBytes(hashHex)
            if (hashBytes == null || hashBytes.isEmpty()) {
                pushEvent(logView, statusView, "Invalid hash hex")
                return@setOnClickListener
            }
            val chunkCount = inputChunkCountView.text.toString().trim().toIntOrNull()

            receivedChunkCount = 0
            totalExpectedChunks = chunkCount ?: 0
            transferCard.visibility = View.VISIBLE
            transferCard.setBackgroundColor(Color.parseColor("#E3F2FD"))
            transferTitle.text = getString(R.string.transfer_incoming)
            transferProgress.progress = 0
            transferDetail.text = "Requesting file… ${hashHex.take(12)}…"

            val response = if (chunkCount != null && chunkCount > 0) {
                CoreBridge.nativeRequestFileWithChunkCount(nodeHandle, hashBytes, chunkCount)
            } else {
                CoreBridge.nativeRequestFile(nodeHandle, hashBytes)
            }
            if (response == null) {
                val code = CoreBridge.nativeRequestFileCode(nodeHandle, hashBytes)
                val hint = if (code == 2) " (hint: enter sender chunk count)" else ""
                pushEvent(logView, statusView, "Request returned no payload (code=$code ${errorName(code)})$hint")
            } else {
                pushEvent(logView, statusView, "Request returned ${response.size} bytes")
            }
        }

        findViewById<Button>(R.id.btn_reconstruct_file).setOnClickListener {
            val hashHex = inputHashView.text.toString().trim().ifEmpty { lastSharedHashHex }
            if (hashHex.isEmpty()) {
                pushEvent(logView, statusView, "Enter/paste a file hash first")
                return@setOnClickListener
            }
            val out = reconstructReceivedFile(hashHex)
            if (out == null) {
                pushEvent(logView, statusView, "Reconstruct failed: no chunks found for hash")
                return@setOnClickListener
            }
            val savedUri = saveToDownloads(out)
            if (savedUri != null) {
                pushEvent(logView, statusView, "Saved to Downloads: ${out.name} (${out.length()} bytes)")
                openFile(savedUri, out.name)
            } else {
                val uri = FileProvider.getUriForFile(this, "${packageName}.fileprovider", out)
                pushEvent(logView, statusView, "Reconstructed: ${out.name} (${out.length()} bytes)")
                openFile(uri, out.name)
            }
        }

        findViewById<Button>(R.id.btn_send_bad_payload).setOnClickListener {
            if (nodeHandle == 0L) {
                pushEvent(logView, statusView, "Create node first")
                return@setOnClickListener
            }
            val peerId = BleTransportManager.connectedPeerIds().firstOrNull()
            if (peerId == null) {
                pushEvent(logView, statusView, "No connected BLE peer")
                return@setOnClickListener
            }
            CoreBridge.nativeOnDataReceived(nodeHandle, peerId, byteArrayOf(0x01, 0x02, 0x03))
            pushEvent(logView, statusView, "Invalid payload injected; app stayed alive")
        }

        findViewById<Button>(R.id.btn_disconnect_peer).setOnClickListener {
            renderPeers()
            pushEvent(logView, statusView, "Real BLE disconnect is automatic; moved away from fake test peer")
        }

        findViewById<Button>(R.id.btn_destroy_node).setOnClickListener {
            if (nodeHandle == 0L) {
                pushEvent(logView, statusView, "No active node")
                return@setOnClickListener
            }
            CoreBridge.destroyNode(nodeHandle)
            pushEvent(logView, statusView, "Node destroyed (handle=$nodeHandle)")
            nodeHandle = 0L
        }
    }

    override fun onDestroy() {
        if (nodeHandle != 0L) {
            CoreBridge.destroyNode(nodeHandle)
            nodeHandle = 0L
        }
        NativeHooks.setTransferEventsListener(null)
        BleTransportManager.setPeerEventsListener(null)
        super.onDestroy()
        BleTransportManager.stop()
    }

    override fun onStart() {
        super.onStart()
    }

    override fun onStop() {
        super.onStop()
    }

    private fun hexToBytes(hex: String): ByteArray? {
        val clean = hex.lowercase(Locale.US).filterNot { it.isWhitespace() }
        if (clean.length % 2 != 0) return null
        return try {
            ByteArray(clean.length / 2) { i ->
                clean.substring(i * 2, i * 2 + 2).toInt(16).toByte()
            }
        } catch (_: Exception) {
            null
        }
    }

    private fun updateSharedFileFields(payload: ByteArray): Int {
        val hash = MessageDigest.getInstance("SHA-256").digest(payload)
        lastSharedHashHex = hash.joinToString("") { "%02x".format(it) }
        val chunkSize = 64 * 1024
        val chunkCount = (payload.size + chunkSize - 1) / chunkSize
        inputHashView.setText(lastSharedHashHex)
        inputChunkCountView.setText(chunkCount.toString())
        return chunkCount
    }

    @SuppressLint("Range")
    private fun resolveDisplayName(uri: android.net.Uri): String? {
        return contentResolver.query(uri, null, null, null, null)?.use { cursor ->
            if (cursor.moveToFirst()) {
                val idx = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                if (idx >= 0) cursor.getString(idx) else null
            } else {
                null
            }
        }
    }

    private fun reconstructReceivedFile(hashHexInput: String): File? {
        val hashHex = hashHexInput.lowercase(Locale.US).filterNot { it.isWhitespace() }
        if (hashHex.isEmpty()) return null

        val chunkDir = File(filesDir, "chunks")
        if (!chunkDir.exists()) return null

        val chunkFiles = chunkDir.listFiles()
            ?.filter { it.isFile && it.name.startsWith("$hashHex:") && it.name.endsWith(".bin") }
            ?.sortedBy { file ->
                val indexText = file.name.removePrefix("$hashHex:").removeSuffix(".bin")
                indexText.toIntOrNull() ?: Int.MAX_VALUE
            }
            ?: emptyList()
        if (chunkFiles.isEmpty()) return null

        val outDir = File(filesDir, "received")
        if (!outDir.exists()) outDir.mkdirs()

        val header = chunkFiles.first().readBytes().take(16).toByteArray()
        val ext = detectExtension(header)
        val outFile = File(outDir, "$hashHex.$ext")

        FileOutputStream(outFile, false).use { fos ->
            for (chunkFile in chunkFiles) {
                fos.write(chunkFile.readBytes())
            }
            fos.flush()
        }
        return outFile
    }

    private fun detectExtension(header: ByteArray): String {
        if (header.size < 4) return "bin"
        val b = header
        if (b[0] == 0x89.toByte() && b[1] == 0x50.toByte() && b[2] == 0x4E.toByte() && b[3] == 0x47.toByte()) return "png"
        if (b[0] == 0xFF.toByte() && b[1] == 0xD8.toByte() && b[2] == 0xFF.toByte()) return "jpg"
        if (b[0] == 0x47.toByte() && b[1] == 0x49.toByte() && b[2] == 0x46.toByte()) return "gif"
        if (b[0] == 0x25.toByte() && b[1] == 0x50.toByte() && b[2] == 0x44.toByte() && b[3] == 0x46.toByte()) return "pdf"
        if (b[0] == 0x50.toByte() && b[1] == 0x4B.toByte() && b[2] == 0x03.toByte() && b[3] == 0x04.toByte()) return "zip"
        if (b[0] == 0x52.toByte() && b[1] == 0x49.toByte() && b[2] == 0x46.toByte() && b[3] == 0x46.toByte()) return "webp"
        if (header.size >= 12 && b[4] == 0x66.toByte() && b[5] == 0x74.toByte() && b[6] == 0x79.toByte() && b[7] == 0x70.toByte()) return "mp4"
        val text = header.all { it in 0x09..0x0D || it in 0x20..0x7E }
        if (text) return "txt"
        return "bin"
    }

    private fun saveToDownloads(file: File): android.net.Uri? {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) return null
        val ext = file.extension
        val mime = MimeTypeMap.getSingleton().getMimeTypeFromExtension(ext) ?: "application/octet-stream"
        val values = ContentValues().apply {
            put(MediaStore.Downloads.DISPLAY_NAME, file.name)
            put(MediaStore.Downloads.MIME_TYPE, mime)
            put(MediaStore.Downloads.RELATIVE_PATH, Environment.DIRECTORY_DOWNLOADS + "/BurntPeanut")
        }
        val uri = contentResolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values) ?: return null
        return try {
            contentResolver.openOutputStream(uri)?.use { os ->
                file.inputStream().use { it.copyTo(os) }
            }
            uri
        } catch (e: Exception) {
            runCatching { contentResolver.delete(uri, null, null) }
            null
        }
    }

    private fun openFile(uri: android.net.Uri, fileName: String) {
        val ext = fileName.substringAfterLast('.', "bin")
        val mime = MimeTypeMap.getSingleton().getMimeTypeFromExtension(ext) ?: "application/octet-stream"
        val intent = Intent(Intent.ACTION_VIEW).apply {
            setDataAndType(uri, mime)
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        if (intent.resolveActivity(packageManager) != null) {
            startActivity(intent)
        } else {
            Toast.makeText(this, "No app found to open .$ext files", Toast.LENGTH_SHORT).show()
        }
    }

    private fun ensureBlePermissions() {
        val needed = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            arrayOf(
                Manifest.permission.BLUETOOTH_SCAN,
                Manifest.permission.BLUETOOTH_CONNECT,
                Manifest.permission.BLUETOOTH_ADVERTISE,
                Manifest.permission.ACCESS_FINE_LOCATION,
            )
        } else {
            arrayOf(
                Manifest.permission.ACCESS_FINE_LOCATION,
                Manifest.permission.BLUETOOTH,
                Manifest.permission.BLUETOOTH_ADMIN,
            )
        }
        val missing = needed.filter {
            ContextCompat.checkSelfPermission(this, it) != PackageManager.PERMISSION_GRANTED
        }
        if (missing.isNotEmpty()) {
            ActivityCompat.requestPermissions(this, missing.toTypedArray(), blePermReqCode)
        }
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode == blePermReqCode) {
            val granted = grantResults.isNotEmpty() && grantResults.all { it == PackageManager.PERMISSION_GRANTED }
            if (granted) {
                BleTransportManager.start(this)
            }
        }
    }
}
