package com.burntpeanut.core

import android.Manifest
import android.content.pm.PackageManager
import android.os.Bundle
import android.os.Build
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import java.io.File
import java.security.MessageDigest
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

class MainActivity : AppCompatActivity() {
    private var nodeHandle: Long = 0L
    private val testPeerId = 1L
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
        CoreBridge.initRuntime(filesDir)
        ensureBlePermissions()
        BleTransportManager.start(this)
        val status = findViewById<TextView>(R.id.status)
        val logView = findViewById<TextView>(R.id.event_log)
        val inputHash = findViewById<EditText>(R.id.input_file_hash)

        findViewById<Button>(R.id.btn_create_node).setOnClickListener {
            val db = File(filesDir, "meshledger.db").absolutePath
            nodeHandle = CoreBridge.createNode(db)
            if (nodeHandle == 0L) {
                pushEvent(logView, status, "nativeCreateNode failed (handle 0)")
            } else {
                pushEvent(logView, status, "Node created (handle=$nodeHandle)")
            }
        }

        findViewById<Button>(R.id.btn_connect_peer).setOnClickListener {
            if (nodeHandle == 0L) {
                pushEvent(logView, status, "Create node first")
                return@setOnClickListener
            }
            BleTransportManager.connectPeer(testPeerId)
            pushEvent(logView, status, "Peer connected (id=$testPeerId)")
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
            inputHash.setText(lastSharedHashHex)
            pushEvent(logView, status, "Shared sample file. Hash=$lastSharedHashHex")
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
            val response = CoreBridge.nativeRequestFile(nodeHandle, hashBytes)
            if (response == null) {
                val code = CoreBridge.nativeRequestFileCode(nodeHandle, hashBytes)
                pushEvent(logView, status, "Request returned no payload (code=$code ${errorName(code)})")
            } else {
                pushEvent(logView, status, "Request returned ${response.size} bytes")
            }
        }

        findViewById<Button>(R.id.btn_send_bad_payload).setOnClickListener {
            if (nodeHandle == 0L) {
                pushEvent(logView, status, "Create node first")
                return@setOnClickListener
            }
            CoreBridge.nativeOnDataReceived(nodeHandle, testPeerId, byteArrayOf(0x01, 0x02, 0x03))
            pushEvent(logView, status, "Invalid payload injected; app stayed alive")
        }

        findViewById<Button>(R.id.btn_disconnect_peer).setOnClickListener {
            if (nodeHandle == 0L) {
                pushEvent(logView, status, "Create node first")
                return@setOnClickListener
            }
            BleTransportManager.disconnectPeer(testPeerId)
            pushEvent(logView, status, "Peer disconnected (id=$testPeerId)")
        }

        findViewById<Button>(R.id.btn_destroy_node).setOnClickListener {
            if (nodeHandle == 0L) {
                pushEvent(logView, status, "No active node")
                return@setOnClickListener
            }
            BleTransportManager.disconnectPeer(testPeerId)
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
        super.onDestroy()
        BleTransportManager.stop()
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

    private fun ensureBlePermissions() {
        val needed = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            arrayOf(
                Manifest.permission.BLUETOOTH_SCAN,
                Manifest.permission.BLUETOOTH_CONNECT,
                Manifest.permission.BLUETOOTH_ADVERTISE,
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
}
