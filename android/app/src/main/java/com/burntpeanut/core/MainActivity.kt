package com.burntpeanut.core

import android.os.Bundle
import android.widget.Button
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import java.io.File

class MainActivity : AppCompatActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)
        val status = findViewById<TextView>(R.id.status)
        findViewById<Button>(R.id.btn_smoke).setOnClickListener {
            val db = File(filesDir, "meshledger.db").absolutePath
            val h = CoreBridge.nativeCreateNode(db)
            if (h == 0L) {
                status.text = "nativeCreateNode failed (handle 0). Check Logcat / jniLibs."
            } else {
                status.text = "Node OK (handle=$h). Destroying…"
                CoreBridge.nativeDestroyNode(h)
                status.append("\nDestroyed.")
            }
        }
    }
}
