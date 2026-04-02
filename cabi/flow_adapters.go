//go:build cgo

package main

import (
	"context"
	"fmt"
	"sync"
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
	preMu     sync.Mutex
	preRecv   []*pb.Envelope // FIFO pushed by PutBack, consumed before incoming
	delegateMu     sync.Mutex
	delegatePeer   *cabiPeerTransport // when set, Send/Recv forward to surviving link after BLE path change
	incomingClosed bool
}

func newCabiPeerTransport(peerID uintptr, callbacks *NativeCallbacks) *cabiPeerTransport {
	return &cabiPeerTransport{
		peerID:    peerID,
		callbacks: callbacks,
		incoming:  make(chan *pb.Envelope, 32),
	}
}

// linkDelegate drains queued envelopes into surv, then forwards Send/Recv/enqueue to surv and closes
// t.incoming so goroutines blocked on Recv continue on the survivor transport.
func (t *cabiPeerTransport) linkDelegate(surv *cabiPeerTransport) {
	if t == nil || surv == nil || t == surv {
		return
	}
	t.delegateMu.Lock()
	if t.incomingClosed {
		t.delegateMu.Unlock()
		return
	}
	t.preMu.Lock()
	for _, env := range t.preRecv {
		surv.enqueueLocked(env)
	}
	t.preRecv = nil
	t.preMu.Unlock()
	for {
		select {
		case env, ok := <-t.incoming:
			if !ok {
				t.delegatePeer = surv
				t.incomingClosed = true
				t.delegateMu.Unlock()
				return
			}
			surv.enqueueLocked(env)
		default:
			t.delegatePeer = surv
			t.incomingClosed = true
			close(t.incoming)
			t.delegateMu.Unlock()
			return
		}
	}
}

func (t *cabiPeerTransport) delegate() *cabiPeerTransport {
	t.delegateMu.Lock()
	defer t.delegateMu.Unlock()
	return t.delegatePeer
}

// delegateRoot follows delegate links to the active transport. A mistaken cycle would recurse
// until stack overflow when JNI calls stack with Send/Recv; guard with a seen set.
func (t *cabiPeerTransport) delegateRoot() (*cabiPeerTransport, error) {
	cur := t
	seen := make(map[*cabiPeerTransport]struct{})
	for {
		if cur == nil {
			return nil, fmt.Errorf("nil transport")
		}
		if _, dup := seen[cur]; dup {
			return nil, fmt.Errorf("cabiPeerTransport: delegate cycle")
		}
		seen[cur] = struct{}{}
		if d := cur.delegate(); d != nil {
			cur = d
			continue
		}
		return cur, nil
	}
}

func (t *cabiPeerTransport) Send(env *pb.Envelope) error {
	cur, err := t.delegateRoot()
	if err != nil {
		return err
	}
	if env == nil {
		return fmt.Errorf("nil envelope")
	}
	data, err := wire.EncodeEnvelope(env)
	if err != nil {
		return err
	}
	if rc := cur.callbacks.Send(cur.peerID, data); rc != ML_OK {
		return codeToError(rc)
	}
	return nil
}

func (t *cabiPeerTransport) takePreRecv() (*pb.Envelope, bool) {
	t.preMu.Lock()
	defer t.preMu.Unlock()
	if len(t.preRecv) == 0 {
		return nil, false
	}
	env := t.preRecv[0]
	t.preRecv = t.preRecv[1:]
	return env, true
}

func (t *cabiPeerTransport) Recv() (*pb.Envelope, error) {
	cur := t
	seen := make(map[*cabiPeerTransport]struct{})
	for {
		if cur == nil {
			return nil, fmt.Errorf("nil transport")
		}
		if _, dup := seen[cur]; dup {
			return nil, fmt.Errorf("cabiPeerTransport: delegate cycle in Recv")
		}
		seen[cur] = struct{}{}
		if env, ok := cur.takePreRecv(); ok {
			return env, nil
		}
		if d := cur.delegate(); d != nil {
			cur = d
			continue
		}
		env, ok := <-cur.incoming
		if !ok {
			return nil, fmt.Errorf("transport closed")
		}
		return env, nil
	}
}

func (t *cabiPeerTransport) TryRecv() (*pb.Envelope, bool) {
	cur := t
	seen := make(map[*cabiPeerTransport]struct{})
	for {
		if cur == nil {
			return nil, false
		}
		if _, dup := seen[cur]; dup {
			return nil, false
		}
		seen[cur] = struct{}{}
		if env, ok := cur.takePreRecv(); ok {
			return env, true
		}
		if d := cur.delegate(); d != nil {
			cur = d
			continue
		}
		select {
		case env, ok := <-cur.incoming:
			if !ok {
				return nil, false
			}
			return env, true
		default:
			return nil, false
		}
	}
}

func (t *cabiPeerTransport) PutBack(env *pb.Envelope) {
	if env == nil {
		return
	}
	cur, err := t.delegateRoot()
	if err != nil {
		return
	}
	cur.preMu.Lock()
	defer cur.preMu.Unlock()
	cur.preRecv = append([]*pb.Envelope{env}, cur.preRecv...)
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

func (t *cabiPeerTransport) enqueueLocked(env *pb.Envelope) {
	if env == nil {
		return
	}
	select {
	case t.incoming <- env:
	default:
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

func (t *cabiPeerTransport) enqueue(env *pb.Envelope) {
	if env == nil {
		return
	}
	cur, err := t.delegateRoot()
	if err != nil {
		return
	}
	cur.enqueueLocked(env)
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
		// Must set before RunSession: handleTransferring skips chunk work when pendingRequest is nil
		// (race caused sender to jump to CoSigning without ChunkBatch — matches "no chunks" on receiver).
		s.SetPendingRequest(req)
		if err := node.Transfer.Add(s); err != nil {
			return err
		}
		go func(sess *transfer.TransferSession) {
			if err := sess.RunSession(context.Background()); err != nil {
				fmt.Printf("[cabi][session] run failed sessionID=%s peer=%s dir=%s err=%v\n", sess.ID, sess.PeerID, sess.Direction, err)
			}
			node.Transfer.RemoveCompleted()
		}(s)
		return nil
	}
	s.SetPendingRequest(req)
	return nil
}
