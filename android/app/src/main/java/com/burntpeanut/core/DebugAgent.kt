package com.burntpeanut.core

import android.util.Log
import org.json.JSONObject
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import kotlin.concurrent.thread

/**
 * Fire-and-forget NDJSON: HTTP POST (adb reverse tcp:7718 tcp:7718) and/or local file
 * `adb pull /data/data/.../files/debug-69dc1f.ndjson`.
 */
object DebugAgent {
    /** USB + `adb reverse tcp:7718 tcp:7718` */
    private const val ENDPOINT_LOCAL =
        "http://127.0.0.1:7718/ingest/0915ce25-7778-4ec9-a91b-57799a9ea703"
    private const val INGEST_PATH = "/ingest/0915ce25-7778-4ec9-a91b-57799a9ea703"
    private const val SESSION = "69dc1f"
    private const val TAG = "DebugAgent69dc1f"

    private fun ingestEndpoint(): String {
        val h = BuildConfig.DEBUG_INGEST_HOST.trim()
        return if (h.isEmpty()) {
            ENDPOINT_LOCAL
        } else {
            "http://$h:7718$INGEST_PATH"
        }
    }

    // #region agent log
    fun emit(hypothesisId: String, location: String, message: String, data: Map<String, Any?>) {
        val jo = JSONObject()
        jo.put("sessionId", SESSION)
        jo.put("timestamp", System.currentTimeMillis())
        jo.put("hypothesisId", hypothesisId)
        jo.put("location", location)
        jo.put("message", message)
        val dataObj = JSONObject()
        for ((k, v) in data) {
            when (v) {
                null -> dataObj.put(k, JSONObject.NULL)
                is Number, is Boolean, is String -> dataObj.put(k, v)
                else -> dataObj.put(k, v.toString())
            }
        }
        jo.put("data", dataObj)
        val line = jo.toString()
        Log.i(TAG, line)
        thread(name = "debug-agent", isDaemon = true) {
            appendLocal(line)
            runCatching { postHttp(line) }.onFailure { Log.w(TAG, "ingest POST failed: ${it.message}") }
        }
    }

    private fun appendLocal(line: String) {
        val dirs = listOfNotNull(
            CoreBridge.appFilesDir.takeIf { it.isNotEmpty() },
            CoreBridge.appExternalFilesDir.takeIf { it.isNotEmpty() },
        )
        if (dirs.isEmpty()) return
        for (d in dirs) {
            runCatching {
                File(d, "debug-69dc1f.ndjson").appendText(line + "\n")
            }.onFailure { Log.w(TAG, "local append failed dir=$d: ${it.message}") }
        }
    }

    private fun postHttp(jsonBody: String) {
        val url = URL(ingestEndpoint())
        val conn = url.openConnection() as HttpURLConnection
        conn.requestMethod = "POST"
        conn.connectTimeout = 2000
        conn.readTimeout = 2000
        conn.doOutput = true
        conn.setRequestProperty("Content-Type", "application/json")
        conn.setRequestProperty("X-Debug-Session-Id", SESSION)
        conn.outputStream.use { os ->
            os.write(jsonBody.toByteArray(Charsets.UTF_8))
        }
        runCatching { conn.inputStream?.use { it.readBytes() } }
    }
    // #endregion
}
