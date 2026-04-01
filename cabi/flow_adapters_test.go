//go:build cgo

package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nyshthefantastic/burnt-peanut-network-core/identity"
	"github.com/nyshthefantastic/burnt-peanut-network-core/storage"
	"github.com/nyshthefantastic/burnt-peanut-network-core/transfer"
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

func testNodeContext(t *testing.T) *NodeContext {
	t.Helper()
	db, err := storage.OpenDatabase(filepath.Join(t.TempDir(), "cabi-flow.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	dev, err := identity.NewIdentity(db)
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	return &NodeContext{
		Store:          db,
		Identity:       dev,
		Transfer:       transfer.NewSessionManager(4),
		SessionKeys:    make(map[uintptr][]byte),
		SharedSecrets:  make(map[uintptr][]byte),
		PeerTransports: make(map[uintptr]*cabiPeerTransport),
	}
}

func TestEnsurePeerTransportReuse(t *testing.T) {
	node := testNodeContext(t)
	first := ensurePeerTransport(node, 7)
	second := ensurePeerTransport(node, 7)
	if first == nil || second == nil {
		t.Fatalf("expected non-nil transport")
	}
	if first != second {
		t.Fatalf("expected transport reuse for same peer")
	}
}

func TestStartSessionAddsTransferSession(t *testing.T) {
	node := testNodeContext(t)
	req := &pb.TransferRequest{
		RequesterPubkey: node.Identity.Pubkey,
		FileHash:        []byte("file-hash"),
		ChunkIndices:    nil,
		Nonce:           []byte("nonce"),
		Timestamp:       time.Now().Unix(),
	}
	if err := startSession(node, 42, req, transfer.DirectionOutbound); err != nil {
		t.Fatalf("start session: %v", err)
	}
	if node.Transfer.ActiveCount() != 1 {
		t.Fatalf("expected exactly one active session, got %d", node.Transfer.ActiveCount())
	}
}
