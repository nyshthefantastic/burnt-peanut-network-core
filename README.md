# Burnt Peanut Network Core

Peer-to-peer file sharing protocol where devices earn credit by sharing and spend credit to receive. No central server — all transfers happen directly between devices.

## Packages

**crypto/** — Cryptographic primitives. Ed25519 signing/verification, SHA-256 hashing, X25519 key exchange.

**wire/** — Protobuf message definitions and encode/decode helpers. The shared data format every other package depends on.

**storage/** — SQLite persistence layer. Stores records, peers, files, checkpoints, fork evidence, transfer requests, and device identity.

**identity/** — Device identity management. Keypair creation, key succession, and platform attestation verification.

**cabi/** — C ABI bridge for native mobile integration. Exposes the Go core as a shared library callable from Android (JNI) and iOS (Swift).

**dag/** — Chain data structures. ShareRecord construction/validation, chain append/walk, and fork detection.

**credit/** — Economic system. Drip allowance, diversity-weighted credit, time decay, per-peer caps, effective balance computation, and checkpoints.

**transfer/** — Transfer engine. State machine for file transfers, handshake protocol, service policy evaluation, chunk batching, and co-signing.

**gossip/** — Gossip protocol. State sync between peers, fork evidence propagation, and checkpoint propagation.

**discovery/** — File discovery. File index, salted hash advertising for BLE, and capability tokens.

**node/** — Node coordinator. Ties all subsystems together into a single event loop. Entry point for the C ABI.

## Protocol Layers

1. Identity: device-bound key material and attestations.
2. Record DAG: bilateral co-signed share records.
3. Credit: drip, diversity, decay, and per-peer caps.
4. Gossip: peer summaries, fork evidence, and checkpoint sync.
5. Discovery: salted advertisement prefixes and capability gating.
6. Transport: envelope exchange over BLE/WiFi Direct/TCP-like adapters.
7. Application: user-driven file share/request behavior.

## Quickstart

Run all tests:

```bash
go test ./...
```

Run CI checks:

```bash
make ci
```

## Policy Levels

- `POLICY_NONE`: no balance gate, only safety checks.
- `POLICY_LIGHT`: checkpoint + chain consistency + positive effective balance + no fork evidence.
- `POLICY_STRICT`: high-confidence checkpoint (K/D/F thresholds) + full credit checks + no fork evidence.
