# Burnt Peanut Network

A peer-to-peer file sharing protocol engine written in Go. Devices earn credit by sharing files and spend credit to receive - no central server, no accounts, no cloud. All transfers happen directly between devices over Bluetooth or WiFi.

This repository contains the Go core library that compiles into a C shared library (`.so` / `.a`) for native integration with Android and iOS apps via JNI and Swift, respectively.

---

## What This Project Does

Burnt Peanut Network enables serverless, offline-first file sharing between mobile devices. The protocol is built around three ideas:

1. **Bilateral records** - Every file transfer produces a co-signed `ShareRecord` that both sender and receiver agree on. These records form an append-only chain per device (a DAG), making it possible to verify a device's history without a server.

2. **Credit-based economics** - Devices start with a small drip allowance and earn more credit by sharing files with diverse peers. The system uses time decay, per-peer caps, and diversity weighting to discourage collusion and freeloading.

3. **Gossip-based reputation** - When two devices connect, they exchange summaries of peers they've interacted with. Fork evidence (a device presenting two different histories) propagates through the network, and forked devices are rejected by honest peers.

---

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                 Native App (Kotlin / Swift)          │
│       BLE transport, chunk storage, UI, notifications│
└──────────────────────┬──────────────────────────────┘
                       │ C ABI (core.h)
┌──────────────────────┴──────────────────────────────┐
│                    cabi/                              │
│   CGo exports, callback wrappers, handle registry    │
├──────────────────────────────────────────────────────┤
│  node/         │  transfer/      │  gossip/          │
│  Coordinator   │  State machine  │  State sync       │
│  Event loop    │  Handshake      │  Fork propagation  │
│  Routing       │  Batch transfer │  Checkpoint relay  │
│                │  Co-signing     │                    │
│                │  Policy eval    │                    │
├────────────────┼─────────────────┼───────────────────┤
│  dag/          │  credit/        │  discovery/        │
│  Record types  │  Drip allowance │  File index        │
│  Validation    │  Diversity wt.  │  Salted hash ads   │
│  Chain verify  │  Time decay     │  Capability tokens │
│  Fork detect   │  Per-peer caps  │                    │
│                │  Checkpoints    │                    │
├────────────────┴─────────────────┴───────────────────┤
│  crypto/          │  wire/           │  storage/       │
│  Ed25519 sign     │  Protobuf schema │  SQLite + WAL   │
│  SHA-256 hash     │  Codec (len-pfx) │  Dual conn pool │
│  X25519 ECDH      │  Envelope framing│  Migrations     │
└───────────────────┴──────────────────┴─────────────────┘
```

---

## Packages

### Foundation Layer

**`crypto/`** - Ed25519 signing/verification, SHA-256 hashing (including chunk hash aggregation), and X25519 ECDH key exchange for encrypted session setup.

**`wire/`** - Protobuf message definitions (`core.proto`) covering all protocol types: `ShareRecord`, `TransferRequest`, `FileMeta`, `Checkpoint`, `ForkEvidence`, `GossipPayload`, `HandshakeMsg`, `ChunkBatch`, and `Envelope`. Includes a codec with 4-byte length-prefixed framing for streaming over transports.

**`storage/`** - SQLite persistence with WAL mode enabled. Dual connection pools (1 writer, N readers) for concurrent access. Sequential migration framework. Full CRUD across all entity types: records, peers, files, checkpoints, fork evidence, transfer requests, transfer state, and device identity.

### Protocol Layer

**`dag/`** - ShareRecord construction and validation (dual-signature verification, ID recomputation, cumulative total consistency). Chain segment verification for contiguous record sequences. Fork detection when two records from the same device share an index but differ in content.

**`credit/`** - The economic engine. Computes effective balance from: drip allowance (`min(rate × age, max)`), diversity-weighted credit (counterparty frequency weighting over a sliding window), time decay (half-life exponential), and per-peer epoch caps. Checkpoint creation and witness-based confidence scoring (geographic cluster diversity).

**`identity/`** - Device identity lifecycle. Generates Ed25519 keypairs, persists to storage, and supports key succession (old key signs a record linking to the new key, enabling credit carry-forward). Includes attestation verification structure for Android Play Integrity / iOS App Attest (platform-specific chain validation deferred to native side).

### Network Layer

**`transfer/`** - File transfer state machine with states: `IDLE → HANDSHAKE → VERIFYING → TRANSFERRING → CO_SIGNING → GOSSIPING → COMPLETE`. Includes handshake protocol (ephemeral key exchange + policy advertisement), three-tier service policy evaluation (`NONE` / `LIGHT` / `STRICT`), chunk batching (up to 64 chunks per batch), co-signing flow, and session recovery for interrupted transfers.

**`gossip/`** - Piggybacked state sync on peer connections. Exchanges peer summaries, fork evidence, file metadata, and checkpoints. Byte-budget prioritization: fork evidence first, then peer summaries for mutual contacts, then file metadata.

**`discovery/`** - File index tracking, salted hash advertising for BLE (4-byte prefix with rotating 8-byte salt for privacy), and capability tokens for access control (signed, time-bounded, grantee-specific or bearer).

### Integration Layer

**`node/`** - Coordinator that ties all subsystems together. Runs a single event loop goroutine that serializes chain mutations. Routes incoming transport events to the correct transfer session or gossip engine. Handles user actions (request file, share file, get balance, set policy). Periodic checkpoint creation.

**`cabi/`** - C ABI bridge that exposes the Go core as a shared library. Thread-safe handle registry, C shims for calling native function pointers from Go, CGo exports for all public API functions, and callback wrappers for transport, hardware crypto, chunk storage, and notification events. Guarded with `CORE_GO_EXPORTS` preprocessor define to prevent CGo declaration conflicts.

**`integration/`** - End-to-end tests using in-process mock transports. Covers the full flow: identity creation → file share → transfer request → handshake → batch transfer → co-signing → chain verification → gossip exchange → fork detection.

---

## C ABI Surface

The native app interacts with the Go core through a C header (`cabi/core.h`). Key functions:

```c
// Lifecycle
MLNode  ml_node_create(const char* db_path, MLCallbacks callbacks);
void    ml_node_destroy(MLNode node);

// User actions
MLResult ml_request_file(MLNode node, const uint8_t* file_hash, int32_t len);
MLResult ml_get_balance(MLNode node);
int32_t  ml_set_service_policy(MLNode node, int32_t policy);
int32_t  ml_share_file(MLNode node, const uint8_t* data, int32_t len, const char* name);
MLResult ml_get_peers(MLNode node);
MLResult ml_get_file_index(MLNode node);

// Transport events (called by native BLE/WiFi layer)
void ml_on_peer_discovered(MLNode node, uintptr_t peer_id);
void ml_on_peer_connected(MLNode node, uintptr_t peer_id);
void ml_on_peer_disconnected(MLNode node, uintptr_t peer_id);
void ml_on_data_received(MLNode node, uintptr_t peer_id, const uint8_t* data, int32_t len);

// Memory
void ml_free(void* ptr);
```

The native app provides callbacks for transport (send, advertise, scan, disconnect), hardware crypto (secure element signing, attestation), chunk storage (read/write/delete), and UI notifications (transfer progress, balance changes, fork detection).

---

## Build

**Prerequisites:** Go 1.25+, `protoc` with `protoc-gen-go`, Android NDK (for Android builds).

```bash
# Regenerate protobuf types
make proto

# Run tests (requires CGo for SQLite)
make test

# Build Android shared libraries (arm64 + x86_64)
make build-android-lib

# Build iOS static library (arm64)
make build-ios

# Build all targets
make build-all
```

The Android build script (`scripts/build-android-lib.sh`) handles NDK toolchain detection and cross-compilation. Output goes to `build/android/{arm64-v8a,x86_64}/libcore.so`.

---

## Tests

```bash
go test ./...
```

Test coverage spans all packages:

| Package        | What's Tested                                                                                                 |
| -------------- | ------------------------------------------------------------------------------------------------------------- |
| `crypto/`      | Ed25519 sign/verify round-trip, SHA-256 known vectors, ECDH shared secret derivation                          |
| `credit/`      | Checkpoint creation, balance computation, drip accrual                                                        |
| `transfer/`    | State machine transitions, policy evaluation (NONE/LIGHT/STRICT), batch construction, chunk hash verification |
| `discovery/`   | Salted hash matching, capability token validation (valid, expired, wrong grantee)                             |
| `gossip/`      | Payload construction, fork evidence propagation, state sync                                                   |
| `node/`        | Node lifecycle, event routing, transfer handling, fork detection                                              |
| `cabi/`        | Flow adapter transport merging and delegation                                                                 |
| `integration/` | Full two-node end-to-end flow with mock transport                                                             |

---

## Project Structure

```
burnt-peanut-network-core/
├── crypto/               # Cryptographic primitives (Ed25519, SHA-256, X25519)
├── wire/
│   ├── proto/core.proto  # Protobuf schema (source of truth for all types)
│   ├── gen/              # Generated Go types (do not edit)
│   └── codec.go          # Length-prefixed envelope encoding
├── storage/              # SQLite layer (WAL, migrations, full CRUD)
├── identity/             # Device identity, key succession, attestation
├── dag/                  # Record validation, chain integrity, fork detection
├── credit/               # Economic system (drip, diversity, decay, caps, checkpoints)
├── transfer/             # Transfer state machine, handshake, policy, batching
├── gossip/               # Peer state sync, fork propagation
├── discovery/            # File index, salted hash ads, capability tokens
├── node/                 # Coordinator, event loop, routing
├── cabi/                 # C ABI bridge (exports, shims, callbacks, handles)
├── integration/          # End-to-end tests
├── android/              # Android demo app (Kotlin, BLE transport, JNI bridge)
├── scripts/              # Build scripts (Android cross-compilation, CI)
├── Makefile              # Proto codegen, cross-compilation, test targets
└── go.mod
```

---

## Tech Stack

- **Language:** Go
- **Serialization:** Protocol Buffers (protobuf)
- **Storage:** SQLite via `mattn/go-sqlite3` (CGo)
- **Cryptography:** Go stdlib `crypto/ed25519`, `crypto/ecdh` (X25519), `crypto/sha256`
- **Mobile integration:** CGo with `c-shared` build mode
- **Android:** Kotlin + JNI + BLE

---

## Key Design Decisions

- **c-shared over gomobile** - The core compiles to a plain C shared library. This gives full control over the ABI boundary and works with any language that can call C functions (JNI, Swift, Flutter FFI, etc.).

- **Dual-signature co-signing** - Every `ShareRecord` requires signatures from both sender and receiver. The `visibility` field is included in the signed bytes, meaning both parties must agree on whether a transfer is public or private. This prevents unilateral history rewriting.

- **Single-writer event loop** - All chain mutations (record appends, fork evidence inserts, checkpoint stores) flow through a single goroutine via channels. This eliminates lock contention on the critical path and makes the mutation order deterministic.

- **Interface-driven testing** - The transfer engine depends on interfaces (`ChainAppender`, `BalanceChecker`, `Signer`, `Transport`, `FileStorage`) rather than concrete types. This allows full state machine testing with mocks before integration with real implementations.

- **Private key in DB (temporary)** - The device's Ed25519 private key is currently stored in SQLite. This is a known shortcut; the intended path is hardware-backed signing via the `cabi` callback to Android Keystore / iOS Secure Enclave.

---

## Status

The Go core library is feature-complete across all protocol layers. An Android demo app with BLE transport is included. Current areas for future work:

- Hardware-backed signing integration (Android Keystore, iOS Secure Enclave)
- Platform attestation chain validation (Play Integrity, App Attest)
- Multi-source chunk downloads from different peers
- Trusted time source for stronger replay protection
- AddressSanitizer / leak testing across the C boundary
