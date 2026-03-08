#ifndef SHIMS_H
#define SHIMS_H

#include "core.h"

/* Transport */
int32_t ml_shim_send(ml_send_fn fn, uintptr_t peer_id, const uint8_t* data, int32_t len);
int32_t ml_shim_start_advertising(ml_start_advertising_fn fn, const uint8_t* payload, int32_t len);
int32_t ml_shim_stop_advertising(ml_stop_advertising_fn fn);
int32_t ml_shim_start_scanning(ml_start_scanning_fn fn);
int32_t ml_shim_stop_scanning(ml_stop_scanning_fn fn);
int32_t ml_shim_disconnect(ml_disconnect_fn fn, uintptr_t peer_id);

/* Hardware crypto */
int32_t ml_shim_sign_secure(ml_sign_secure_fn fn, const uint8_t* data, int32_t data_len,
                             uint8_t* sig_out, int32_t sig_out_len);
int32_t ml_shim_get_pubkey(ml_get_pubkey_fn fn, uint8_t* pubkey_out, int32_t pubkey_out_len);
int32_t ml_shim_get_attestation(ml_get_attestation_fn fn, uint8_t* att_out, int32_t att_out_len);
bool   ml_shim_has_secure_element(ml_has_secure_element_fn fn);

/* Chunk storage */
int32_t ml_shim_write_chunk(ml_write_chunk_fn fn, const uint8_t* file_hash, int32_t fh_len,
                             uint32_t chunk_index, const uint8_t* data, int32_t data_len);
int32_t ml_shim_read_chunk(ml_read_chunk_fn fn, const uint8_t* file_hash, int32_t fh_len,
                            uint32_t chunk_index, uint8_t* data_out, int32_t data_out_len);
bool   ml_shim_has_chunk(ml_has_chunk_fn fn, const uint8_t* file_hash, int32_t fh_len, uint32_t chunk_index);
int32_t ml_shim_delete_file(ml_delete_file_fn fn, const uint8_t* file_hash, int32_t fh_len);
int64_t ml_shim_available_space(ml_available_space_fn fn);

/* Notifications */
void ml_shim_notify_transfer_progress(ml_notify_transfer_progress_fn fn, uintptr_t peer_id, int32_t percent);
void ml_shim_notify_transfer_complete(ml_notify_transfer_complete_fn fn, uintptr_t peer_id,
                                       const uint8_t* file_hash, int32_t fh_len);
void ml_shim_notify_transfer_failed(ml_notify_transfer_failed_fn fn, uintptr_t peer_id, int32_t error_code);
void ml_shim_notify_peer_verified(ml_notify_peer_verified_fn fn, uintptr_t peer_id, bool valid);
void ml_shim_notify_fork_detected(ml_notify_fork_detected_fn fn, const uint8_t* device_pubkey, int32_t pk_len);
void ml_shim_notify_balance_changed(ml_notify_balance_changed_fn fn, int64_t new_balance);
void ml_shim_notify_gossip_received(ml_notify_gossip_received_fn fn, uintptr_t peer_id);

#endif /* SHIMS_H */