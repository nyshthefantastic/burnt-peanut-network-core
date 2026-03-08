#ifndef CORE_H
#define CORE_H

#include <stdint.h>
#include <stdbool.h>

#ifdef __cplusplus
extern "C" {
#endif

/* ─── Error Codes ─── */

#define ML_OK               0
#define ML_ERR_INVALID_ARG  1
#define ML_ERR_NOT_FOUND    2
#define ML_ERR_DB           3
#define ML_ERR_CRYPTO       4
#define ML_ERR_EXISTS       5
#define ML_ERR_OVERFLOW     6
#define ML_ERR_INTERNAL     7

/* ─── Opaque Handle ─── */

typedef uintptr_t MLNode;

/* ─── Result Type ─── */

typedef struct {
    const uint8_t* data;
    int32_t        len;
    int32_t        error_code;
} MLResult;

/* ─── Callback Function Pointers ─── */

// Transport
typedef int32_t (*ml_send_fn)(uintptr_t peer_id, const uint8_t* data, int32_t len);
typedef int32_t (*ml_start_advertising_fn)(const uint8_t* payload, int32_t len);
typedef int32_t (*ml_stop_advertising_fn)(void);
typedef int32_t (*ml_start_scanning_fn)(void);
typedef int32_t (*ml_stop_scanning_fn)(void);
typedef int32_t (*ml_disconnect_fn)(uintptr_t peer_id);

// Hardware crypto
typedef int32_t (*ml_sign_secure_fn)(const uint8_t* data, int32_t data_len,
                                     uint8_t* sig_out, int32_t sig_out_len);
typedef int32_t (*ml_get_pubkey_fn)(uint8_t* pubkey_out, int32_t pubkey_out_len);
typedef int32_t (*ml_get_attestation_fn)(uint8_t* att_out, int32_t att_out_len);
typedef bool    (*ml_has_secure_element_fn)(void);

// Chunk storage
typedef int32_t (*ml_write_chunk_fn)(const uint8_t* file_hash, int32_t fh_len,
                                     uint32_t chunk_index,
                                     const uint8_t* data, int32_t data_len);
typedef int32_t (*ml_read_chunk_fn)(const uint8_t* file_hash, int32_t fh_len,
                                    uint32_t chunk_index,
                                    uint8_t* data_out, int32_t data_out_len);
typedef bool    (*ml_has_chunk_fn)(const uint8_t* file_hash, int32_t fh_len,
                                   uint32_t chunk_index);
typedef int32_t (*ml_delete_file_fn)(const uint8_t* file_hash, int32_t fh_len);
typedef int64_t (*ml_available_space_fn)(void);

// Notifications
typedef void (*ml_notify_transfer_progress_fn)(uintptr_t peer_id, int32_t percent);
typedef void (*ml_notify_transfer_complete_fn)(uintptr_t peer_id,
                                               const uint8_t* file_hash, int32_t fh_len);
typedef void (*ml_notify_transfer_failed_fn)(uintptr_t peer_id, int32_t error_code);
typedef void (*ml_notify_peer_verified_fn)(uintptr_t peer_id, bool valid);
typedef void (*ml_notify_fork_detected_fn)(const uint8_t* device_pubkey, int32_t pk_len);
typedef void (*ml_notify_balance_changed_fn)(int64_t new_balance);
typedef void (*ml_notify_gossip_received_fn)(uintptr_t peer_id);

/* ─── Callbacks Struct ─── */

typedef struct {
    // Transport
    ml_send_fn              send;
    ml_start_advertising_fn start_advertising;
    ml_stop_advertising_fn  stop_advertising;
    ml_start_scanning_fn    start_scanning;
    ml_stop_scanning_fn     stop_scanning;
    ml_disconnect_fn        disconnect;

    // Hardware crypto
    ml_sign_secure_fn       sign_with_secure_key;
    ml_get_pubkey_fn        get_public_key;
    ml_get_attestation_fn   get_attestation;
    ml_has_secure_element_fn has_secure_element;

    // Chunk storage
    ml_write_chunk_fn       write_chunk;
    ml_read_chunk_fn        read_chunk;
    ml_has_chunk_fn         has_chunk;
    ml_delete_file_fn       delete_file;
    ml_available_space_fn   available_space;

    // Notifications
    ml_notify_transfer_progress_fn  notify_transfer_progress;
    ml_notify_transfer_complete_fn  notify_transfer_complete;
    ml_notify_transfer_failed_fn    notify_transfer_failed;
    ml_notify_peer_verified_fn      notify_peer_verified;
    ml_notify_fork_detected_fn      notify_fork_detected;
    ml_notify_balance_changed_fn    notify_balance_changed;
    ml_notify_gossip_received_fn    notify_gossip_received;
} MLCallbacks;

/* ─── Node Lifecycle ─── */

MLNode  ml_node_create(const char* db_path, MLCallbacks callbacks);
void    ml_node_destroy(MLNode node);

/* ─── User Actions ─── */

MLResult ml_request_file(MLNode node, const uint8_t* file_hash, int32_t len);
MLResult ml_get_balance(MLNode node);
MLResult ml_get_chain_summary(MLNode node);
int32_t  ml_set_service_policy(MLNode node, int32_t policy);
MLResult ml_get_peers(MLNode node);
MLResult ml_get_file_index(MLNode node);
int32_t  ml_share_file(MLNode node, const uint8_t* file_data, int32_t len,
                        const char* file_name);

/* ─── Transport Events ─── */

void ml_on_peer_discovered(MLNode node, uintptr_t peer_id);
void ml_on_peer_connected(MLNode node, uintptr_t peer_id);
void ml_on_peer_disconnected(MLNode node, uintptr_t peer_id);
void ml_on_data_received(MLNode node, uintptr_t peer_id,
                          const uint8_t* data, int32_t len);

/* ─── Memory ─── */

void ml_free(void* ptr);

#ifdef __cplusplus
}
#endif

#endif