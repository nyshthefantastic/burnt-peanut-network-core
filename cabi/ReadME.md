# C ABI Bridge (cabi/) — Concepts & Notes

## The Problem

The Go core (crypto, wire, storage, identity) is a library. It needs to run inside Android apps (Kotlin/Java) and iOS apps (Swift). But neither platform can call Go functions directly.

The solution: compile the Go core into a **C shared library** — both Android and iOS know how to call C functions.

```
Android App (Kotlin)          iOS App (Swift)
    ↓ JNI                        ↓ C interop
    └──────────┐    ┌────────────┘
               ↓    ↓
        C ABI Layer (cabi/)
               ↓
        Go Core (crypto, wire, storage, identity)
```

The cabi/ package is the **translator** between the C world and the Go world.

---

## Why Can't C and Go Talk Directly?

Two fundamental differences:

### 1. Memory Management

Go has **garbage collection** — you create objects and Go automatically cleans them up when they're no longer used. C has **manual memory** — you allocate and free everything yourself.

If Go garbage-collects an object that C is still using → crash.
If C frees memory that Go is still using → crash.

### 2. Types

Go has rich types: slices (`[]byte`), strings, structs with methods, interfaces.
C has primitive types: raw pointers, fixed-size arrays, plain structs.

You can't pass a Go `[]byte` to C. You can't return a Go `error` to C. Everything must be translated.

---

## How the Bridge Works

### Handles (handles.go)

Go's garbage collector can move objects in memory at any time. If C holds a raw pointer to a Go object and Go moves it → crash.

**Solution: handles.** Instead of giving C a pointer, give it a **number** (like a coat check ticket).

```
C calls:  ml_node_create(...)  → gets back handle 42
C calls:  ml_get_balance(42)   → registry finds the Go Node object
C calls:  ml_node_destroy(42)  → registry removes it, Go can garbage collect
```

The handle registry is a thread-safe map:

```
Handle 1 → *Node{...}
Handle 2 → *storage.Store{...}
Handle 3 → *DeviceIdentity{...}
```

C only ever sees numbers. Go objects stay safely inside Go's memory.

Three operations:

- RegisterHandle(obj) → handle number
- GetHandle(handle) → Go object
- ReleaseHandle(handle) → removes the object

### Error Mapping (errors.go)

In Go, errors are rich objects with messages: `errors.New("database is locked")`
In C, errors are just integer codes: `#define ML_ERR_DB 3`

Two translation functions:

- errorToCode(goError) → C integer code
- codeToError(intCode) → Go error

Error codes:

```
ML_OK              = 0   Success
ML_ERR_INVALID_ARG = 1   Bad input
ML_ERR_NOT_FOUND   = 2   Record/peer/file not found
ML_ERR_DB          = 3   Database error
ML_ERR_CRYPTO      = 4   Signing/verification error
ML_ERR_EXISTS      = 5   Already exists
ML_ERR_OVERFLOW    = 6   Size limit exceeded
ML_ERR_INTERNAL    = 7   Unknown/unexpected error
```

### C Header (core.h)

The contract between Go and native code. Defines:

- All function signatures C can call (ml_node_create, ml_get_balance, etc.)
- All callback function signatures Go can call back into native code
- All error codes
- The MLCallbacks struct (function pointers the native side provides)

This file is reviewed by the whole team because changes affect JNI bridges (Android) and Swift bridges (iOS).

### C Shims (shims.c)

Go (via CGo) cannot call C function pointers directly. Shims are tiny C wrapper functions that take a function pointer and call it. One shim per callback.

```c
// Go can't do: callbacks->send(data, len)
// Instead Go calls: ml_shim_send(callbacks->send, data, len)

void ml_shim_send(send_fn fn, uint8_t* data, int len) {
    fn(data, len);
}
```

### CGo Exports (exports.go)

The Go functions that C can call. Each one:

1. Receives C types (integers, raw pointers)
2. Converts to Go types (slices, structs)
3. Calls the actual Go logic
4. Converts the result back to C types
5. Returns a C-compatible result

Marked with `//export` comment so CGo makes them visible to C:

```go
//export ml_get_balance
func ml_get_balance(handle C.uintptr_t) C.int32_t {
    node := GetHandle(uintptr(handle))
    balance, err := node.GetBalance()
    if err != nil {
        return C.int32_t(errorToCode(err))
    }
    // ... return balance
}
```

### Callback Wrappers (callbacks.go)

The reverse direction — Go calling into native code. The native side provides function pointers for things Go can't do:

**Transport callbacks:**

- Send(data) — send bytes over Bluetooth/WiFi
- StartAdvertising / StopAdvertising — BLE advertising
- StartScanning / StopScanning — BLE scanning
- Disconnect(peerID)

**Hardware callbacks:**

- SignWithSecureKey(data) — sign using the hardware secure element
- GetPublicKey() — get the hardware-stored public key
- GetAttestation() — get platform attestation blob
- HasSecureElement() — check if hardware signing is available

**Storage callbacks (chunk-level):**

- WriteChunk / ReadChunk / HasChunk / DeleteFile — native file system for chunk storage
- AvailableSpace() — check remaining storage

**Notification callbacks:**

- NotifyTransferProgress / NotifyTransferComplete / NotifyTransferFailed
- NotifyPeerVerified / NotifyForkDetected
- NotifyBalanceChanged / NotifyGossipReceived

These are wrapped in a Go-friendly `NativeCallbacks` struct so the rest of the Go code can call them without knowing about C.

### Build Integration (Makefile)

Cross-compilation targets:

- Android ARM64 (.so shared library)
- Android x86_64 (.so for emulator)
- iOS ARM64 (.a static archive, c-archive mode)

---

## Data Flow Example

**User taps "Request File" on Android:**

```
1. Kotlin app calls ml_request_file(nodeHandle, fileHashPtr, fileHashLen)
       ↓
2. exports.go: ml_request_file receives C types
       ↓
3. Converts C pointer+length to Go []byte
       ↓
4. Calls node.RequestFile(fileHash)
       ↓
5. Transfer engine needs to send data over Bluetooth
       ↓
6. callbacks.go: NativeCallbacks.Send(data) wraps Go []byte to C pointer
       ↓
7. shims.c: ml_shim_send calls the function pointer
       ↓
8. Kotlin's Bluetooth implementation sends the bytes
```

**Response comes back:**

```
1. Kotlin receives bytes over Bluetooth
       ↓
2. Calls ml_on_data_received(nodeHandle, peerID, dataPtr, dataLen)
       ↓
3. exports.go: converts C types to Go types
       ↓
4. Routes to the correct transfer session
       ↓
5. Transfer completes → callback NotifyTransferComplete
       ↓
6. Kotlin updates the UI
```

---

## File Structure

```
cabi/
├── core.h            ← C header (the contract)
├── errors.go         ← Error code mapping (Go ↔ C)
├── handles.go        ← Handle registry (safe object references for C)
├── shims.c           ← C wrappers for calling function pointers
├── exports.go        ← //export functions C can call
├── callbacks.go      ← Go wrappers for calling native code
└── Makefile          ← Cross-compilation targets
```

## Memory Rules

1. Go objects NEVER leave Go. C gets handles (numbers) instead.
2. When C needs bytes, Go allocates a C buffer, copies data in, C must free it with ml_free().
3. When C passes bytes to Go, Go copies them immediately — never holds the pointer.
4. Every RegisterHandle must eventually have a ReleaseHandle, or memory leaks.

## Thread Safety

- The handle registry uses sync.Mutex — safe for concurrent access.
- The node event loop serializes all mutations — callbacks are safe.
- Multiple transfer sessions can look up handles simultaneously.
