# Wire Package — Concepts & Notes

## The Problem

Devices in this project communicate over Bluetooth/WiFi. They need to send structured data (like "I want this file" or "here's a signed receipt") over a connection that only understands **raw bytes**.

Two problems to solve:

1. How to convert Go structs into bytes and back
2. How to tell where one message ends and the next begins in a byte stream

---

## Protocol Buffers (Protobuf)

### Why not JSON?

|                | JSON                                   | Protobuf                                    |
| -------------- | -------------------------------------- | ------------------------------------------- |
| Format         | Text (human readable)                  | Binary (compact)                            |
| Field names    | Sent every time (`"file_hash": "abc"`) | Replaced by small numbers (`field 1 = abc`) |
| Size           | Large                                  | ~3-5x smaller                               |
| Parse speed    | Slow (scan text, match names)          | Fast (binary, no name matching)             |
| Type safety    | Loose                                  | Strict                                      |
| Cross-language | Yes                                    | Yes                                         |

For two phones on Bluetooth, every byte matters. Protobuf is smaller and faster.

### The .proto File

The `.proto` file is a **language-neutral template**. It defines message types and their fields. It is NOT Go code — it's a contract that all sides agree on.

```protobuf
syntax = "proto3";
package burntPeanut;
option go_package = "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen";

message TransferRequest {
  bytes file_hash = 1;
  bytes requester_pubkey = 2;
  int64 timestamp = 3;
}
```

Key parts of the header:

- `syntax = "proto3"` — protobuf version 3
- `package burntPeanut` — protobuf namespace (NOT a Go package)
- `go_package` — tells the code generator where to put generated Go files and what Go package name to use

### Field Numbers

Each field has a number (`= 1`, `= 2`, etc.). Protobuf sends these numbers on the wire **instead of field names**. The receiver looks up "field 1" in the `.proto` template to know it means `file_hash`.

**IMPORTANT: Never change field numbers after releasing.** Old devices expect field 1 to be `file_hash`. Changing it would break everything.

### Protobuf Types

| Proto type | Go type  | Used for                                   |
| ---------- | -------- | ------------------------------------------ |
| `bytes`    | `[]byte` | Raw binary data (keys, hashes, signatures) |
| `string`   | `string` | Human-readable text (file names)           |
| `uint64`   | `uint64` | Unsigned numbers (sizes, indices)          |
| `int64`    | `int64`  | Signed numbers (timestamps, balances)      |
| `uint32`   | `uint32` | Smaller unsigned numbers (chunk indices)   |
| `int32`    | `int32`  | Smaller signed numbers                     |
| `bool`     | `bool`   | True/false flags                           |

### Keywords

**`repeated`** — means an array/slice.

- `repeated bytes chunk_hashes` → `[][]byte` in Go
- `repeated PeerInfo peers` → `[]*PeerInfo` in Go

**`oneof`** — means exactly ONE of the options. Like a physical envelope that holds one letter, not many.

```protobuf
message Envelope {
  oneof payload {
    HandshakeMsg handshake = 1;
    TransferRequest transfer_request = 2;
  }
}
```

An Envelope carries either a handshake OR a transfer request, never both.

**`enum`** — a fixed set of named values.

```protobuf
enum ServicePolicy {
  POLICY_NONE = 0;
  POLICY_LIGHT = 1;
  POLICY_STRICT = 2;
}
```

### Code Generation

```
.proto file (template)
    ↓  protoc + protoc-gen-go (run via: make proto)
wire/gen/meshledger.pb.go (auto-generated Go structs)
```

`protoc` reads the `.proto` file and generates Go structs with serialization built in. You never edit the generated file — if you need changes, edit the `.proto` and regenerate.

All engineers import the same generated types:

```go
import pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
```

### Using Generated Types

Creating a message:

```go
req := &pb.TransferRequest{
    FileHash:       someHash,
    RequesterPubkey: myKey,
    Timestamp:       time.Now().Unix(),
}
```

Serializing (struct → bytes):

```go
data, err := proto.Marshal(req)
```

Deserializing (bytes → struct):

```go
req := &pb.TransferRequest{}
err := proto.Unmarshal(data, req)
```

`proto.Unmarshal` takes a **pointer** (`&req`) because it needs to write into the struct. Without `&`, it gets a copy and the original stays empty.

---

## Length-Prefix Framing (codec.go)

### The Problem

Protobuf handles converting structs to bytes. But when sending multiple messages over a stream (Bluetooth), the receiver sees a continuous flow of bytes:

```
[message1bytes][message2bytes][message3bytes]
```

How does it know where one message ends and the next begins?

### The Solution

**Length-prefix framing.** Before each message, send a 4-byte header containing the message length:

```
[4 bytes: length][payload bytes][4 bytes: length][payload bytes]...
```

The receiver:

1. Reads 4 bytes → knows the payload is N bytes long
2. Reads exactly N bytes → that's one complete message
3. Repeat

### Why not use a separator?

A separator (like `\n`) only works if that byte never appears inside the message data. With binary data, any byte value (0-255) could appear in the payload, so there's no safe separator.

### Four Functions

| Function         | Input                      | Output                     | Used when                      |
| ---------------- | -------------------------- | -------------------------- | ------------------------------ |
| `EncodeEnvelope` | Envelope struct            | `[]byte` (length-prefixed) | You have a struct, need bytes  |
| `DecodeEnvelope` | `[]byte` (length-prefixed) | Envelope struct            | You have all bytes in memory   |
| `WriteEnvelope`  | Envelope struct + stream   | writes to stream           | Sending over live connection   |
| `ReadEnvelope`   | stream                     | Envelope struct            | Receiving from live connection |

**Encode/Decode** — work with `[]byte`. All data is already in memory. Like reading a fully loaded file.

**Read/Write** — work with `io.Reader`/`io.Writer`. Data arrives gradually over a live connection. Like a phone call where you listen and wait.

### io.ReadFull vs io.Read

```go
// May return fewer bytes than requested (whatever is available now)
n, err := r.Read(buffer)

// Keeps reading until ALL requested bytes arrive (or error)
n, err := io.ReadFull(r, buffer)
```

`io.ReadFull` is essential for streaming — it waits until the complete header or payload arrives.

### MaxMessageSize

All functions check against `MaxMessageSize` (16MB) to prevent:

- `EncodeEnvelope`: accidentally creating a huge message
- `DecodeEnvelope`: processing a corrupted length field
- `ReadEnvelope`: a malicious device claiming a 2GB payload, causing out-of-memory

---

## File Structure

```
wire/
├── proto/
│   └── meshledger.proto      ← the template (source of truth)
├── gen/
│   └── meshledger.pb.go      ← auto-generated (never edit by hand)
└── codec.go                  ← length-prefix framing helpers
```

## Full Data Flow

```
Go struct
  ↓ proto.Marshal (protobuf serialization)
Raw bytes
  ↓ EncodeEnvelope (add 4-byte length header)
Length-prefixed bytes
  ↓ WriteEnvelope (send to stream)
Bluetooth / WiFi
  ↓ ReadEnvelope (read from stream)
Length-prefixed bytes
  ↓ DecodeEnvelope (strip header, unmarshal)
Go struct
```
