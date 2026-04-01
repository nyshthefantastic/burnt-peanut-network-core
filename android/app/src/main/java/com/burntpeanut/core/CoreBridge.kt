package com.burntpeanut.core

import android.util.Log
import java.io.File

object CoreBridge {
    private const val TAG = "CoreBridge"

    @Volatile
    var currentNodeHandle: Long = 0L
        private set

    @Volatile
    var appFilesDir: String = ""
        private set

    init {
        System.loadLibrary("core")
        System.loadLibrary("mljni")
    }

    fun initRuntime(filesDir: File) {
        appFilesDir = filesDir.absolutePath
    }

    fun createNode(dbPath: String): Long {
        val handle = nativeCreateNode(dbPath)
        currentNodeHandle = handle
        Log.i(TAG, "createNode handle=$handle")
        return handle
    }

    fun destroyNode(handle: Long) {
        nativeDestroyNode(handle)
        if (currentNodeHandle == handle) currentNodeHandle = 0L
        Log.i(TAG, "destroyNode handle=$handle")
    }

    @JvmStatic
    external fun nativeCreateNode(dbPath: String): Long

    @JvmStatic
    external fun nativeDestroyNode(handle: Long)

    @JvmStatic
    external fun nativeOnPeerDiscovered(handle: Long, peerId: Long)

    @JvmStatic
    external fun nativeOnPeerConnected(handle: Long, peerId: Long)

    @JvmStatic
    external fun nativeOnPeerDisconnected(handle: Long, peerId: Long)

    @JvmStatic
    external fun nativeOnDataReceived(handle: Long, peerId: Long, data: ByteArray)

    @JvmStatic
    external fun nativeRequestFile(handle: Long, fileHash: ByteArray): ByteArray?

    @JvmStatic
    external fun nativeRequestFileCode(handle: Long, fileHash: ByteArray): Int

    @JvmStatic
    external fun nativeShareFile(handle: Long, fileData: ByteArray, fileName: String): Int
}
