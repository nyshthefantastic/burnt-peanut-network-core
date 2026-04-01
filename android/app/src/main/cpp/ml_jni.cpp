#include <jni.h>
#include <cstring>
#include <cstdint>

#include "core.h"

namespace {

static int32_t stub_send(uintptr_t peer_id, const uint8_t* data, int32_t len) {
    (void)peer_id;
    (void)data;
    (void)len;
    return ML_OK;
}

static int32_t stub_start_advertising(const uint8_t* payload, int32_t len) {
    (void)payload;
    (void)len;
    return ML_OK;
}

static int32_t stub_ok_void(void) { return ML_OK; }

static int32_t stub_disconnect(uintptr_t peer_id) {
    (void)peer_id;
    return ML_OK;
}

static int32_t stub_sign_secure(const uint8_t* data, int32_t data_len,
                                uint8_t* sig_out, int32_t sig_out_len) {
    (void)data;
    (void)data_len;
    (void)sig_out;
    (void)sig_out_len;
    return ML_ERR_CRYPTO;
}

static int32_t stub_get_pubkey(uint8_t* pubkey_out, int32_t pubkey_out_len) {
    (void)pubkey_out;
    (void)pubkey_out_len;
    return ML_ERR_CRYPTO;
}

static int32_t stub_get_attestation(uint8_t* att_out, int32_t att_out_len) {
    (void)att_out;
    (void)att_out_len;
    return ML_ERR_CRYPTO;
}

static bool stub_has_secure_element(void) { return false; }

static int32_t stub_write_chunk(const uint8_t* file_hash, int32_t fh_len,
                                uint32_t chunk_index,
                                const uint8_t* data, int32_t data_len) {
    (void)file_hash;
    (void)fh_len;
    (void)chunk_index;
    (void)data;
    (void)data_len;
    return ML_ERR_INTERNAL;
}

static int32_t stub_read_chunk(const uint8_t* file_hash, int32_t fh_len,
                               uint32_t chunk_index,
                               uint8_t* data_out, int32_t data_out_len) {
    (void)file_hash;
    (void)fh_len;
    (void)chunk_index;
    (void)data_out;
    (void)data_out_len;
    return ML_ERR_INTERNAL;
}

static bool stub_has_chunk(const uint8_t* file_hash, int32_t fh_len,
                           uint32_t chunk_index) {
    (void)file_hash;
    (void)fh_len;
    (void)chunk_index;
    return false;
}

static int32_t stub_delete_file(const uint8_t* file_hash, int32_t fh_len) {
    (void)file_hash;
    (void)fh_len;
    return ML_ERR_INTERNAL;
}

static int64_t stub_available_space(void) { return 1LL << 30; }

static void stub_notify_transfer_progress(uintptr_t peer_id, int32_t percent) {
    (void)peer_id;
    (void)percent;
}

static void stub_notify_transfer_complete(uintptr_t peer_id,
                                          const uint8_t* file_hash, int32_t fh_len) {
    (void)peer_id;
    (void)file_hash;
    (void)fh_len;
}

static void stub_notify_transfer_failed(uintptr_t peer_id, int32_t error_code) {
    (void)peer_id;
    (void)error_code;
}

static void stub_notify_peer_verified(uintptr_t peer_id, bool valid) {
    (void)peer_id;
    (void)valid;
}

static void stub_notify_fork_detected(const uint8_t* device_pubkey, int32_t pk_len) {
    (void)device_pubkey;
    (void)pk_len;
}

static void stub_notify_balance_changed(int64_t new_balance) {
    (void)new_balance;
}

static void stub_notify_gossip_received(uintptr_t peer_id) {
    (void)peer_id;
}

MLCallbacks make_stub_callbacks() {
    MLCallbacks cb{};
    cb.send = stub_send;
    cb.start_advertising = stub_start_advertising;
    cb.stop_advertising = stub_ok_void;
    cb.start_scanning = stub_ok_void;
    cb.stop_scanning = stub_ok_void;
    cb.disconnect = stub_disconnect;
    cb.sign_with_secure_key = stub_sign_secure;
    cb.get_public_key = stub_get_pubkey;
    cb.get_attestation = stub_get_attestation;
    cb.has_secure_element = stub_has_secure_element;
    cb.write_chunk = stub_write_chunk;
    cb.read_chunk = stub_read_chunk;
    cb.has_chunk = stub_has_chunk;
    cb.delete_file = stub_delete_file;
    cb.available_space = stub_available_space;
    cb.notify_transfer_progress = stub_notify_transfer_progress;
    cb.notify_transfer_complete = stub_notify_transfer_complete;
    cb.notify_transfer_failed = stub_notify_transfer_failed;
    cb.notify_peer_verified = stub_notify_peer_verified;
    cb.notify_fork_detected = stub_notify_fork_detected;
    cb.notify_balance_changed = stub_notify_balance_changed;
    cb.notify_gossip_received = stub_notify_gossip_received;
    return cb;
}

}  // namespace

extern "C" JNIEXPORT jlong JNICALL
Java_com_burntpeanut_core_CoreBridge_nativeCreateNode(JNIEnv* env, jobject /*thiz*/, jstring jpath) {
    if (!jpath) {
        return 0;
    }
    const char* path = env->GetStringUTFChars(jpath, nullptr);
    if (!path) {
        return 0;
    }
    MLCallbacks cb = make_stub_callbacks();
    MLNode node = ml_node_create(path, cb);
    env->ReleaseStringUTFChars(jpath, path);
    return static_cast<jlong>(node);
}

extern "C" JNIEXPORT void JNICALL
Java_com_burntpeanut_core_CoreBridge_nativeDestroyNode(JNIEnv* /*env*/, jobject /*thiz*/, jlong handle) {
    if (handle != 0) {
        ml_node_destroy(static_cast<MLNode>(handle));
    }
}
