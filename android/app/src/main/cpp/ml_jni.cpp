#include <jni.h>
#include <algorithm>
#include <cstdint>
#include <cstring>
#include <vector>
#include <android/log.h>

#include "core.h"

namespace {
constexpr const char* TAG = "ml_jni";

JavaVM* g_vm = nullptr;
jclass g_hooks_cls = nullptr;

jmethodID g_onSend = nullptr;
jmethodID g_onStartAdvertising = nullptr;
jmethodID g_onStopAdvertising = nullptr;
jmethodID g_onStartScanning = nullptr;
jmethodID g_onStopScanning = nullptr;
jmethodID g_onDisconnect = nullptr;
jmethodID g_onSignSecure = nullptr;
jmethodID g_onGetPublicKey = nullptr;
jmethodID g_onGetAttestation = nullptr;
jmethodID g_onHasSecureElement = nullptr;
jmethodID g_onWriteChunk = nullptr;
jmethodID g_onReadChunk = nullptr;
jmethodID g_onHasChunk = nullptr;
jmethodID g_onDeleteFile = nullptr;
jmethodID g_onAvailableSpace = nullptr;
jmethodID g_onNotifyTransferProgress = nullptr;
jmethodID g_onNotifyTransferComplete = nullptr;
jmethodID g_onNotifyTransferFailed = nullptr;
jmethodID g_onNotifyPeerVerified = nullptr;
jmethodID g_onNotifyForkDetected = nullptr;
jmethodID g_onNotifyBalanceChanged = nullptr;
jmethodID g_onNotifyGossipReceived = nullptr;

JNIEnv* env_or_null() {
    if (!g_vm) return nullptr;
    JNIEnv* env = nullptr;
    jint status = g_vm->GetEnv(reinterpret_cast<void**>(&env), JNI_VERSION_1_6);
    if (status == JNI_EDETACHED) {
        if (g_vm->AttachCurrentThread(&env, nullptr) != JNI_OK) return nullptr;
    }
    return env;
}

bool clear_jni_exception(JNIEnv* env, const char* where) {
    if (!env || !env->ExceptionCheck()) return false;
    env->ExceptionDescribe();
    env->ExceptionClear();
    __android_log_print(ANDROID_LOG_ERROR, TAG, "%s: cleared Java exception", where);
    return true;
}

jbyteArray to_jbyte_array(JNIEnv* env, const uint8_t* data, int32_t len) {
    if (!env || !data || len <= 0) return env ? env->NewByteArray(0) : nullptr;
    jbyteArray arr = env->NewByteArray(len);
    if (!arr) return nullptr;
    env->SetByteArrayRegion(arr, 0, len, reinterpret_cast<const jbyte*>(data));
    return arr;
}

int32_t cb_send(uintptr_t peer_id, const uint8_t* data, int32_t len) {
    JNIEnv* env = env_or_null();
    if (!env || !g_hooks_cls || !g_onSend) return ML_ERR_INTERNAL;
    jbyteArray payload = to_jbyte_array(env, data, len);
    if (!payload && len > 0) return ML_ERR_INTERNAL;
    jint rc = env->CallStaticIntMethod(g_hooks_cls, g_onSend, static_cast<jlong>(peer_id), payload);
    if (clear_jni_exception(env, "cb_send")) rc = ML_ERR_INTERNAL;
    if (payload) env->DeleteLocalRef(payload);
    return static_cast<int32_t>(rc);
}

int32_t cb_start_advertising(const uint8_t* payload, int32_t len) {
    JNIEnv* env = env_or_null();
    if (!env || !g_hooks_cls || !g_onStartAdvertising) return ML_ERR_INTERNAL;
    jbyteArray arr = to_jbyte_array(env, payload, len);
    if (!arr && len > 0) return ML_ERR_INTERNAL;
    jint rc = env->CallStaticIntMethod(g_hooks_cls, g_onStartAdvertising, arr);
    if (clear_jni_exception(env, "cb_start_advertising")) rc = ML_ERR_INTERNAL;
    if (arr) env->DeleteLocalRef(arr);
    return static_cast<int32_t>(rc);
}

int32_t cb_stop_advertising() {
    JNIEnv* env = env_or_null();
    if (!env || !g_hooks_cls || !g_onStopAdvertising) return ML_ERR_INTERNAL;
    jint rc = env->CallStaticIntMethod(g_hooks_cls, g_onStopAdvertising);
    if (clear_jni_exception(env, "cb_stop_advertising")) return ML_ERR_INTERNAL;
    return static_cast<int32_t>(rc);
}

int32_t cb_start_scanning() {
    JNIEnv* env = env_or_null();
    if (!env || !g_hooks_cls || !g_onStartScanning) return ML_ERR_INTERNAL;
    jint rc = env->CallStaticIntMethod(g_hooks_cls, g_onStartScanning);
    if (clear_jni_exception(env, "cb_start_scanning")) return ML_ERR_INTERNAL;
    return static_cast<int32_t>(rc);
}

int32_t cb_stop_scanning() {
    JNIEnv* env = env_or_null();
    if (!env || !g_hooks_cls || !g_onStopScanning) return ML_ERR_INTERNAL;
    jint rc = env->CallStaticIntMethod(g_hooks_cls, g_onStopScanning);
    if (clear_jni_exception(env, "cb_stop_scanning")) return ML_ERR_INTERNAL;
    return static_cast<int32_t>(rc);
}

int32_t cb_disconnect(uintptr_t peer_id) {
    JNIEnv* env = env_or_null();
    if (!env || !g_hooks_cls || !g_onDisconnect) return ML_ERR_INTERNAL;
    jint rc = env->CallStaticIntMethod(g_hooks_cls, g_onDisconnect, static_cast<jlong>(peer_id));
    if (clear_jni_exception(env, "cb_disconnect")) return ML_ERR_INTERNAL;
    return static_cast<int32_t>(rc);
}

int32_t cb_sign_secure(const uint8_t* data, int32_t data_len, uint8_t* sig_out, int32_t sig_out_len) {
    JNIEnv* env = env_or_null();
    if (!env || !g_hooks_cls || !g_onSignSecure || !sig_out || sig_out_len <= 0) return ML_ERR_INTERNAL;
    jbyteArray arr = to_jbyte_array(env, data, data_len);
    if (!arr && data_len > 0) return ML_ERR_INTERNAL;
    auto sig = static_cast<jbyteArray>(env->CallStaticObjectMethod(g_hooks_cls, g_onSignSecure, arr));
    if (arr) env->DeleteLocalRef(arr);
    if (clear_jni_exception(env, "cb_sign_secure")) {
        return ML_ERR_CRYPTO;
    }
    if (!sig) return ML_ERR_CRYPTO;
    const jsize n = env->GetArrayLength(sig);
    const jsize copy_n = std::min<jsize>(n, sig_out_len);
    env->GetByteArrayRegion(sig, 0, copy_n, reinterpret_cast<jbyte*>(sig_out));
    env->DeleteLocalRef(sig);
    return ML_OK;
}

int32_t copy_java_bytes(jmethodID method, uint8_t* out, int32_t out_len) {
    JNIEnv* env = env_or_null();
    if (!env || !g_hooks_cls || !method || !out || out_len <= 0) return ML_ERR_INTERNAL;
    auto arr = static_cast<jbyteArray>(env->CallStaticObjectMethod(g_hooks_cls, method));
    if (clear_jni_exception(env, "copy_java_bytes")) return ML_ERR_INTERNAL;
    if (!arr) return ML_ERR_INTERNAL;
    jsize n = env->GetArrayLength(arr);
    jsize copy_n = std::min<jsize>(n, out_len);
    env->GetByteArrayRegion(arr, 0, copy_n, reinterpret_cast<jbyte*>(out));
    env->DeleteLocalRef(arr);
    return ML_OK;
}

int32_t cb_get_pubkey(uint8_t* pubkey_out, int32_t pubkey_out_len) {
    return copy_java_bytes(g_onGetPublicKey, pubkey_out, pubkey_out_len);
}

int32_t cb_get_attestation(uint8_t* att_out, int32_t att_out_len) {
    return copy_java_bytes(g_onGetAttestation, att_out, att_out_len);
}

bool cb_has_secure_element() {
    JNIEnv* env = env_or_null();
    if (!env || !g_hooks_cls || !g_onHasSecureElement) return false;
    jboolean ok = env->CallStaticBooleanMethod(g_hooks_cls, g_onHasSecureElement);
    if (clear_jni_exception(env, "cb_has_secure_element")) return false;
    return ok == JNI_TRUE;
}

int32_t cb_write_chunk(const uint8_t* file_hash, int32_t fh_len, uint32_t chunk_index, const uint8_t* data, int32_t data_len) {
    JNIEnv* env = env_or_null();
    if (!env || !g_hooks_cls || !g_onWriteChunk) return ML_ERR_INTERNAL;
    jbyteArray hash = to_jbyte_array(env, file_hash, fh_len);
    jbyteArray chunk = to_jbyte_array(env, data, data_len);
    if ((!hash && fh_len > 0) || (!chunk && data_len > 0)) {
        if (hash) env->DeleteLocalRef(hash);
        if (chunk) env->DeleteLocalRef(chunk);
        return ML_ERR_INTERNAL;
    }
    jint rc = env->CallStaticIntMethod(g_hooks_cls, g_onWriteChunk, hash, static_cast<jint>(chunk_index), chunk);
    if (clear_jni_exception(env, "cb_write_chunk")) rc = ML_ERR_INTERNAL;
    if (hash) env->DeleteLocalRef(hash);
    if (chunk) env->DeleteLocalRef(chunk);
    return static_cast<int32_t>(rc);
}

int32_t cb_read_chunk(const uint8_t* file_hash, int32_t fh_len, uint32_t chunk_index, uint8_t* data_out, int32_t data_out_len) {
    JNIEnv* env = env_or_null();
    if (!env || !g_hooks_cls || !g_onReadChunk || !data_out) return ML_ERR_INTERNAL;
    jbyteArray hash = to_jbyte_array(env, file_hash, fh_len);
    if (!hash && fh_len > 0) return ML_ERR_INTERNAL;
    auto out = static_cast<jbyteArray>(env->CallStaticObjectMethod(g_hooks_cls, g_onReadChunk, hash, static_cast<jint>(chunk_index)));
    if (hash) env->DeleteLocalRef(hash);
    if (clear_jni_exception(env, "cb_read_chunk")) return ML_ERR_INTERNAL;
    if (!out) return ML_ERR_NOT_FOUND;
    jsize n = env->GetArrayLength(out);
    jsize copy_n = std::min<jsize>(n, data_out_len);
    env->GetByteArrayRegion(out, 0, copy_n, reinterpret_cast<jbyte*>(data_out));
    if (copy_n < data_out_len) {
        std::memset(data_out + copy_n, 0, static_cast<size_t>(data_out_len - copy_n));
    }
    env->DeleteLocalRef(out);
    return ML_OK;
}

bool cb_has_chunk(const uint8_t* file_hash, int32_t fh_len, uint32_t chunk_index) {
    JNIEnv* env = env_or_null();
    if (!env || !g_hooks_cls || !g_onHasChunk) return false;
    jbyteArray hash = to_jbyte_array(env, file_hash, fh_len);
    if (!hash && fh_len > 0) return false;
    jboolean ok = env->CallStaticBooleanMethod(g_hooks_cls, g_onHasChunk, hash, static_cast<jint>(chunk_index));
    if (clear_jni_exception(env, "cb_has_chunk")) ok = JNI_FALSE;
    if (hash) env->DeleteLocalRef(hash);
    return ok == JNI_TRUE;
}

int32_t cb_delete_file(const uint8_t* file_hash, int32_t fh_len) {
    JNIEnv* env = env_or_null();
    if (!env || !g_hooks_cls || !g_onDeleteFile) return ML_ERR_INTERNAL;
    jbyteArray hash = to_jbyte_array(env, file_hash, fh_len);
    if (!hash && fh_len > 0) return ML_ERR_INTERNAL;
    jint rc = env->CallStaticIntMethod(g_hooks_cls, g_onDeleteFile, hash);
    if (clear_jni_exception(env, "cb_delete_file")) rc = ML_ERR_INTERNAL;
    if (hash) env->DeleteLocalRef(hash);
    return static_cast<int32_t>(rc);
}

int64_t cb_available_space() {
    JNIEnv* env = env_or_null();
    if (!env || !g_hooks_cls || !g_onAvailableSpace) return 0;
    jlong n = env->CallStaticLongMethod(g_hooks_cls, g_onAvailableSpace);
    if (clear_jni_exception(env, "cb_available_space")) return 0;
    return static_cast<int64_t>(n);
}

void notify_void_peer_int(jmethodID method, uintptr_t peer_id, int32_t value) {
    JNIEnv* env = env_or_null();
    if (!env || !g_hooks_cls || !method) return;
    env->CallStaticVoidMethod(g_hooks_cls, method, static_cast<jlong>(peer_id), static_cast<jint>(value));
    clear_jni_exception(env, "notify_void_peer_int");
}

void cb_notify_transfer_progress(uintptr_t peer_id, int32_t percent) { notify_void_peer_int(g_onNotifyTransferProgress, peer_id, percent); }
void cb_notify_transfer_failed(uintptr_t peer_id, int32_t error_code) { notify_void_peer_int(g_onNotifyTransferFailed, peer_id, error_code); }

void cb_notify_transfer_complete(uintptr_t peer_id, const uint8_t* file_hash, int32_t fh_len) {
    JNIEnv* env = env_or_null();
    if (!env || !g_hooks_cls || !g_onNotifyTransferComplete) return;
    jbyteArray hash = to_jbyte_array(env, file_hash, fh_len);
    if (!hash && fh_len > 0) return;
    env->CallStaticVoidMethod(g_hooks_cls, g_onNotifyTransferComplete, static_cast<jlong>(peer_id), hash);
    clear_jni_exception(env, "cb_notify_transfer_complete");
    if (hash) env->DeleteLocalRef(hash);
}

void cb_notify_peer_verified(uintptr_t peer_id, bool valid) {
    JNIEnv* env = env_or_null();
    if (!env || !g_hooks_cls || !g_onNotifyPeerVerified) return;
    env->CallStaticVoidMethod(g_hooks_cls, g_onNotifyPeerVerified, static_cast<jlong>(peer_id), valid ? JNI_TRUE : JNI_FALSE);
    clear_jni_exception(env, "cb_notify_peer_verified");
}

void cb_notify_fork_detected(const uint8_t* device_pubkey, int32_t pk_len) {
    JNIEnv* env = env_or_null();
    if (!env || !g_hooks_cls || !g_onNotifyForkDetected) return;
    jbyteArray key = to_jbyte_array(env, device_pubkey, pk_len);
    if (!key && pk_len > 0) return;
    env->CallStaticVoidMethod(g_hooks_cls, g_onNotifyForkDetected, key);
    clear_jni_exception(env, "cb_notify_fork_detected");
    if (key) env->DeleteLocalRef(key);
}

void cb_notify_balance_changed(int64_t new_balance) {
    JNIEnv* env = env_or_null();
    if (!env || !g_hooks_cls || !g_onNotifyBalanceChanged) return;
    env->CallStaticVoidMethod(g_hooks_cls, g_onNotifyBalanceChanged, static_cast<jlong>(new_balance));
    clear_jni_exception(env, "cb_notify_balance_changed");
}

void cb_notify_gossip_received(uintptr_t peer_id) {
    JNIEnv* env = env_or_null();
    if (!env || !g_hooks_cls || !g_onNotifyGossipReceived) return;
    env->CallStaticVoidMethod(g_hooks_cls, g_onNotifyGossipReceived, static_cast<jlong>(peer_id));
    clear_jni_exception(env, "cb_notify_gossip_received");
}

MLCallbacks make_callbacks() {
    MLCallbacks cb{};
    cb.send = cb_send;
    cb.start_advertising = cb_start_advertising;
    cb.stop_advertising = cb_stop_advertising;
    cb.start_scanning = cb_start_scanning;
    cb.stop_scanning = cb_stop_scanning;
    cb.disconnect = cb_disconnect;
    cb.sign_with_secure_key = cb_sign_secure;
    cb.get_public_key = cb_get_pubkey;
    cb.get_attestation = cb_get_attestation;
    cb.has_secure_element = cb_has_secure_element;
    cb.write_chunk = cb_write_chunk;
    cb.read_chunk = cb_read_chunk;
    cb.has_chunk = cb_has_chunk;
    cb.delete_file = cb_delete_file;
    cb.available_space = cb_available_space;
    cb.notify_transfer_progress = cb_notify_transfer_progress;
    cb.notify_transfer_complete = cb_notify_transfer_complete;
    cb.notify_transfer_failed = cb_notify_transfer_failed;
    cb.notify_peer_verified = cb_notify_peer_verified;
    cb.notify_fork_detected = cb_notify_fork_detected;
    cb.notify_balance_changed = cb_notify_balance_changed;
    cb.notify_gossip_received = cb_notify_gossip_received;
    return cb;
}

bool init_hook_ids(JNIEnv* env) {
    jclass local = env->FindClass("com/burntpeanut/core/NativeHooks");
    if (!local) return false;
    g_hooks_cls = reinterpret_cast<jclass>(env->NewGlobalRef(local));
    env->DeleteLocalRef(local);
    if (!g_hooks_cls) return false;

    g_onSend = env->GetStaticMethodID(g_hooks_cls, "onSend", "(J[B)I");
    g_onStartAdvertising = env->GetStaticMethodID(g_hooks_cls, "onStartAdvertising", "([B)I");
    g_onStopAdvertising = env->GetStaticMethodID(g_hooks_cls, "onStopAdvertising", "()I");
    g_onStartScanning = env->GetStaticMethodID(g_hooks_cls, "onStartScanning", "()I");
    g_onStopScanning = env->GetStaticMethodID(g_hooks_cls, "onStopScanning", "()I");
    g_onDisconnect = env->GetStaticMethodID(g_hooks_cls, "onDisconnect", "(J)I");
    g_onSignSecure = env->GetStaticMethodID(g_hooks_cls, "onSignSecure", "([B)[B");
    g_onGetPublicKey = env->GetStaticMethodID(g_hooks_cls, "onGetPublicKey", "()[B");
    g_onGetAttestation = env->GetStaticMethodID(g_hooks_cls, "onGetAttestation", "()[B");
    g_onHasSecureElement = env->GetStaticMethodID(g_hooks_cls, "onHasSecureElement", "()Z");
    g_onWriteChunk = env->GetStaticMethodID(g_hooks_cls, "onWriteChunk", "([BI[B)I");
    g_onReadChunk = env->GetStaticMethodID(g_hooks_cls, "onReadChunk", "([BI)[B");
    g_onHasChunk = env->GetStaticMethodID(g_hooks_cls, "onHasChunk", "([BI)Z");
    g_onDeleteFile = env->GetStaticMethodID(g_hooks_cls, "onDeleteFile", "([B)I");
    g_onAvailableSpace = env->GetStaticMethodID(g_hooks_cls, "onAvailableSpace", "()J");
    g_onNotifyTransferProgress = env->GetStaticMethodID(g_hooks_cls, "onNotifyTransferProgress", "(JI)V");
    g_onNotifyTransferComplete = env->GetStaticMethodID(g_hooks_cls, "onNotifyTransferComplete", "(J[B)V");
    g_onNotifyTransferFailed = env->GetStaticMethodID(g_hooks_cls, "onNotifyTransferFailed", "(JI)V");
    g_onNotifyPeerVerified = env->GetStaticMethodID(g_hooks_cls, "onNotifyPeerVerified", "(JZ)V");
    g_onNotifyForkDetected = env->GetStaticMethodID(g_hooks_cls, "onNotifyForkDetected", "([B)V");
    g_onNotifyBalanceChanged = env->GetStaticMethodID(g_hooks_cls, "onNotifyBalanceChanged", "(J)V");
    g_onNotifyGossipReceived = env->GetStaticMethodID(g_hooks_cls, "onNotifyGossipReceived", "(J)V");

    return g_onSend && g_onStartAdvertising && g_onStopAdvertising && g_onStartScanning &&
           g_onStopScanning && g_onDisconnect && g_onSignSecure && g_onGetPublicKey &&
           g_onGetAttestation && g_onHasSecureElement && g_onWriteChunk && g_onReadChunk &&
           g_onHasChunk && g_onDeleteFile && g_onAvailableSpace &&
           g_onNotifyTransferProgress && g_onNotifyTransferComplete && g_onNotifyTransferFailed &&
           g_onNotifyPeerVerified && g_onNotifyForkDetected && g_onNotifyBalanceChanged &&
           g_onNotifyGossipReceived;
}

}  // namespace

extern "C" JNIEXPORT jlong JNICALL
Java_com_burntpeanut_core_CoreBridge_nativeCreateNode(JNIEnv* env, jclass /*clazz*/, jstring jpath) {
    if (!jpath) {
        return 0;
    }
    if (!g_hooks_cls && !init_hook_ids(env)) {
        return 0;
    }
    const char* path = env->GetStringUTFChars(jpath, nullptr);
    if (!path) {
        return 0;
    }
    MLCallbacks cb = make_callbacks();
    MLNode node = ml_node_create(path, cb);
    env->ReleaseStringUTFChars(jpath, path);
    return static_cast<jlong>(node);
}

extern "C" JNIEXPORT void JNICALL
Java_com_burntpeanut_core_CoreBridge_nativeDestroyNode(JNIEnv* /*env*/, jclass /*clazz*/, jlong handle) {
    if (handle != 0) {
        ml_node_destroy(static_cast<MLNode>(handle));
    }
}

extern "C" JNIEXPORT void JNICALL
Java_com_burntpeanut_core_CoreBridge_nativeOnPeerDiscovered(JNIEnv* /*env*/, jclass /*clazz*/, jlong handle, jlong peerId) {
    if (handle != 0) {
        ml_on_peer_discovered(static_cast<MLNode>(handle), static_cast<uintptr_t>(peerId));
    }
}

extern "C" JNIEXPORT void JNICALL
Java_com_burntpeanut_core_CoreBridge_nativeOnPeerConnected(JNIEnv* /*env*/, jclass /*clazz*/, jlong handle, jlong peerId) {
    if (handle != 0) {
        ml_on_peer_connected(static_cast<MLNode>(handle), static_cast<uintptr_t>(peerId));
    }
}

extern "C" JNIEXPORT void JNICALL
Java_com_burntpeanut_core_CoreBridge_nativeOnPeerDisconnected(JNIEnv* /*env*/, jclass /*clazz*/, jlong handle, jlong peerId) {
    if (handle != 0) {
        ml_on_peer_disconnected(static_cast<MLNode>(handle), static_cast<uintptr_t>(peerId));
    }
}

extern "C" JNIEXPORT void JNICALL
Java_com_burntpeanut_core_CoreBridge_nativeOnDataReceived(JNIEnv* env, jclass /*clazz*/, jlong handle, jlong peerId, jbyteArray data) {
    if (handle == 0 || !data) return;
    const jsize len = env->GetArrayLength(data);
    if (len <= 0) return;
    std::vector<uint8_t> bytes(static_cast<size_t>(len));
    env->GetByteArrayRegion(data, 0, len, reinterpret_cast<jbyte*>(bytes.data()));
    ml_on_data_received(static_cast<MLNode>(handle),
                        static_cast<uintptr_t>(peerId),
                        bytes.data(),
                        static_cast<int32_t>(len));
}

extern "C" JNIEXPORT jbyteArray JNICALL
Java_com_burntpeanut_core_CoreBridge_nativeRequestFile(JNIEnv* env, jclass /*clazz*/, jlong handle, jbyteArray fileHash) {
    if (handle == 0 || !fileHash) return nullptr;
    const jsize len = env->GetArrayLength(fileHash);
    if (len <= 0) return nullptr;
    std::vector<uint8_t> hash(static_cast<size_t>(len));
    env->GetByteArrayRegion(fileHash, 0, len, reinterpret_cast<jbyte*>(hash.data()));
    MLResult r = ml_request_file(static_cast<MLNode>(handle), hash.data(), static_cast<int32_t>(len));
    __android_log_print(ANDROID_LOG_INFO, TAG, "nativeRequestFile rc=%d len=%d", static_cast<int>(r.error_code), static_cast<int>(r.len));
    if (r.error_code != ML_OK || r.data == nullptr || r.len <= 0) {
        if (r.data != nullptr) {
            ml_free(const_cast<uint8_t*>(r.data));
        }
        return nullptr;
    }
    jbyteArray out = env->NewByteArray(r.len);
    env->SetByteArrayRegion(out, 0, r.len, reinterpret_cast<const jbyte*>(r.data));
    ml_free(const_cast<uint8_t*>(r.data));
    return out;
}

extern "C" JNIEXPORT jint JNICALL
Java_com_burntpeanut_core_CoreBridge_nativeRequestFileCode(JNIEnv* env, jclass /*clazz*/, jlong handle, jbyteArray fileHash) {
    if (handle == 0 || !fileHash) return ML_ERR_INVALID_ARG;
    const jsize len = env->GetArrayLength(fileHash);
    if (len <= 0) return ML_ERR_INVALID_ARG;
    std::vector<uint8_t> hash(static_cast<size_t>(len));
    env->GetByteArrayRegion(fileHash, 0, len, reinterpret_cast<jbyte*>(hash.data()));
    MLResult r = ml_request_file(static_cast<MLNode>(handle), hash.data(), static_cast<int32_t>(len));
    const int32_t code = r.error_code;
    __android_log_print(ANDROID_LOG_INFO, TAG, "nativeRequestFileCode rc=%d len=%d", static_cast<int>(r.error_code), static_cast<int>(r.len));
    if (r.data != nullptr) {
        ml_free(const_cast<uint8_t*>(r.data));
    }
    return static_cast<jint>(code);
}

extern "C" JNIEXPORT jint JNICALL
Java_com_burntpeanut_core_CoreBridge_nativeShareFile(JNIEnv* env, jclass /*clazz*/, jlong handle, jbyteArray fileData, jstring fileName) {
    if (handle == 0 || !fileData || !fileName) {
        __android_log_print(ANDROID_LOG_ERROR, TAG, "nativeShareFile invalid arg handle=%lld fileData=%p fileName=%p",
                            static_cast<long long>(handle), fileData, fileName);
        return ML_ERR_INVALID_ARG;
    }
    const jsize len = env->GetArrayLength(fileData);
    if (len <= 0) {
        __android_log_print(ANDROID_LOG_ERROR, TAG, "nativeShareFile empty payload len=%d", static_cast<int>(len));
        return ML_ERR_INVALID_ARG;
    }
    std::vector<uint8_t> bytes(static_cast<size_t>(len));
    env->GetByteArrayRegion(fileData, 0, len, reinterpret_cast<jbyte*>(bytes.data()));
    const char* cName = env->GetStringUTFChars(fileName, nullptr);
    if (!cName) {
        __android_log_print(ANDROID_LOG_ERROR, TAG, "nativeShareFile GetStringUTFChars failed");
        return ML_ERR_INVALID_ARG;
    }
    __android_log_print(ANDROID_LOG_INFO, TAG, "nativeShareFile begin handle=%lld len=%d name=%s",
                        static_cast<long long>(handle), static_cast<int>(len), cName);
    const int32_t rc = ml_share_file(static_cast<MLNode>(handle), bytes.data(), static_cast<int32_t>(len), cName);
    __android_log_print(ANDROID_LOG_INFO, TAG, "nativeShareFile end rc=%d", static_cast<int>(rc));
    env->ReleaseStringUTFChars(fileName, cName);
    return rc;
}

jint JNI_OnLoad(JavaVM* vm, void* /*reserved*/) {
    g_vm = vm;
    JNIEnv* env = nullptr;
    if (vm->GetEnv(reinterpret_cast<void**>(&env), JNI_VERSION_1_6) != JNI_OK) {
        return JNI_ERR;
    }
    if (!init_hook_ids(env)) {
        return JNI_ERR;
    }
    return JNI_VERSION_1_6;
}
