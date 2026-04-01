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
import java.util.concurrent.CopyOnWriteArraySet
import java.util.concurrent.atomic.AtomicInteger
import android.os.Handler
import android.os.Looper
import android.os.ParcelUuid

/**
 * Real BLE transport for envelope byte exchange (GATT notify + write).
 */
object BleTransportManager {
    private const val TAG = "BleTransportManager"
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
    @Volatile
    private var legacyScanFallbackActive = false

    interface PeerEventsListener {
        fun onPeersChanged()
    }
    @Volatile
    private var peerEventsListener: PeerEventsListener? = null
    @Volatile
    private var started = false
    @Volatile
    private var loggedFirstRawScan = false
    @Volatile
    private var sawAnyScanCallback = false
    @Volatile
    private var lastScanKickMs = 0L
    @Volatile
    private var scanActive = false
    @Volatile
    private var scanBlockedUntilMs = 0L

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
            startScanning()
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
        startScanning()
        started = true
    }

    fun setPeerEventsListener(listener: PeerEventsListener?) {
        peerEventsListener = listener
    }

    fun connectedPeerIds(): List<Long> = peers.toList().sorted()

    fun peerAddress(peerId: Long): String? = synchronized(inboundLock) { peerAddresses[peerId] }

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
    }

    fun connectPeer(peerId: Long) {
        // Legacy no-op: keep API stable but avoid fake/manual peer injection.
        appContext?.let { if (hasBlePermissions(it)) startScanning() }
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

    private fun makeFrames(data: ByteArray): List<ByteArray> {
        if (data.isEmpty()) return emptyList()
        val parts = chunk(data, MAX_FRAME_PAYLOAD)
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
            for (frame in frames) {
                ch.value = frame
                val ok = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                    gatt.writeCharacteristic(ch, frame, BluetoothGattCharacteristic.WRITE_TYPE_DEFAULT) == BluetoothGatt.GATT_SUCCESS
                } else {
                    @Suppress("DEPRECATION")
                    gatt.writeCharacteristic(ch)
                }
                if (!ok) {
                    Log.w(TAG, "gatt write failed peer=$peerId bytes=${frame.size}")
                    return false
                }
            }
            return true
        }

        // If we have a central connected to our server, push notify.
        val server = gattServer
        val device = inboundDevice
        if (server != null && device != null) {
            val service = server.getService(SERVICE_UUID) ?: return false
            val characteristic = service.getCharacteristic(CHAR_UUID) ?: return false
            for (frame in makeFrames(data)) {
                characteristic.value = frame
                @Suppress("DEPRECATION")
                server.notifyCharacteristicChanged(device, characteristic, false)
            }
            return true
        }
        return false
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

    private fun startScanning() {
        val ctx = appContext ?: return
        if (!hasBlePermissions(ctx)) return
        val now = System.currentTimeMillis()
        if (scanActive) return
        if (now < scanBlockedUntilMs) {
            Log.w(TAG, "scan temporarily blocked; retry in ${scanBlockedUntilMs - now}ms")
            return
        }
        if (now - lastScanKickMs < 2500L) return
        lastScanKickMs = now
        logLocationState(ctx)
        loggedFirstRawScan = false
        sawAnyScanCallback = false
        val filters = listOf(
            ScanFilter.Builder()
                .setServiceUuid(ParcelUuid(SERVICE_UUID))
                .build(),
            ScanFilter.Builder()
                .setManufacturerData(MANUFACTURER_ID, MANUFACTURER_TAG)
                .build(),
        )
        val settings = ScanSettings.Builder()
            .setScanMode(ScanSettings.SCAN_MODE_LOW_LATENCY)
            .setReportDelay(0L)
            .build()
        try {
            // Filtered scan avoids Android throttling of unfiltered background/screen-off scans.
            scanner?.startScan(filters, settings, scanCallback)
            scanActive = true
            Log.i(TAG, "scan started (filtered)")
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

    private val scanCallback = object : ScanCallback() {
        override fun onScanResult(callbackType: Int, result: ScanResult) {
            sawAnyScanCallback = true
            val device = result.device ?: return
            val address = device.address ?: return
            if (!loggedFirstRawScan) {
                loggedFirstRawScan = true
                Log.i(TAG, "raw scan callback addr=$address rssi=${result.rssi}")
            }
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

    private val gattCallback = object : BluetoothGattCallback() {
        override fun onConnectionStateChange(gatt: BluetoothGatt, status: Int, newState: Int) {
            val handle = CoreBridge.currentNodeHandle
            val peerId = peerIdFromAddress(gatt.device.address ?: "")
            pendingConnectAddress = null
            if (newState == BluetoothProfile.STATE_CONNECTED) {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S && appContext?.let { hasBlePermissions(it) } != true) return
                outboundDevice = gatt.device
                gatt.discoverServices()
                peers.add(peerId)
                synchronized(inboundLock) { peerAddresses[peerId] = gatt.device.address ?: "unknown" }
                if (handle != 0L) CoreBridge.nativeOnPeerConnected(handle, peerId)
                peerEventsListener?.onPeersChanged()
                Log.i(TAG, "gatt connected addr=${gatt.device.address} peerId=$peerId")
            } else if (newState == BluetoothProfile.STATE_DISCONNECTED) {
                peers.remove(peerId)
                if (handle != 0L) CoreBridge.nativeOnPeerDisconnected(handle, peerId)
                runCatching { gatt.close() }
                if (outboundGatt === gatt) outboundGatt = null
                if (outboundDevice?.address == gatt.device.address) outboundDevice = null
                outboundChar = null
                appContext?.let { if (hasBlePermissions(it)) startScanning() }
                peerEventsListener?.onPeersChanged()
                Log.w(TAG, "gatt disconnected addr=${gatt.device.address} status=$status")
            }
        }

        override fun onServicesDiscovered(gatt: BluetoothGatt, status: Int) {
            val service = gatt.getService(SERVICE_UUID) ?: return
            val characteristic = service.getCharacteristic(CHAR_UUID) ?: return
            outboundChar = characteristic
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S && appContext?.let { hasBlePermissions(it) } != true) return
            @Suppress("DEPRECATION")
            gatt.setCharacteristicNotification(characteristic, true)
            val desc = characteristic.getDescriptor(CCCD_UUID)
            if (desc != null) {
                desc.value = BluetoothGattDescriptor.ENABLE_NOTIFICATION_VALUE
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                    gatt.writeDescriptor(desc, BluetoothGattDescriptor.ENABLE_NOTIFICATION_VALUE)
                } else {
                    @Suppress("DEPRECATION")
                    gatt.writeDescriptor(desc)
                }
            }
        }

        override fun onCharacteristicChanged(gatt: BluetoothGatt, characteristic: BluetoothGattCharacteristic, value: ByteArray) {
            val peerId = peerIdFromAddress(gatt.device.address ?: "")
            onBleFrame(peerId, value)
        }

        @Suppress("DEPRECATION")
        override fun onCharacteristicChanged(gatt: BluetoothGatt, characteristic: BluetoothGattCharacteristic) {
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
