package com.burntpeanut.core

import android.Manifest
import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothDevice
import android.bluetooth.BluetoothGatt
import android.bluetooth.BluetoothGattCallback
import android.bluetooth.BluetoothGattCharacteristic
import android.bluetooth.BluetoothGattDescriptor
import android.bluetooth.BluetoothGattServer
import android.bluetooth.BluetoothGattServerCallback
import android.bluetooth.BluetoothGattService
import android.bluetooth.BluetoothManager
import android.bluetooth.BluetoothProfile
import android.bluetooth.le.AdvertiseCallback
import android.bluetooth.le.AdvertiseData
import android.bluetooth.le.AdvertiseSettings
import android.bluetooth.le.BluetoothLeAdvertiser
import android.bluetooth.le.BluetoothLeScanner
import android.bluetooth.le.ScanCallback
import android.bluetooth.le.ScanFilter
import android.bluetooth.le.ScanResult
import android.bluetooth.le.ScanSettings
import android.bluetooth.le.ScanCallback.SCAN_FAILED_ALREADY_STARTED
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.os.PowerManager
import android.location.LocationManager
import androidx.core.content.ContextCompat
import android.util.Log
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.util.UUID
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.CopyOnWriteArraySet
import java.util.concurrent.LinkedBlockingDeque
import java.util.concurrent.atomic.AtomicInteger
import android.os.Handler
import android.os.Looper
import android.os.ParcelUuid

/**
 * Real BLE transport for envelope byte exchange (GATT notify + write).
 */
object BleTransportManager {
    private const val TAG = "BleTransportManager"
    /** [BluetoothStatusCodes.ERROR_GATT_WRITE_REQUEST_BUSY] — only one client GATT op in flight. */
    private const val GATT_WRITE_REQUEST_BUSY = 201
    private const val GATT_WRITE_BUSY_RETRY_MS = 35L
    /** CCCD enable often completes with 133 ([BluetoothGatt.GATT_ERROR]) during link teardown; ignore and retry on live links only. */
    private const val CCCD_RETRY_DELAY_MS = 150L
    private const val CCCD_MAX_ATTEMPTS = 3
    /** Delay between GATT notifications so the ATT queue drains reliably (API 33+ pacing must not rely solely on [onNotificationSent]). */
    private const val NOTIFY_PACE_MS = 15L
    /** Delay after each central [BluetoothGatt.writeCharacteristic] success before starting the next; avoids flooding the peripheral ATT queue. */
    private const val WRITE_PACE_MS = 15L
    private val peers = CopyOnWriteArraySet<Long>()
    private const val FRAME_MAGIC: Short = 0x4250 // "BP"
    private const val FRAME_HEADER_SIZE = 8
    private const val MAX_CHUNK = 180
    private const val MAX_FRAME_PAYLOAD = MAX_CHUNK - FRAME_HEADER_SIZE
    private const val MANUFACTURER_ID = 0x1337
    private val MANUFACTURER_TAG = byteArrayOf(0x42, 0x50, 0x4E) // "BPN"

    private val SERVICE_UUID: UUID = UUID.fromString("8f4c1f20-8a41-44c8-a667-f5c4ac6f3010")
    private val CHAR_UUID: UUID = UUID.fromString("8f4c1f21-8a41-44c8-a667-f5c4ac6f3010")
    private val CCCD_UUID: UUID = UUID.fromString("00002902-0000-1000-8000-00805f9b34fb")

    private var appContext: Context? = null
    private var manager: BluetoothManager? = null
    private var adapter: BluetoothAdapter? = null
    private var advertiser: BluetoothLeAdvertiser? = null
    private var scanner: BluetoothLeScanner? = null
    private var gattServer: BluetoothGattServer? = null
    private var outboundGatt: BluetoothGatt? = null
    private var outboundChar: BluetoothGattCharacteristic? = null
    private var outboundDevice: BluetoothDevice? = null
    private var pendingConnectAddress: String? = null
    private var inboundDevice: BluetoothDevice? = null
    private val seq = AtomicInteger(1)
    private val inbound = mutableMapOf<Long, FrameAssembly>()
    private val inboundLock = Any()
    private val peerAddresses = mutableMapOf<Long, String>()
    private val seenScanAddresses = mutableSetOf<String>()
    private val mainHandler = Handler(Looper.getMainLooper())

    interface PeerEventsListener {
        fun onPeersChanged()
    }
    @Volatile
    private var peerEventsListener: PeerEventsListener? = null
    @Volatile
    private var started = false
    @Volatile
    private var lastScanKickMs = 0L
    @Volatile
    private var scanActive = false
    @Volatile
    private var scanBlockedUntilMs = 0L
    private val outboundWriteQueue = LinkedBlockingDeque<ByteArray>()
    @Volatile
    private var outboundWriteInFlight = false
    /** [kickOutboundWrite] dropped frame after non-GATT_SUCCESS; bounded re-try to avoid tight loops. */
    @Volatile
    private var consecutiveOutboundWriteFailures = 0
    /** CCCD write must finish before characteristic writes or the stack returns BUSY (201). */
    @Volatile
    private var outboundCccdReady = false
    /** Monotonic count of CCCD writes issued for this discovery cycle (cap [CCCD_MAX_ATTEMPTS]). */
    private var cccdWritesIssued = 0
    private val gattWriteBusyRetryRunnable = Runnable { kickOutboundWrite() }
    private val cccdRetryRunnable = Runnable { writeOutboundCccdNow() }
    /** True while a delayed [outboundWriteDrainRunnable] will run; blocks eager [kickOutboundWrite] during pacing. */
    @Volatile
    private var outboundWriteDrainPosted = false
    private val outboundWriteDrainRunnable = Runnable {
        outboundWriteDrainPosted = false
        kickOutboundWrite()
    }
    /** Serialized notify PDUs (back-to-back notifyCharacteristicChanged often drops on server). */
    private val notifyFrameQueue = ConcurrentLinkedQueue<ByteArray>()
    @Volatile
    private var notifyDrainPosted = false
    private val notifyChainLock = Any()
    private val notifyChainRunnable = object : Runnable {
        override fun run() {
            val server = gattServer ?: run { endNotifyChain(); return }
            val device = inboundDevice ?: run { endNotifyChain(); return }
            val frame = notifyFrameQueue.poll() ?: run { endNotifyChain(); return }
            val service = server.getService(SERVICE_UUID) ?: run { endNotifyChain(); return }
            val characteristic = service.getCharacteristic(CHAR_UUID) ?: run { endNotifyChain(); return }
            characteristic.value = frame
            server.notifyCharacteristicChanged(device, characteristic, false)
            if (notifyFrameQueue.isEmpty()) {
                endNotifyChain()
            } else {
                mainHandler.postDelayed(this, NOTIFY_PACE_MS)
            }
        }

        private fun endNotifyChain() {
            synchronized(notifyChainLock) { notifyDrainPosted = false }
        }
    }
    @Volatile
    private var servicesDiscoverRetries = 0
    /** Negotiated ATT MTU (default 23 until [BluetoothGattCallback.onMtuChanged]). */
    @Volatile
    private var lastAttMtu = 23
    /**
     * Outbound client must not call [BluetoothGatt.discoverServices] until [BluetoothGatt.requestMtu]
     * completes; overlapping ops leave CCCD writes un-acked until link teardown (status 133).
     */
    @Volatile
    private var pendingOutboundServiceDiscover = false

    private data class FrameAssembly(
        val total: Int,
        val parts: MutableMap<Int, ByteArray> = mutableMapOf(),
    )

    fun start(context: Context) {
        if (started) {
            Log.i(TAG, "start transport (already started)")
            appContext = context.applicationContext
            manager = context.getSystemService(BluetoothManager::class.java)
            adapter = manager?.adapter
            advertiser = adapter?.bluetoothLeAdvertiser
            scanner = adapter?.bluetoothLeScanner
            if (adapter?.isEnabled == true && hasBlePermissions(context)) {
                // Re-ensure peripherals in case OS/vendor stack dropped state.
                if (gattServer == null) startGattServer()
                startAdvertising()
            }
            val hasActiveLink = peers.isNotEmpty() || outboundGatt != null || inboundDevice != null
            if (!hasActiveLink) {
                // Only resume discovery when no active BLE link exists.
                startScanning(forceRestart = true)
            }
            return
        }
        appContext = context.applicationContext
        manager = context.getSystemService(BluetoothManager::class.java)
        adapter = manager?.adapter
        advertiser = adapter?.bluetoothLeAdvertiser
        scanner = adapter?.bluetoothLeScanner
        Log.i(TAG, "start transport")
        if (adapter?.isEnabled != true || !hasBlePermissions(context)) {
            Log.w(TAG, "BLE unavailable or permissions missing")
            return
        }
        startGattServer()
        startAdvertising()
        startScanning(forceRestart = false)
        started = true
    }

    fun setPeerEventsListener(listener: PeerEventsListener?) {
        peerEventsListener = listener
    }

    fun connectedPeerIds(): List<Long> = peers.toList().sorted()

    /**
     * After [CoreBridge.createNode], replay discover+connected for every live BLE link.
     * Links that came up while handle was 0 never reached the core (see gatt server/client
     * connect paths). A fresh node also has empty transports, so this is safe for first create.
     * Call only once per new node from MainActivity — not on every UI tick — to avoid duplicate
     * handshakes on the same node.
     */
    fun rehydrateCorePeerLifecycle(nodeHandle: Long) {
        if (nodeHandle == 0L) return
        val ids = connectedPeerIds()
        if (ids.isEmpty()) {
            Log.d(TAG, "rehydrateCorePeerLifecycle: no connected peers")
            return
        }
        for (pid in ids) {
            CoreBridge.nativeOnPeerDiscovered(nodeHandle, pid)
            CoreBridge.nativeOnPeerConnected(nodeHandle, pid)
            Log.i(TAG, "rehydrateCorePeerLifecycle peerId=$pid")
        }
        peerEventsListener?.onPeersChanged()
    }

    fun peerAddress(peerId: Long): String? = synchronized(inboundLock) { peerAddresses[peerId] }

    fun debugState(): Map<String, Any> = mapOf(
        "started" to started,
        "peers" to peers.size,
        "hasOutboundGatt" to (outboundGatt != null),
        "hasOutboundChar" to (outboundChar != null),
        "outboundCccdReady" to outboundCccdReady,
        "hasInboundDevice" to (inboundDevice != null),
        "lastAttMtu" to lastAttMtu,
        "notifyQ" to notifyFrameQueue.size,
        "writeQ" to outboundWriteQueue.size,
    )

    fun stop() {
        Log.i(TAG, "stop transport")
        started = false
        scanActive = false
        scanBlockedUntilMs = 0L
        peers.clear()
        synchronized(inboundLock) { seenScanAddresses.clear() }
        runCatching { scanner?.stopScan(scanCallback) }
        runCatching { advertiser?.stopAdvertising(advertiseCallback) }
        runCatching { outboundGatt?.disconnect(); outboundGatt?.close() }
        outboundGatt = null
        outboundChar = null
        outboundDevice = null
        inboundDevice = null
        runCatching { gattServer?.close() }
        gattServer = null
        notifyFrameQueue.clear()
        synchronized(notifyChainLock) { notifyDrainPosted = false }
        mainHandler.removeCallbacks(notifyChainRunnable)
        mainHandler.removeCallbacks(gattWriteBusyRetryRunnable)
        mainHandler.removeCallbacks(cccdRetryRunnable)
        mainHandler.removeCallbacks(outboundWriteDrainRunnable)
        outboundWriteDrainPosted = false
        outboundWriteQueue.clear()
        outboundWriteInFlight = false
        consecutiveOutboundWriteFailures = 0
        outboundCccdReady = false
    }

    fun connectPeer(peerId: Long) {
        // Legacy no-op: keep API stable but avoid fake/manual peer injection.
        appContext?.let { if (hasBlePermissions(it)) startScanning(forceRestart = false) }
    }

    fun connectToAddress(address: String): Boolean {
        val ctx = appContext ?: return false
        if (!hasBlePermissions(ctx)) return false
        val normalized = address.trim().uppercase()
        if (!normalized.matches(Regex("([0-9A-F]{2}:){5}[0-9A-F]{2}"))) {
            Log.w(TAG, "connectToAddress invalid address=$address")
            return false
        }
        val btAdapter = adapter ?: return false
        return try {
            val device = btAdapter.getRemoteDevice(normalized)
            if (outboundGatt != null && outboundDevice?.address == normalized) return true
            pendingConnectAddress = normalized
            val gatt = device.connectGatt(ctx, false, gattCallback, BluetoothDevice.TRANSPORT_LE)
            outboundGatt = gatt
            synchronized(inboundLock) { peerAddresses[peerIdFromAddress(normalized)] = normalized }
            Log.i(TAG, "manual connect requested addr=$normalized")
            true
        } catch (t: Throwable) {
            Log.e(TAG, "manual connect failed addr=$normalized: ${t.message}", t)
            false
        }
    }

    fun disconnectPeer(peerId: Long) {
        // Legacy no-op: real disconnect events come from BLE callbacks.
    }

    fun deliverInbound(peerId: Long, data: ByteArray) {
        val handle = CoreBridge.currentNodeHandle
        if (handle != 0L) {
            Log.d(TAG, "deliverInbound peer=$peerId bytes=${data.size}")
            CoreBridge.nativeOnDataReceived(handle, peerId, data)
        }
    }

    private fun onBleFrame(peerId: Long, frame: ByteArray) {
        if (frame.size < FRAME_HEADER_SIZE) return
        val buf = ByteBuffer.wrap(frame).order(ByteOrder.BIG_ENDIAN)
        val magic = buf.short
        if (magic != FRAME_MAGIC) return
        val messageId = buf.short.toInt() and 0xFFFF
        val total = buf.short.toInt() and 0xFFFF
        val index = buf.short.toInt() and 0xFFFF
        if (total <= 0 || index < 0 || index >= total) return
        val payload = frame.copyOfRange(FRAME_HEADER_SIZE, frame.size)
        val key = (peerId shl 16) xor messageId.toLong()
        val complete: ByteArray? = synchronized(inboundLock) {
            val entry = inbound.getOrPut(key) { FrameAssembly(total = total) }
            entry.parts[index] = payload
            if (entry.parts.size == entry.total) {
                val ordered = ByteArray(entry.parts.values.sumOf { it.size })
                var offset = 0
                for (i in 0 until entry.total) {
                    val part = entry.parts[i] ?: return@synchronized null
                    System.arraycopy(part, 0, ordered, offset, part.size)
                    offset += part.size
                }
                inbound.remove(key)
                ordered
            } else {
                null
            }
        }
        if (complete != null) {
            deliverInbound(peerId, complete)
        }
    }

    /** Max application payload per frame so full PDU fits in ATT (mtu - 3). */
    private fun effectiveMaxFramePayload(): Int {
        val maxPdu = (lastAttMtu - 3).coerceAtLeast(FRAME_HEADER_SIZE + 1)
        val cap = maxPdu - FRAME_HEADER_SIZE
        return minOf(MAX_FRAME_PAYLOAD, maxOf(1, cap))
    }

    private fun makeFrames(data: ByteArray): List<ByteArray> {
        if (data.isEmpty()) return emptyList()
        val parts = chunk(data, effectiveMaxFramePayload())
        val messageId = seq.getAndIncrement() and 0xFFFF
        val total = parts.size
        return parts.mapIndexed { idx, payload ->
            val out = ByteArray(FRAME_HEADER_SIZE + payload.size)
            val hdr = ByteBuffer.wrap(out).order(ByteOrder.BIG_ENDIAN)
            hdr.putShort(FRAME_MAGIC)
            hdr.putShort(messageId.toShort())
            hdr.putShort(total.toShort())
            hdr.putShort(idx.toShort())
            System.arraycopy(payload, 0, out, FRAME_HEADER_SIZE, payload.size)
            out
        }
    }

    fun send(peerId: Long, data: ByteArray): Boolean {
        val gatt = outboundGatt
        val ch = outboundChar
        if (gatt != null && ch != null && outboundDevice != null) {
            val frames = makeFrames(data)
            for (frame in frames) outboundWriteQueue.offerLast(frame)
            kickOutboundWrite()
            return true
        }
        if (gatt != null && outboundDevice != null && ch == null) {
            Log.w(TAG, "send queued but service not ready yet peer=$peerId bytes=${data.size} q=${outboundWriteQueue.size}")
            val frames = makeFrames(data)
            for (frame in frames) outboundWriteQueue.offerLast(frame)
            return true
        }

        // If we have a central connected to our server, push notify (serialized; no tight loop).
        val server = gattServer
        val device = inboundDevice
        if (server != null && device != null) {
            server.getService(SERVICE_UUID)?.getCharacteristic(CHAR_UUID) ?: return false
            val frames = makeFrames(data)
            for (frame in frames) notifyFrameQueue.add(frame)
            scheduleNotifyDrain()
            return true
        }
        return false
    }

    private fun scheduleNotifyDrain() {
        mainHandler.post {
            synchronized(notifyChainLock) {
                if (notifyDrainPosted) return@post
                notifyDrainPosted = true
            }
            mainHandler.post(notifyChainRunnable)
        }
    }

    /** Subscribe + write CCCD for current [outboundGatt]/[outboundChar]. No-op if GATT closed. */
    private fun writeOutboundCccdNow() {
        val gatt = outboundGatt ?: return
        val characteristic = outboundChar ?: return
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S && appContext?.let { hasBlePermissions(it) } != true) return
        @Suppress("DEPRECATION")
        gatt.setCharacteristicNotification(characteristic, true)
        val desc = characteristic.getDescriptor(CCCD_UUID) ?: run {
            outboundCccdReady = true
            kickOutboundWrite()
            return
        }
        desc.value = BluetoothGattDescriptor.ENABLE_NOTIFICATION_VALUE
        val writeStatus = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            gatt.writeDescriptor(desc, BluetoothGattDescriptor.ENABLE_NOTIFICATION_VALUE)
        } else {
            @Suppress("DEPRECATION")
            if (gatt.writeDescriptor(desc)) BluetoothGatt.GATT_SUCCESS else BluetoothGatt.GATT_FAILURE
        }
    }

    private fun cancelOutboundWritePacing() {
        mainHandler.removeCallbacks(outboundWriteDrainRunnable)
        outboundWriteDrainPosted = false
    }

    private fun kickOutboundWrite() {
        if (Looper.myLooper() != mainHandler.looper) {
            mainHandler.post { kickOutboundWrite() }
            return
        }
        if (!outboundCccdReady) return
        val gatt = outboundGatt ?: return
        val ch = outboundChar ?: return
        if (outboundWriteInFlight) return
        if (outboundWriteDrainPosted) return
        val next = outboundWriteQueue.pollFirst() ?: return
        outboundWriteInFlight = true
        val status = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            gatt.writeCharacteristic(ch, next, BluetoothGattCharacteristic.WRITE_TYPE_DEFAULT)
        } else {
            @Suppress("DEPRECATION")
            ch.writeType = BluetoothGattCharacteristic.WRITE_TYPE_DEFAULT
            ch.value = next
            if (gatt.writeCharacteristic(ch)) BluetoothGatt.GATT_SUCCESS else BluetoothGatt.GATT_FAILURE
        }
        val ok = status == BluetoothGatt.GATT_SUCCESS
        if (!ok) {
            outboundWriteInFlight = false
            val busy = status == GATT_WRITE_REQUEST_BUSY
            if (busy) {
                Log.w(
                    TAG,
                    "gatt write busy (201); retry delayed ms=$GATT_WRITE_BUSY_RETRY_MS bytes=${next.size} q=${outboundWriteQueue.size}",
                )
                outboundWriteQueue.offerFirst(next)
                mainHandler.removeCallbacks(gattWriteBusyRetryRunnable)
                mainHandler.postDelayed(gattWriteBusyRetryRunnable, GATT_WRITE_BUSY_RETRY_MS)
                return
            }
            consecutiveOutboundWriteFailures++
            Log.w(
                TAG,
                "gatt write rejected status=$status bytes=${next.size} q=${outboundWriteQueue.size} streak=$consecutiveOutboundWriteFailures",
            )
            if (consecutiveOutboundWriteFailures >= 64) {
                Log.e(TAG, "gatt write abandoning outbound queue after repeated failures")
                outboundWriteQueue.clear()
                consecutiveOutboundWriteFailures = 0
                return
            }
            outboundWriteQueue.offerFirst(next)
            mainHandler.post { kickOutboundWrite() }
        } else {
            consecutiveOutboundWriteFailures = 0
            Log.d(TAG, "gatt write started bytes=${next.size} q=${outboundWriteQueue.size}")
        }
    }

    private fun startGattServer() {
        val ctx = appContext ?: return
        val mgr = manager ?: return
        if (!hasBlePermissions(ctx)) return
        gattServer = mgr.openGattServer(ctx, gattServerCallback)
        val service = BluetoothGattService(SERVICE_UUID, BluetoothGattService.SERVICE_TYPE_PRIMARY)
        val characteristic = BluetoothGattCharacteristic(
            CHAR_UUID,
            BluetoothGattCharacteristic.PROPERTY_WRITE or
                BluetoothGattCharacteristic.PROPERTY_WRITE_NO_RESPONSE or
                BluetoothGattCharacteristic.PROPERTY_NOTIFY or
                BluetoothGattCharacteristic.PROPERTY_READ,
            BluetoothGattCharacteristic.PERMISSION_READ or
                BluetoothGattCharacteristic.PERMISSION_WRITE,
        )
        val cccd = BluetoothGattDescriptor(CCCD_UUID, BluetoothGattDescriptor.PERMISSION_READ or BluetoothGattDescriptor.PERMISSION_WRITE)
        characteristic.addDescriptor(cccd)
        service.addCharacteristic(characteristic)
        gattServer?.addService(service)
    }

    private fun startAdvertising() {
        val ctx = appContext ?: return
        if (!hasBlePermissions(ctx)) return
        runCatching { advertiser?.stopAdvertising(advertiseCallback) }
        val settings = AdvertiseSettings.Builder()
            .setAdvertiseMode(AdvertiseSettings.ADVERTISE_MODE_LOW_LATENCY)
            .setConnectable(true)
            .setTxPowerLevel(AdvertiseSettings.ADVERTISE_TX_POWER_MEDIUM)
            .build()
        // Keep primary ADV packet tiny for better compatibility.
        val data = AdvertiseData.Builder()
            .addManufacturerData(MANUFACTURER_ID, MANUFACTURER_TAG)
            .addServiceUuid(ParcelUuid(SERVICE_UUID))
            .setIncludeDeviceName(false)
            .build()
        // Put UUID into scan response so scanners can still match by service.
        val scanResponse = AdvertiseData.Builder()
            .addServiceUuid(ParcelUuid(SERVICE_UUID))
            .setIncludeDeviceName(false)
            .build()
        try {
            advertiser?.startAdvertising(settings, data, scanResponse, advertiseCallback)
        } catch (se: SecurityException) {
            Log.e(TAG, "startAdvertising SecurityException: ${se.message}", se)
        }
    }

    private fun startScanning(forceRestart: Boolean) {
        val ctx = appContext ?: return
        if (!hasBlePermissions(ctx)) return
        val now = System.currentTimeMillis()
        if (forceRestart && scanActive) {
            runCatching { scanner?.stopScan(scanCallback) }
            scanActive = false
        } else if (scanActive) {
            return
        }
        if (now < scanBlockedUntilMs) {
            Log.w(TAG, "scan temporarily blocked; retry in ${scanBlockedUntilMs - now}ms")
            return
        }
        if (now - lastScanKickMs < 2500L && !forceRestart) {
            return
        }
        lastScanKickMs = now
        logLocationState(ctx)
        val filters = listOf(
            ScanFilter.Builder()
                .setServiceUuid(ParcelUuid(SERVICE_UUID))
                .build(),
            ScanFilter.Builder()
                .setManufacturerData(MANUFACTURER_ID, MANUFACTURER_TAG)
                .build(),
        )
        val settingsBuilder = ScanSettings.Builder()
            .setScanMode(ScanSettings.SCAN_MODE_LOW_LATENCY)
            .setReportDelay(0L)
        val settings = settingsBuilder.build()
        val pm = ctx.getSystemService(PowerManager::class.java)
        val interactive = pm?.isInteractive == true
        try {
            // Use broad scan while interactive to avoid vendor-specific filter misses.
            if (interactive) {
                scanner?.startScan(scanCallback)
            } else {
                scanner?.startScan(filters, settings, scanCallback)
            }
            scanActive = true
            Log.i(TAG, "scan started")
        } catch (se: SecurityException) {
            Log.e(TAG, "startScan SecurityException: ${se.message}", se)
        }
    }

    private val advertiseCallback = object : AdvertiseCallback() {
        override fun onStartSuccess(settingsInEffect: AdvertiseSettings) {
            Log.i(TAG, "advertising started")
        }

        override fun onStartFailure(errorCode: Int) {
            Log.e(TAG, "advertising failed code=$errorCode")
        }
    }

    private val scanCallback: ScanCallback = object : ScanCallback() {
        override fun onScanResult(callbackType: Int, result: ScanResult) {
            val device = result.device ?: return
            val address = device.address ?: return
            val advertisesService = result.scanRecord
                ?.serviceUuids
                ?.any { it.uuid == SERVICE_UUID } == true
            val hasTag = result.scanRecord
                ?.getManufacturerSpecificData(MANUFACTURER_ID)
                ?.contentEquals(MANUFACTURER_TAG) == true
            if (!advertisesService && !hasTag) return
            synchronized(inboundLock) {
                if (seenScanAddresses.add(address)) {
                    val uuids = result.scanRecord?.serviceUuids?.joinToString { it.uuid.toString() } ?: "none"
                    Log.i(TAG, "scan result addr=$address rssi=${result.rssi} uuids=$uuids tag=$hasTag")
                }
            }
            if (peers.isNotEmpty()) return
            if (outboundGatt != null) return
            if (pendingConnectAddress == address) return
            val localAddr = adapter?.address?.uppercase()
            if (!localAddr.isNullOrBlank() && localAddr != "02:00:00:00:00:00" && localAddr >= address) {
                // Deterministic tie-breaker to avoid both phones initiating at once.
                return
            }
            val ctx = appContext ?: return
            if (!hasBlePermissions(ctx)) return
            pendingConnectAddress = address
            val peerId = peerIdFromAddress(address)
            val handle = CoreBridge.currentNodeHandle
            if (handle != 0L) {
                CoreBridge.nativeOnPeerDiscovered(handle, peerId)
            }
            synchronized(inboundLock) { peerAddresses[peerId] = address }
            peerEventsListener?.onPeersChanged()
            Log.i(TAG, "scan discovered addr=$address peerId=$peerId, connecting...")
            outboundGatt = device.connectGatt(ctx, false, gattCallback, BluetoothDevice.TRANSPORT_LE)
        }

        override fun onScanFailed(errorCode: Int) {
            scanActive = false
            if (errorCode != SCAN_FAILED_ALREADY_STARTED) {
                Log.e(TAG, "scan failed code=$errorCode")
            }
            // Avoid repeated startScan attempts after controller rate-limit.
            if (errorCode == 6) {
                scanBlockedUntilMs = System.currentTimeMillis() + 30_000L
            }
        }

        override fun onBatchScanResults(results: MutableList<ScanResult>) {
            if (results.isNotEmpty()) {
                Log.i(TAG, "batch scan results count=${results.size}")
            }
            for (result in results) {
                onScanResult(ScanSettings.CALLBACK_TYPE_ALL_MATCHES, result)
            }
        }
    }

    private val gattCallback: BluetoothGattCallback = object : BluetoothGattCallback() {
        override fun onConnectionStateChange(gatt: BluetoothGatt, status: Int, newState: Int) {
            val handle = CoreBridge.currentNodeHandle
            val peerId = peerIdFromAddress(gatt.device.address ?: "")
            pendingConnectAddress = null
            if (newState == BluetoothProfile.STATE_CONNECTED) {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S && appContext?.let { hasBlePermissions(it) } != true) return
                outboundDevice = gatt.device
                servicesDiscoverRetries = 0
                lastAttMtu = 23
                outboundCccdReady = false
                pendingOutboundServiceDiscover = true
                runCatching { gatt.requestConnectionPriority(BluetoothGatt.CONNECTION_PRIORITY_HIGH) }
                runCatching { gatt.requestMtu(247) }
                // Avoid scan churn while an active link exists.
                try {
                    scanner?.stopScan(scanCallback)
                } catch (_: Throwable) {
                }
                scanActive = false
                mainHandler.removeCallbacks(gattWriteBusyRetryRunnable)
                mainHandler.removeCallbacks(cccdRetryRunnable)
                cancelOutboundWritePacing()
                cccdWritesIssued = 0
                outboundWriteQueue.clear()
                outboundWriteInFlight = false
                consecutiveOutboundWriteFailures = 0
                peers.add(peerId)
                synchronized(inboundLock) { peerAddresses[peerId] = gatt.device.address ?: "unknown" }
                if (handle != 0L) {
                    CoreBridge.nativeOnPeerDiscovered(handle, peerId)
                    CoreBridge.nativeOnPeerConnected(handle, peerId)
                }
                peerEventsListener?.onPeersChanged()
                Log.i(TAG, "gatt connected addr=${gatt.device.address} peerId=$peerId")
            } else if (newState == BluetoothProfile.STATE_DISCONNECTED) {
                peers.remove(peerId)
                if (handle != 0L) CoreBridge.nativeOnPeerDisconnected(handle, peerId)
                runCatching { gatt.close() }
                if (outboundGatt === gatt) outboundGatt = null
                if (outboundDevice?.address == gatt.device.address) outboundDevice = null
                outboundChar = null
                outboundWriteQueue.clear()
                outboundWriteInFlight = false
                consecutiveOutboundWriteFailures = 0
                outboundCccdReady = false
                pendingOutboundServiceDiscover = false
                mainHandler.removeCallbacks(gattWriteBusyRetryRunnable)
                mainHandler.removeCallbacks(cccdRetryRunnable)
                cancelOutboundWritePacing()
                cccdWritesIssued = 0
                val hasActiveLink = peers.isNotEmpty() || outboundGatt != null || inboundDevice != null
                if (!hasActiveLink) {
                    appContext?.let { if (hasBlePermissions(it)) startScanning(forceRestart = false) }
                } else {
                }
                peerEventsListener?.onPeersChanged()
                Log.w(TAG, "gatt disconnected addr=${gatt.device.address} status=$status")
            }
        }

        override fun onServicesDiscovered(gatt: BluetoothGatt, status: Int) {
            Log.i(TAG, "services discovered status=$status")
            if (status != BluetoothGatt.GATT_SUCCESS && servicesDiscoverRetries < 1) {
                servicesDiscoverRetries++
                Log.w(TAG, "services discovery failed; retrying once")
                gatt.discoverServices()
                return
            }
            val service = gatt.getService(SERVICE_UUID) ?: return
            val characteristic = service.getCharacteristic(CHAR_UUID) ?: return
            outboundChar = characteristic
            cccdWritesIssued = 0
            mainHandler.removeCallbacks(cccdRetryRunnable)
            Log.i(TAG, "outbound characteristic ready; awaiting CCCD before writes q=${outboundWriteQueue.size}")
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S && appContext?.let { hasBlePermissions(it) } != true) return
            val desc = characteristic.getDescriptor(CCCD_UUID)
            val gattRef = gatt
            val chRef = characteristic
            if (desc != null) {
                cccdWritesIssued = 1
                mainHandler.post {
                    if (outboundGatt !== gattRef || outboundChar !== chRef) return@post
                    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S && appContext?.let { hasBlePermissions(it) } != true) return@post
                    writeOutboundCccdNow()
                }
            } else {
                outboundCccdReady = true
                kickOutboundWrite()
            }
        }

        override fun onDescriptorWrite(gatt: BluetoothGatt, descriptor: BluetoothGattDescriptor, status: Int) {
            if (descriptor.uuid != CCCD_UUID) return
            if (outboundGatt !== gatt) {
                Log.d(TAG, "onDescriptorWrite ignore non-current gatt status=$status")
                return
            }
            if (status == BluetoothGatt.GATT_SUCCESS) {
                outboundCccdReady = true
                cccdWritesIssued = 0
                mainHandler.removeCallbacks(cccdRetryRunnable)
                kickOutboundWrite()
                return
            }
            Log.w(TAG, "cccd write failed status=$status issued=$cccdWritesIssued/$CCCD_MAX_ATTEMPTS (not enabling outbound writes)")
            outboundCccdReady = false
            if (cccdWritesIssued < CCCD_MAX_ATTEMPTS && outboundGatt != null && outboundChar != null) {
                cccdWritesIssued++
                mainHandler.removeCallbacks(cccdRetryRunnable)
                mainHandler.postDelayed(cccdRetryRunnable, CCCD_RETRY_DELAY_MS)
            } else {
                Log.e(TAG, "cccd exhausted ($cccdWritesIssued writes); outbound queue stays until new services/CCCD success")
            }
        }

        override fun onCharacteristicChanged(gatt: BluetoothGatt, characteristic: BluetoothGattCharacteristic, value: ByteArray) {
            val peerId = peerIdFromAddress(gatt.device.address ?: "")
            onBleFrame(peerId, value)
        }

        override fun onCharacteristicWrite(gatt: BluetoothGatt, characteristic: BluetoothGattCharacteristic, status: Int) {
            if (status == BluetoothGatt.GATT_SUCCESS) {
                consecutiveOutboundWriteFailures = 0
                Log.d(TAG, "gatt onCharacteristicWrite status=$status q=${outboundWriteQueue.size}")
                if (outboundWriteQueue.isNotEmpty()) {
                    mainHandler.removeCallbacks(outboundWriteDrainRunnable)
                    outboundWriteDrainPosted = true
                    outboundWriteInFlight = false
                    mainHandler.postDelayed(outboundWriteDrainRunnable, WRITE_PACE_MS)
                } else {
                    outboundWriteInFlight = false
                    cancelOutboundWritePacing()
                }
            } else {
                cancelOutboundWritePacing()
                outboundWriteInFlight = false
                Log.d(TAG, "gatt onCharacteristicWrite status=$status q=${outboundWriteQueue.size}")
                kickOutboundWrite()
            }
        }

        override fun onMtuChanged(gatt: BluetoothGatt, mtu: Int, status: Int) {
            Log.i(TAG, "mtu changed mtu=$mtu status=$status")
            if (status == BluetoothGatt.GATT_SUCCESS) {
                lastAttMtu = mtu
                if (outboundCccdReady) kickOutboundWrite()
            }
            if (outboundGatt === gatt && pendingOutboundServiceDiscover) {
                pendingOutboundServiceDiscover = false
                servicesDiscoverRetries = 0
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S && appContext?.let { hasBlePermissions(it) } != true) return
                gatt.discoverServices()
            }
        }

        @Suppress("DEPRECATION")
        override fun onCharacteristicChanged(gatt: BluetoothGatt, characteristic: BluetoothGattCharacteristic) {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                return
            }
            val peerId = peerIdFromAddress(gatt.device.address ?: "")
            onBleFrame(peerId, characteristic.value ?: ByteArray(0))
        }
    }

    private val gattServerCallback = object : BluetoothGattServerCallback() {
        override fun onConnectionStateChange(device: BluetoothDevice, status: Int, newState: Int) {
            val handle = CoreBridge.currentNodeHandle
            val peerId = peerIdFromAddress(device.address ?: "")
            if (newState == BluetoothProfile.STATE_CONNECTED) {
                inboundDevice = device
                peers.add(peerId)
                synchronized(inboundLock) { peerAddresses[peerId] = device.address ?: "unknown" }
                if (handle != 0L) {
                    CoreBridge.nativeOnPeerDiscovered(handle, peerId)
                    CoreBridge.nativeOnPeerConnected(handle, peerId)
                }
                peerEventsListener?.onPeersChanged()
            } else if (newState == BluetoothProfile.STATE_DISCONNECTED) {
                if (inboundDevice?.address == device.address) inboundDevice = null
                peers.remove(peerId)
                if (handle != 0L) CoreBridge.nativeOnPeerDisconnected(handle, peerId)
                peerEventsListener?.onPeersChanged()
            }
        }

        override fun onCharacteristicWriteRequest(
            device: BluetoothDevice,
            requestId: Int,
            characteristic: BluetoothGattCharacteristic,
            preparedWrite: Boolean,
            responseNeeded: Boolean,
            offset: Int,
            value: ByteArray,
        ) {
            val peerId = peerIdFromAddress(device.address ?: "")
            onBleFrame(peerId, value)
            if (responseNeeded) {
                gattServer?.sendResponse(device, requestId, BluetoothGatt.GATT_SUCCESS, offset, null)
            }
        }

        override fun onDescriptorReadRequest(
            device: BluetoothDevice,
            requestId: Int,
            offset: Int,
            descriptor: BluetoothGattDescriptor,
        ) {
            if (descriptor.uuid != CCCD_UUID) {
                gattServer?.sendResponse(device, requestId, BluetoothGatt.GATT_FAILURE, offset, null)
                return
            }
            val d = gattServer?.getService(SERVICE_UUID)?.getCharacteristic(CHAR_UUID)?.getDescriptor(CCCD_UUID)
            val v = d?.value ?: byteArrayOf(0x00, 0x00)
            gattServer?.sendResponse(device, requestId, BluetoothGatt.GATT_SUCCESS, offset, v)
        }

        override fun onDescriptorWriteRequest(
            device: BluetoothDevice,
            requestId: Int,
            descriptor: BluetoothGattDescriptor,
            preparedWrite: Boolean,
            responseNeeded: Boolean,
            offset: Int,
            value: ByteArray,
        ) {
            if (descriptor.uuid != CCCD_UUID) {
                if (responseNeeded) {
                    gattServer?.sendResponse(device, requestId, BluetoothGatt.GATT_FAILURE, offset, null)
                }
                return
            }
            val d = gattServer?.getService(SERVICE_UUID)?.getCharacteristic(CHAR_UUID)?.getDescriptor(CCCD_UUID)
            if (d == null) {
                if (responseNeeded) {
                    gattServer?.sendResponse(device, requestId, BluetoothGatt.GATT_FAILURE, offset, null)
                }
                return
            }
            if (!preparedWrite && offset == 0 && value.isNotEmpty()) {
                d.value = value.copyOf()
            }
            if (responseNeeded) {
                gattServer?.sendResponse(device, requestId, BluetoothGatt.GATT_SUCCESS, offset, null)
            }
        }

        override fun onNotificationSent(device: BluetoothDevice, status: Int) {
            if (status != BluetoothGatt.GATT_SUCCESS) {
                Log.w(TAG, "notification send failed status=$status")
            }
        }
    }

    private fun hasBlePermissions(context: Context): Boolean {
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            ContextCompat.checkSelfPermission(context, Manifest.permission.BLUETOOTH_SCAN) == PackageManager.PERMISSION_GRANTED &&
                ContextCompat.checkSelfPermission(context, Manifest.permission.BLUETOOTH_CONNECT) == PackageManager.PERMISSION_GRANTED &&
                ContextCompat.checkSelfPermission(context, Manifest.permission.BLUETOOTH_ADVERTISE) == PackageManager.PERMISSION_GRANTED
        } else {
            ContextCompat.checkSelfPermission(context, Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED &&
                ContextCompat.checkSelfPermission(context, Manifest.permission.BLUETOOTH) == PackageManager.PERMISSION_GRANTED &&
                ContextCompat.checkSelfPermission(context, Manifest.permission.BLUETOOTH_ADMIN) == PackageManager.PERMISSION_GRANTED
        }
    }

    private fun logLocationState(context: Context) {
        val lm = context.getSystemService(LocationManager::class.java)
        val enabled = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            lm?.isLocationEnabled == true
        } else {
            val gps = runCatching { lm?.isProviderEnabled(LocationManager.GPS_PROVIDER) == true }.getOrDefault(false)
            val net = runCatching { lm?.isProviderEnabled(LocationManager.NETWORK_PROVIDER) == true }.getOrDefault(false)
            gps || net
        }
        Log.i(TAG, "location enabled=$enabled")
    }

    private fun peerIdFromAddress(address: String): Long {
        val id = address.hashCode().toLong()
        return if (id >= 0) id else -id
    }

    private fun chunk(data: ByteArray, size: Int): List<ByteArray> {
        if (data.size <= size) return listOf(data)
        val out = ArrayList<ByteArray>((data.size + size - 1) / size)
        var i = 0
        while (i < data.size) {
            val end = minOf(i + size, data.size)
            out.add(data.copyOfRange(i, end))
            i = end
        }
        return out
    }
}
