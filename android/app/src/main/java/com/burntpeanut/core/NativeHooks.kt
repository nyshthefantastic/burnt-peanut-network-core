package com.burntpeanut.core

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Log
import java.io.File
import java.security.InvalidKeyException
import java.security.KeyPair
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.Signature
import java.util.concurrent.ConcurrentHashMap

object NativeHooks {
    private const val TAG = "NativeHooks"
    private const val ANDROID_KEYSTORE = "AndroidKeyStore"
    private const val SIGNING_ALIAS = "burnt_peanut_mvp_ed25519"
    private const val ML_OK = 0
    private const val ML_ERR_INTERNAL = 7

    private val chunkStore = ConcurrentHashMap<String, ByteArray>()
    @Volatile
    private var cachedKeyPair: KeyPair? = null

    @JvmStatic
    fun onSend(peerId: Long, data: ByteArray): Int {
        Log.d(TAG, "send peer=$peerId bytes=${data.size}")
        val ok = BleTransportManager.send(peerId, data)
        // #region agent log
        DebugAgent.emit(
            "H12",
            "NativeHooks:onSend",
            "core->ble send",
            mapOf(
                "peerId" to peerId,
                "bytes" to data.size,
                "ok" to ok,
                "ble" to BleTransportManager.debugState().toString(),
            ),
        )
        // #endregion
        return if (ok) ML_OK else ML_ERR_INTERNAL
    }

    @JvmStatic
    fun onStartAdvertising(payload: ByteArray): Int {
        Log.d(TAG, "startAdvertising bytes=${payload.size}")
        return ML_OK
    }

    @JvmStatic
    fun onStopAdvertising(): Int = ML_OK
    @JvmStatic
    fun onStartScanning(): Int = ML_OK
    @JvmStatic
    fun onStopScanning(): Int = ML_OK
    @JvmStatic
    fun onDisconnect(peerId: Long): Int {
        Log.d(TAG, "disconnect peer=$peerId")
        return ML_OK
    }

    // Temporary software signing seam; Keystore migration can replace this body later.
    @JvmStatic
    fun onSignSecure(data: ByteArray): ByteArray? {
        return runCatching {
            signWithCurrentKey(data)
        }.recoverCatching {
            // Self-heal corrupted/incompatible keystore alias by rotating once.
            if (it is InvalidKeyException) {
                Log.w(TAG, "sign key invalid; rotating alias: ${it.message}")
                rotateSigningKey()
                signWithCurrentKey(data)
            } else {
                throw it
            }
        }.onFailure {
            Log.e(TAG, "onSignSecure failed: ${it.message}", it)
        }.getOrNull()
    }

    @JvmStatic
    fun onGetPublicKey(): ByteArray = runCatching { getSigningKeyPair().public.encoded }
        .onFailure { Log.e(TAG, "onGetPublicKey failed: ${it.message}", it) }
        .getOrElse { ByteArray(0) }
    @JvmStatic
    fun onGetAttestation(): ByteArray = ByteArray(0)
    @JvmStatic
    fun onHasSecureElement(): Boolean = false

    private fun chunkKey(fileHash: ByteArray, chunkIndex: Int): String {
        val hashHex = fileHash.joinToString("") { "%02x".format(it) }
        return "$hashHex:$chunkIndex"
    }

    @JvmStatic
    fun onWriteChunk(fileHash: ByteArray, chunkIndex: Int, data: ByteArray): Int {
        val key = chunkKey(fileHash, chunkIndex)
        return runCatching {
            chunkStore[key] = data
            val persisted = persistChunk(fileHash, chunkIndex, data)
            Log.d(TAG, "writeChunk ok key=$key bytes=${data.size} persisted=$persisted")
            // #region agent log
            DebugAgent.emit(
                "H13",
                "NativeHooks:onWriteChunk",
                "chunk persisted",
                mapOf(
                    "key" to key,
                    "bytes" to data.size,
                    "persisted" to persisted,
                ),
            )
            // #endregion
            ML_OK
        }.getOrElse {
            Log.e(TAG, "writeChunk failed key=$key bytes=${data.size}: ${it.message}", it)
            // #region agent log
            DebugAgent.emit(
                "H13",
                "NativeHooks:onWriteChunk",
                "chunk persist failed",
                mapOf("key" to key, "bytes" to data.size, "error" to (it.message ?: "unknown")),
            )
            // #endregion
            ML_ERR_INTERNAL
        }
    }

    @JvmStatic
    fun onReadChunk(fileHash: ByteArray, chunkIndex: Int): ByteArray? {
        return chunkStore[chunkKey(fileHash, chunkIndex)] ?: readPersistedChunk(fileHash, chunkIndex)
    }

    @JvmStatic
    fun onHasChunk(fileHash: ByteArray, chunkIndex: Int): Boolean {
        if (chunkStore.containsKey(chunkKey(fileHash, chunkIndex))) return true
        val f = chunkPath(fileHash, chunkIndex)
        return f.exists()
    }

    @JvmStatic
    fun onDeleteFile(fileHash: ByteArray): Int {
        val prefix = fileHash.joinToString("") { "%02x".format(it) } + ":"
        chunkStore.keys.removeIf { it.startsWith(prefix) }
        val dir = File(CoreBridge.appFilesDir, "chunks")
        if (dir.exists()) dir.listFiles()?.forEach { if (it.name.startsWith(prefix)) it.delete() }
        return ML_OK
    }

    @JvmStatic
    fun onAvailableSpace(): Long = File(CoreBridge.appFilesDir).usableSpace

    @JvmStatic
    fun onNotifyTransferProgress(peerId: Long, percent: Int) {
        Log.i(TAG, "progress peer=$peerId percent=$percent")
    }

    @JvmStatic
    fun onNotifyTransferComplete(peerId: Long, fileHash: ByteArray) {
        Log.i(TAG, "complete peer=$peerId fileHash=${fileHash.size}B")
    }

    @JvmStatic
    fun onNotifyTransferFailed(peerId: Long, errorCode: Int) {
        Log.w(TAG, "failed peer=$peerId error=$errorCode")
    }

    @JvmStatic
    fun onNotifyPeerVerified(peerId: Long, valid: Boolean) {
        Log.i(TAG, "peerVerified peer=$peerId valid=$valid")
    }

    @JvmStatic
    fun onNotifyForkDetected(devicePubkey: ByteArray) {
        Log.w(TAG, "forkDetected pubkey=${devicePubkey.size}B")
    }

    @JvmStatic
    fun onNotifyBalanceChanged(newBalance: Long) {
        Log.i(TAG, "balanceChanged value=$newBalance")
    }

    @JvmStatic
    fun onNotifyGossipReceived(peerId: Long) {
        Log.i(TAG, "gossipReceived peer=$peerId")
    }

    @JvmStatic
    fun onBridgeError(message: String) {
        Log.e(TAG, message)
    }

    private fun chunkDir(): File {
        val dir = File(CoreBridge.appFilesDir, "chunks")
        if (!dir.exists()) dir.mkdirs()
        return dir
    }

    private fun chunkPath(fileHash: ByteArray, chunkIndex: Int): File {
        val hashHex = fileHash.joinToString("") { "%02x".format(it) }
        return File(chunkDir(), "${hashHex}:${chunkIndex}.bin")
    }

    private fun persistChunk(fileHash: ByteArray, chunkIndex: Int, data: ByteArray): Boolean {
        return runCatching {
            chunkPath(fileHash, chunkIndex).writeBytes(data)
            true
        }.getOrElse {
            onBridgeError("persistChunk failed: ${it.message}")
            false
        }
    }

    private fun readPersistedChunk(fileHash: ByteArray, chunkIndex: Int): ByteArray? {
        val f = chunkPath(fileHash, chunkIndex)
        if (!f.exists()) return null
        return runCatching { f.readBytes() }.getOrNull()
    }

    private fun loadOrCreateSigningKeyPair(): KeyPair {
        val keyStore = KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }
        val existing = keyStore.getEntry(SIGNING_ALIAS, null) as? KeyStore.PrivateKeyEntry
        if (existing != null) {
            return KeyPair(existing.certificate.publicKey, existing.privateKey)
        }

        val generator = KeyPairGenerator.getInstance("Ed25519", ANDROID_KEYSTORE)
        val spec = KeyGenParameterSpec.Builder(
            SIGNING_ALIAS,
            KeyProperties.PURPOSE_SIGN or KeyProperties.PURPOSE_VERIFY,
        )
            .setDigests(KeyProperties.DIGEST_NONE)
            .build()
        generator.initialize(spec)
        return generator.generateKeyPair()
    }

    @Synchronized
    private fun getSigningKeyPair(): KeyPair {
        val existing = cachedKeyPair
        if (existing != null) return existing
        val created = loadOrCreateSigningKeyPair()
        cachedKeyPair = created
        return created
    }

    private fun signWithCurrentKey(data: ByteArray): ByteArray {
        // Do not force AndroidKeyStore provider for Signature lookup:
        // some devices expose keystore-backed Ed25519 keys but route signing
        // through a different provider implementation.
        val signer = Signature.getInstance("Ed25519")
        signer.initSign(getSigningKeyPair().private)
        signer.update(data)
        return signer.sign()
    }

    @Synchronized
    private fun rotateSigningKey() {
        val keyStore = KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }
        runCatching { keyStore.deleteEntry(SIGNING_ALIAS) }
        cachedKeyPair = loadOrCreateSigningKeyPair()
    }
}
