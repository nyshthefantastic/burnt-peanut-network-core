//go:build cgo

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/nyshthefantastic/burnt-peanut-network-core/crypto"
	"github.com/nyshthefantastic/burnt-peanut-network-core/credit"
	"github.com/nyshthefantastic/burnt-peanut-network-core/storage"
	"github.com/nyshthefantastic/burnt-peanut-network-core/transfer"
	"github.com/nyshthefantastic/burnt-peanut-network-core/wire"
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

type cabiPeerTransport struct {
	peerID    uintptr
	callbacks *NativeCallbacks
	incoming  chan *pb.Envelope
}

func newCabiPeerTransport(peerID uintptr, callbacks *NativeCallbacks) *cabiPeerTransport {
	return &cabiPeerTransport{
		peerID:    peerID,
		callbacks: callbacks,
		incoming:  make(chan *pb.Envelope, 32),
	}
}

func (t *cabiPeerTransport) Send(env *pb.Envelope) error {
	if env == nil {
		return fmt.Errorf("nil envelope")
	}
	data, err := wire.EncodeEnvelope(env)
	if err != nil {
		return err
	}
	if rc := t.callbacks.Send(t.peerID, data); rc != ML_OK {
		return codeToError(rc)
	}
	return nil
}

func (t *cabiPeerTransport) Recv() (*pb.Envelope, error) {
	env, ok := <-t.incoming
	if !ok {
		return nil, fmt.Errorf("transport closed")
	}
	return env, nil
}

func (t *cabiPeerTransport) PeerID() string {
	return fmt.Sprintf("%d", t.peerID)
}

func (t *cabiPeerTransport) Close() error {
	select {
	case <-time.After(1 * time.Millisecond):
	default:
	}
	return nil
}

func (t *cabiPeerTransport) enqueue(env *pb.Envelope) {
	select {
	case t.incoming <- env:
	default:
		// Drop oldest by draining one and retrying once.
		select {
		case <-t.incoming:
		default:
		}
		select {
		case t.incoming <- env:
		default:
		}
	}
}

type cabiChainAppender struct {
	store *storage.Store
}

func (a cabiChainAppender) AppendRecord(record *pb.ShareRecord) error {
	return a.store.InsertRecord(record)
}

type cabiBalanceChecker struct {
	store *storage.Store
}

func (b cabiBalanceChecker) EffectiveBalance(records []*pb.ShareRecord, peerPubKey []byte, peerCreatedAt int64) int64 {
	params := credit.DefaultParams()
	now := time.Now().Unix()
	if len(records) == 0 {
		if fetched, err := b.store.GetRecordsByDevice(peerPubKey, 0, 1000); err == nil {
			records = fetched
		}
	}
	return credit.ComputeEffectiveBalance(records, peerPubKey, peerCreatedAt, now, params)
}

type cabiSigner struct {
	node *NodeContext
}

func (s cabiSigner) Sign(message []byte) ([]byte, error) {
	// For mobile MVP we always attempt callback signing first.
	// This works with either real secure element or temporary software-backed callbacks.
	if s.node.Callbacks != nil {
		if sig, code := s.node.Callbacks.SignWithSecureKey(message); code == ML_OK {
			return sig, nil
		}
	}
	return crypto.Sign(s.node.Identity.PrivateKey, message)
}

type cabiFileStorage struct {
	node *NodeContext
}

func (s cabiFileStorage) ReadChunk(fileHash []byte, chunkIndex uint32) ([]byte, error) {
	data, code := s.node.Callbacks.ReadChunk(fileHash, chunkIndex, 256*1024)
	if code != ML_OK {
		return nil, codeToError(code)
	}
	return data, nil
}

func (s cabiFileStorage) WriteChunk(fileHash []byte, chunkIndex uint32, data []byte) error {
	if code := s.node.Callbacks.WriteChunk(fileHash, chunkIndex, data); code != ML_OK {
		return codeToError(code)
	}
	return nil
}

func (s cabiFileStorage) HasChunk(fileHash []byte, chunkIndex uint32) (bool, error) {
	return s.node.Callbacks.HasChunk(fileHash, chunkIndex), nil
}

func ensurePeerTransport(node *NodeContext, peerID uintptr) *cabiPeerTransport {
	node.mu.Lock()
	defer node.mu.Unlock()
	if node.PeerTransports == nil {
		node.PeerTransports = make(map[uintptr]*cabiPeerTransport)
	}
	if t, ok := node.PeerTransports[peerID]; ok {
		return t
	}
	t := newCabiPeerTransport(peerID, node.Callbacks)
	node.PeerTransports[peerID] = t
	return t
}

func startSession(node *NodeContext, peerID uintptr, req *pb.TransferRequest, direction transfer.SessionDirection) error {
	t := ensurePeerTransport(node, peerID)
	sessionID := fmt.Sprintf("%d:%x", peerID, req.GetFileHash())
	peerKey := fmt.Sprintf("%d", peerID)
	s, ok := node.Transfer.Get(sessionID)
	if !ok {
		s = transfer.NewSession(
			peerKey,
			direction,
			req.GetFileHash(),
			t,
			cabiChainAppender{store: node.Store},
			cabiBalanceChecker{store: node.Store},
			cabiSigner{node: node},
		)
		s.ID = sessionID
		s.SetPolicyStore(node.Store)
		s.SetFileStorage(cabiFileStorage{node: node})
		s.SetLocalPubKey(node.Identity.Pubkey)
		if err := node.Transfer.Add(s); err != nil {
			return err
		}
		go func(sess *transfer.TransferSession) {
			_ = sess.RunSession(context.Background())
			node.Transfer.RemoveCompleted()
		}(s)
	}
	s.SetPendingRequest(req)
	return nil
}
