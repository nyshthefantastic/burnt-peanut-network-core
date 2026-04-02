//go:build cgo

package main

/*
#cgo CFLAGS: -I${SRCDIR}
#include "core.h"
#include "shims.h"
#include <stdlib.h>

static inline void cgo_free(void* p) { if (p) free(p); }
*/
import "C"
import "unsafe"

// NativeCallbacks wraps the C MLCallbacks struct into Go-friendly methods.
// Each method calls a C shim, which calls the native function pointer.
// Flow: Go method → C shim → native function pointer
type NativeCallbacks struct {
	raw C.MLCallbacks
}

func wrapCallbacks(callbacks C.MLCallbacks) *NativeCallbacks {
	return &NativeCallbacks{raw: callbacks}
}

// ─── Transport ───

func (nc *NativeCallbacks) Send(peerID uintptr, data []byte) int32 {
	if nc.raw.send == nil {
		return ML_ERR_INTERNAL
	}
	// Convert Go []byte to C pointer + length
	// C.CBytes copies Go memory into C memory
	cData := C.CBytes(data)
	defer C.free(cData)

	result := C.ml_shim_send(
		nc.raw.send,
		C.uintptr_t(peerID),
		(*C.uint8_t)(cData),
		C.int32_t(len(data)),
	)
	return int32(result)
}

func (nc *NativeCallbacks) StartAdvertising(payload []byte) int32 {
	cData := C.CBytes(payload)
	defer C.free(cData)

	result := C.ml_shim_start_advertising(
		nc.raw.start_advertising,
		(*C.uint8_t)(cData),
		C.int32_t(len(payload)),
	)
	return int32(result)
}

func (nc *NativeCallbacks) StopAdvertising() int32 {
	return int32(C.ml_shim_stop_advertising(nc.raw.stop_advertising))
}

func (nc *NativeCallbacks) StartScanning() int32 {
	return int32(C.ml_shim_start_scanning(nc.raw.start_scanning))
}

func (nc *NativeCallbacks) StopScanning() int32 {
	return int32(C.ml_shim_stop_scanning(nc.raw.stop_scanning))
}

func (nc *NativeCallbacks) Disconnect(peerID uintptr) int32 {
	return int32(C.ml_shim_disconnect(nc.raw.disconnect, C.uintptr_t(peerID)))
}

// ─── Hardware Crypto ───

func (nc *NativeCallbacks) SignWithSecureKey(data []byte) ([]byte, int32) {
	if nc.raw.sign_with_secure_key == nil {
		return nil, ML_ERR_INTERNAL
	}
	cData := C.CBytes(data)
	defer C.free(cData)

	// Ed25519 signature is 64 bytes
	sigOut := make([]byte, 64)

	result := C.ml_shim_sign_secure(
		nc.raw.sign_with_secure_key,
		(*C.uint8_t)(cData),
		C.int32_t(len(data)),
		(*C.uint8_t)(unsafe.Pointer(&sigOut[0])),
		C.int32_t(len(sigOut)),
	)
	return sigOut, int32(result)
}

func (nc *NativeCallbacks) GetPublicKey() ([]byte, int32) {
	// Ed25519 public key is 32 bytes
	pubOut := make([]byte, 32)

	result := C.ml_shim_get_pubkey(
		nc.raw.get_public_key,
		(*C.uint8_t)(unsafe.Pointer(&pubOut[0])),
		C.int32_t(len(pubOut)),
	)
	return pubOut, int32(result)
}

func (nc *NativeCallbacks) GetAttestation() ([]byte, int32) {
	// Attestation blob size varies — allocate generous buffer
	attOut := make([]byte, 4096)

	result := C.ml_shim_get_attestation(
		nc.raw.get_attestation,
		(*C.uint8_t)(unsafe.Pointer(&attOut[0])),
		C.int32_t(len(attOut)),
	)
	return attOut, int32(result)
}

func (nc *NativeCallbacks) HasSecureElement() bool {
	if nc.raw.has_secure_element == nil {
		return false
	}
	return bool(C.ml_shim_has_secure_element(nc.raw.has_secure_element))
}

// ─── Chunk Storage ───

func (nc *NativeCallbacks) WriteChunk(fileHash []byte, chunkIndex uint32, data []byte) int32 {
	if nc.raw.write_chunk == nil {
		return ML_ERR_INTERNAL
	}
	cHash := C.CBytes(fileHash)
	defer C.free(cHash)

	cData := C.CBytes(data)
	defer C.free(cData)

	result := C.ml_shim_write_chunk(
		nc.raw.write_chunk,
		(*C.uint8_t)(cHash),
		C.int32_t(len(fileHash)),
		C.uint32_t(chunkIndex),
		(*C.uint8_t)(cData),
		C.int32_t(len(data)),
	)
	return int32(result)
}

func (nc *NativeCallbacks) ReadChunk(fileHash []byte, chunkIndex uint32, bufferSize int) ([]byte, int32) {
	if nc.raw.read_chunk == nil {
		return nil, ML_ERR_INTERNAL
	}
	cHash := C.CBytes(fileHash)
	defer C.free(cHash)

	dataOut := make([]byte, bufferSize)

	result := C.ml_shim_read_chunk(
		nc.raw.read_chunk,
		(*C.uint8_t)(cHash),
		C.int32_t(len(fileHash)),
		C.uint32_t(chunkIndex),
		(*C.uint8_t)(unsafe.Pointer(&dataOut[0])),
		C.int32_t(bufferSize),
	)
	rc := int32(result)
	if rc < 0 {
		return nil, -rc
	}
	if rc == 0 {
		return nil, ML_ERR_NOT_FOUND
	}
	return dataOut[:rc], ML_OK
}

func (nc *NativeCallbacks) HasChunk(fileHash []byte, chunkIndex uint32) bool {
	if nc.raw.has_chunk == nil {
		return false
	}
	cHash := C.CBytes(fileHash)
	defer C.free(cHash)

	return bool(C.ml_shim_has_chunk(
		nc.raw.has_chunk,
		(*C.uint8_t)(cHash),
		C.int32_t(len(fileHash)),
		C.uint32_t(chunkIndex),
	))
}

func (nc *NativeCallbacks) DeleteFile(fileHash []byte) int32 {
	cHash := C.CBytes(fileHash)
	defer C.free(cHash)

	result := C.ml_shim_delete_file(
		nc.raw.delete_file,
		(*C.uint8_t)(cHash),
		C.int32_t(len(fileHash)),
	)
	return int32(result)
}

func (nc *NativeCallbacks) AvailableSpace() int64 {
	return int64(C.ml_shim_available_space(nc.raw.available_space))
}

// ─── Notifications ───

func (nc *NativeCallbacks) NotifyTransferProgress(peerID uintptr, percent int32) {
	if nc.raw.notify_transfer_progress == nil {
		return
	}
	C.ml_shim_notify_transfer_progress(
		nc.raw.notify_transfer_progress,
		C.uintptr_t(peerID),
		C.int32_t(percent),
	)
}

func (nc *NativeCallbacks) NotifyTransferComplete(peerID uintptr, fileHash []byte) {
	if nc.raw.notify_transfer_complete == nil {
		return
	}
	cHash := C.CBytes(fileHash)
	defer C.free(cHash)

	C.ml_shim_notify_transfer_complete(
		nc.raw.notify_transfer_complete,
		C.uintptr_t(peerID),
		(*C.uint8_t)(cHash),
		C.int32_t(len(fileHash)),
	)
}

func (nc *NativeCallbacks) NotifyTransferFailed(peerID uintptr, errorCode int32) {
	if nc.raw.notify_transfer_failed == nil {
		return
	}
	C.ml_shim_notify_transfer_failed(
		nc.raw.notify_transfer_failed,
		C.uintptr_t(peerID),
		C.int32_t(errorCode),
	)
}

func (nc *NativeCallbacks) NotifyPeerVerified(peerID uintptr, valid bool) {
	if nc.raw.notify_peer_verified == nil {
		return
	}
	C.ml_shim_notify_peer_verified(
		nc.raw.notify_peer_verified,
		C.uintptr_t(peerID),
		C.bool(valid),
	)
}

func (nc *NativeCallbacks) NotifyForkDetected(devicePubkey []byte) {
	if nc.raw.notify_fork_detected == nil {
		return
	}
	cPubkey := C.CBytes(devicePubkey)
	defer C.free(cPubkey)

	C.ml_shim_notify_fork_detected(
		nc.raw.notify_fork_detected,
		(*C.uint8_t)(cPubkey),
		C.int32_t(len(devicePubkey)),
	)
}

func (nc *NativeCallbacks) NotifyBalanceChanged(newBalance int64) {
	if nc.raw.notify_balance_changed == nil {
		return
	}
	C.ml_shim_notify_balance_changed(
		nc.raw.notify_balance_changed,
		C.int64_t(newBalance),
	)
}

func (nc *NativeCallbacks) NotifyGossipReceived(peerID uintptr) {
	if nc.raw.notify_gossip_received == nil {
		return
	}
	C.ml_shim_notify_gossip_received(
		nc.raw.notify_gossip_received,
		C.uintptr_t(peerID),
	)
}