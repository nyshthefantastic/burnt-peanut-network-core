package com.burntpeanut.core

import android.Manifest
import android.content.pm.PackageManager
import android.os.Bundle
import android.os.Build
import android.graphics.Color
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
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
        val status = findViewById<TextView>(R.id.status)
        val logView = findViewById<TextView>(R.id.event_log)
        val inputHash = findViewById<EditText>(R.id.input_file_hash)
        val inputChunkCount = findViewById<EditText>(R.id.input_chunk_count)
        val inputPeerAddress = findViewById<EditText>(R.id.input_peer_address)
        val peerStateView = findViewById<TextView>(R.id.peer_state)
        val peerListView = findViewById<TextView>(R.id.peer_list)

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
                pushEvent(logView, status, "nativeCreateNode failed (handle 0)")
            } else {
                BleTransportManager.rehydrateCorePeerLifecycle(nodeHandle)
                pushEvent(logView, status, "Node created (handle=$nodeHandle)")
            }
        }

        findViewById<Button>(R.id.btn_connect_peer).setOnClickListener {
            if (nodeHandle == 0L) {
                pushEvent(logView, status, "Create node first")
                return@setOnClickListener
            }
            BleTransportManager.start(this)
            renderPeers()
            pushEvent(logView, status, "Refreshed BLE scan; waiting for real peer")
        }

        findViewById<Button>(R.id.btn_connect_mac).setOnClickListener {
            if (nodeHandle == 0L) {
                pushEvent(logView, status, "Create node first")
                return@setOnClickListener
            }
            val address = inputPeerAddress.text.toString().trim()
            if (address.isEmpty()) {
                pushEvent(logView, status, "Enter peer MAC first (AA:BB:CC:DD:EE:FF)")
                return@setOnClickListener
            }
            val ok = BleTransportManager.connectToAddress(address)
            if (ok) {
                pushEvent(logView, status, "Manual connect requested for $address")
            } else {
                pushEvent(logView, status, "Manual connect failed for $address")
            }
        }

        findViewById<Button>(R.id.btn_share_sample).setOnClickListener {
            if (nodeHandle == 0L) {
                pushEvent(logView, status, "Create node first")
                return@setOnClickListener
            }
            val payload = "sample file from ${Build.MODEL} @ ${System.currentTimeMillis()}".toByteArray()
            val rc = CoreBridge.nativeShareFile(nodeHandle, payload, "sample.txt")
            if (rc != 0) {
                val space = filesDir.usableSpace
                pushEvent(
                    logView,
                    status,
                    "Share failed code=$rc (${errorName(rc)}), bytes=${payload.size}, free=${space}B. Check Logcat tags: ml_jni, NativeHooks"
                )
                return@setOnClickListener
            }
            val hash = MessageDigest.getInstance("SHA-256").digest(payload)
            lastSharedHashHex = hash.joinToString("") { "%02x".format(it) }
            val chunkSize = 64 * 1024
            val chunkCount = (payload.size + chunkSize - 1) / chunkSize
            inputHash.setText(lastSharedHashHex)
            inputChunkCount.setText(chunkCount.toString())
            pushEvent(logView, status, "Shared sample file. Hash=$lastSharedHashHex chunks=$chunkCount")
        }

        findViewById<Button>(R.id.btn_request_file).setOnClickListener {
            if (nodeHandle == 0L) {
                pushEvent(logView, status, "Create node first")
                return@setOnClickListener
            }
            val hashHex = inputHash.text.toString().trim().ifEmpty { lastSharedHashHex }
            if (hashHex.isEmpty()) {
                pushEvent(logView, status, "Enter/paste a file hash first")
                return@setOnClickListener
            }
            val hashBytes = hexToBytes(hashHex)
            if (hashBytes == null || hashBytes.isEmpty()) {
                pushEvent(logView, status, "Invalid hash hex")
                return@setOnClickListener
            }
            val chunkCount = inputChunkCount.text.toString().trim().toIntOrNull()
            val response = if (chunkCount != null && chunkCount > 0) {
                CoreBridge.nativeRequestFileWithChunkCount(nodeHandle, hashBytes, chunkCount)
            } else {
                CoreBridge.nativeRequestFile(nodeHandle, hashBytes)
            }
            if (response == null) {
                val code = CoreBridge.nativeRequestFileCode(nodeHandle, hashBytes)
                val hint = if (code == 2) " (hint: enter sender chunk count)" else ""
                pushEvent(logView, status, "Request returned no payload (code=$code ${errorName(code)})$hint")
            } else {
                pushEvent(logView, status, "Request returned ${response.size} bytes")
            }
        }

        findViewById<Button>(R.id.btn_reconstruct_file).setOnClickListener {
            val hashHex = inputHash.text.toString().trim().ifEmpty { lastSharedHashHex }
            if (hashHex.isEmpty()) {
                pushEvent(logView, status, "Enter/paste a file hash first")
                return@setOnClickListener
            }
            val chunkDir = File(filesDir, "chunks")
            val normalized = hashHex.lowercase(Locale.US).filterNot { it.isWhitespace() }
            val matching = chunkDir.listFiles()
                ?.count { it.isFile && it.name.startsWith("$normalized:") && it.name.endsWith(".bin") }
                ?: 0
            val out = reconstructReceivedFile(hashHex)
            if (out == null) {
                pushEvent(logView, status, "Reconstruct failed: no chunks found for hash")
            } else {
                pushEvent(logView, status, "Reconstructed file: ${out.absolutePath} (${out.length()} bytes)")
            }
        }

        findViewById<Button>(R.id.btn_send_bad_payload).setOnClickListener {
            if (nodeHandle == 0L) {
                pushEvent(logView, status, "Create node first")
                return@setOnClickListener
            }
            val peerId = BleTransportManager.connectedPeerIds().firstOrNull()
            if (peerId == null) {
                pushEvent(logView, status, "No connected BLE peer")
                return@setOnClickListener
            }
            CoreBridge.nativeOnDataReceived(nodeHandle, peerId, byteArrayOf(0x01, 0x02, 0x03))
            pushEvent(logView, status, "Invalid payload injected; app stayed alive")
        }

        findViewById<Button>(R.id.btn_disconnect_peer).setOnClickListener {
            renderPeers()
            pushEvent(logView, status, "Real BLE disconnect is automatic; moved away from fake test peer")
        }

        findViewById<Button>(R.id.btn_destroy_node).setOnClickListener {
            if (nodeHandle == 0L) {
                pushEvent(logView, status, "No active node")
                return@setOnClickListener
            }
            CoreBridge.destroyNode(nodeHandle)
            pushEvent(logView, status, "Node destroyed (handle=$nodeHandle)")
            nodeHandle = 0L
        }
    }

    override fun onDestroy() {
        if (nodeHandle != 0L) {
            CoreBridge.destroyNode(nodeHandle)
            nodeHandle = 0L
        }
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
        val outFile = File(outDir, "$hashHex.bin")

        FileOutputStream(outFile, false).use { fos ->
            for (chunkFile in chunkFiles) {
                fos.write(chunkFile.readBytes())
            }
            fos.flush()
        }
        return outFile
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
