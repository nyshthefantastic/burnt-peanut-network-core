package com.burntpeanut.core

object CoreBridge {
    init {
        System.loadLibrary("core")
        System.loadLibrary("mljni")
    }

    @JvmStatic
    external fun nativeCreateNode(dbPath: String): Long

    @JvmStatic
    external fun nativeDestroyNode(handle: Long)
}
