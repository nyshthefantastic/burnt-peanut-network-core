# Keystore Migration Follow-up

This app currently uses temporary software signing via `NativeHooks.onSignSecure(...)`.
Use this checklist to migrate to Android Keystore-backed signing.

## Scope

- Replace software keypair generation in `NativeHooks` with Android Keystore key material.
- Keep callback contract unchanged so C ABI and Go core do not need changes.

## Tasks

1. Keystore key lifecycle
   - Create/load Ed25519 key alias (or approved curve/signature suite for target SDK matrix).
   - Enforce non-exportable private key storage.
   - Handle key invalidation/rotation events.

2. Callback migration
   - Update `NativeHooks.onSignSecure(data)` to call `Signature` with Keystore private key.
   - Update `onGetPublicKey()` to return canonical public key bytes used by C ABI identity path.
   - Keep `onHasSecureElement()` aligned with actual key hardware backing capability.

3. Attestation path
   - Implement `onGetAttestation()` with certificate chain / attestation blob required by product policy.
   - Validate blob size/encoding constraints expected by native layer.

4. Fallback policy
   - If Keystore is unavailable, decide explicit behavior:
     - fail closed (preferred for production), or
     - guarded temporary fallback with telemetry.
   - Add clear logs and metrics for fallback events.

5. Test plan
   - Instrumented test: sign/verify roundtrip against pubkey returned from callback.
   - Restart test: key survives app restart and signatures remain valid.
   - Device matrix: at least one hardware-backed device + one emulator fallback path.

6. Rollout gate
   - Remove software-key code path from production build flavor.
   - Keep debug-only fallback if needed behind build flag.
