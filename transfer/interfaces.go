package transfer

import (
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

// ChainAppender is implemented by storage.Store in production.
// The transfer engine calls this after co-signing to persist a new record.
type ChainAppender interface {
	AppendRecord(record *pb.ShareRecord) error
}

// BalanceChecker is implemented by credit.ComputeEffectiveBalance in production.
// The transfer engine calls this during handshake to decide whether to serve a peer.
type BalanceChecker interface {
	EffectiveBalance(records []*pb.ShareRecord, peerPubKey []byte, peerCreatedAt int64) int64
}

// Signer is implemented by crypto.Sign (tests) or cabi hardware callback (mobile).
// Signing may be async due to hardware round-trips — always returns ([]byte, error).
type Signer interface {
	Sign(message []byte) (signature []byte, err error)
}

// Transport abstracts the underlying connection (BLE, WiFi Direct, TCP).
// It wraps wire.WriteEnvelope / wire.ReadEnvelope.
type Transport interface {
	Send(env *pb.Envelope) error
	Recv() (*pb.Envelope, error)
	// TryRecv returns a queued envelope without blocking; ok is false when none is ready.
	TryRecv() (env *pb.Envelope, ok bool)
	// PutBack returns a non-consumed envelope to the head of the receive order (e.g. after draining ChunkBatches).
	PutBack(env *pb.Envelope)
	PeerID() string
	Close() error
}

// FileStorage abstracts chunk-level file reads/writes for transfer batching.
type FileStorage interface {
	ReadChunk(fileHash []byte, chunkIndex uint32) ([]byte, error)
	WriteChunk(fileHash []byte, chunkIndex uint32, data []byte) error
	HasChunk(fileHash []byte, chunkIndex uint32) (bool, error)
}
