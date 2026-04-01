package transfer

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	mlcrypto "github.com/nyshthefantastic/burnt-peanut-network-core/crypto"
	"github.com/nyshthefantastic/burnt-peanut-network-core/dag"
	"github.com/nyshthefantastic/burnt-peanut-network-core/storage"
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

// --- Mocks ---

type mockChainAppender struct {
	records []*pb.ShareRecord
	err     error
}

func (m *mockChainAppender) AppendRecord(record *pb.ShareRecord) error {
	if m.err != nil {
		return m.err
	}
	m.records = append(m.records, record)
	return nil
}

type mockBalanceChecker struct {
	value int64
}

func (m *mockBalanceChecker) EffectiveBalance(records []*pb.ShareRecord, peerPubKey []byte, peerCreatedAt int64) int64 {
	return m.value
}

type mockSigner struct {
	sig []byte
	err error
	priv []byte
}

func (m *mockSigner) Sign(message []byte) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	if len(m.priv) > 0 {
		return mlcrypto.Sign(m.priv, message)
	}
	return m.sig, nil
}

type mockTransport struct {
	peerID     string
	recvQueue  []*pb.Envelope
	recvErr    error
}

func (m *mockTransport) Send(env *pb.Envelope) error {
	return nil
}

func (m *mockTransport) Recv() (*pb.Envelope, error) {
	if m.recvErr != nil {
		return nil, m.recvErr
	}
	if len(m.recvQueue) == 0 {
		return nil, context.Canceled
	}
	env := m.recvQueue[0]
	m.recvQueue = m.recvQueue[1:]
	return env, nil
}

func (m *mockTransport) PeerID() string {
	return m.peerID
}

func (m *mockTransport) Close() error {
	return nil
}

// --- Tests ---

func TestTransferStateTransitionsValid(t *testing.T) {
	s := NewSession(
		"peer-1",
		DirectionOutbound,
		[]byte("file-hash"),
		&mockTransport{peerID: "peer-1"},
		&mockChainAppender{},
		&mockBalanceChecker{value: 1},
		&mockSigner{sig: []byte("ok")},
	)

	if s.State != StateIdle {
		t.Fatalf("expected initial state %q, got %q", StateIdle, s.State)
	}

	sequence := []TransferState{
		StateHandshake,
		StateVerifying,
		StateTransferring,
		StateCoSigning,
		StateGossiping,
		StateComplete,
	}

	for _, next := range sequence {
		if err := s.TransitionTo(next); err != nil {
			t.Fatalf("transition to %q failed: %v", next, err)
		}
		if s.State != next {
			t.Fatalf("expected state %q, got %q", next, s.State)
		}
	}
}

func TestTransferStateTransitionsInvalid(t *testing.T) {
	s := NewSession(
		"peer-1",
		DirectionOutbound,
		[]byte("file-hash"),
		&mockTransport{peerID: "peer-1"},
		&mockChainAppender{},
		&mockBalanceChecker{value: 1},
		&mockSigner{sig: []byte("ok")},
	)

	if err := s.TransitionTo(StateTransferring); err == nil {
		t.Fatalf("expected error for invalid transition Idle -> Transferring, got nil")
	}

	if s.State != StateIdle {
		t.Fatalf("state changed on invalid transition, expected %q got %q", StateIdle, s.State)
	}
}

func TestRunSessionHappyPath(t *testing.T) {
	localPub, localPriv, err := mlcrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate local keys: %v", err)
	}
	peerPub, peerPriv, err := mlcrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate peer keys: %v", err)
	}

	record := &pb.ShareRecord{
		SenderPubkey:      peerPub,
		ReceiverPubkey:    localPub,
		SenderRecordIndex: 1,
		ReceiverRecordIndex: 1,
		RequestHash:       []byte("0123456789abcdef0123456789abcdef"),
		FileHash:          []byte("file-hash"),
		BytesTotal:        7,
		Timestamp:         123,
		Visibility:        pb.Visibility_VISIBILITY_PUBLIC,
	}
	peerSig, err := mlcrypto.Sign(peerPriv, dag.SignableBytes(record))
	if err != nil {
		t.Fatalf("sign peer share record: %v", err)
	}
	dag.AttachSenderSig(record, peerSig)

	verifyEnv := &pb.Envelope{
		Payload: &pb.Envelope_Handshake{
			Handshake: &pb.HandshakeMsg{
				SessionId:      []byte("session-1"),
				IdentityPubkey: []byte("peer-pub"),
				Policy:         pb.ServicePolicy_POLICY_LIGHT,
			},
		},
	}
	coSignEnv := &pb.Envelope{
		Payload: &pb.Envelope_ShareRecord{
			ShareRecord: record,
		},
	}

	s := NewSession(
		"peer-1",
		DirectionOutbound,
		[]byte("file-hash"),
		&mockTransport{
			peerID:    "peer-1",
			recvQueue: []*pb.Envelope{verifyEnv, coSignEnv},
		},
		&mockChainAppender{},
		&mockBalanceChecker{value: 1},
		&mockSigner{priv: localPriv},
	)

	ctx := context.Background()
	if err := s.RunSession(ctx); err != nil {
		t.Fatalf("RunSession returned error: %v", err)
	}

	if s.State != StateComplete {
		t.Fatalf("expected final state %q, got %q", StateComplete, s.State)
	}
}

func TestCheckpointAndRecoverSessions(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	s := NewSession(
		"peer-1",
		DirectionOutbound,
		[]byte("file-hash"),
		nil,
		nil,
		nil,
		nil,
	)
	if err := s.TransitionTo(StateHandshake); err != nil {
		t.Fatalf("failed to set state: %v", err)
	}

	if err := CheckpointTransferState(store, s); err != nil {
		t.Fatalf("CheckpointTransferState failed: %v", err)
	}

	recovered, err := RecoverSessions(store)
	if err != nil {
		t.Fatalf("RecoverSessions failed: %v", err)
	}

	if len(recovered) != 1 {
		t.Fatalf("expected 1 recovered session, got %d", len(recovered))
	}

	r := recovered[0]
	if r.ID != s.ID || r.PeerID != s.PeerID {
		t.Fatalf("recovered session identity mismatch: got ID=%q PeerID=%q, want ID=%q PeerID=%q", r.ID, r.PeerID, s.ID, s.PeerID)
	}
	if r.State != s.State {
		t.Fatalf("recovered session state mismatch: got %q, want %q", r.State, s.State)
	}
	if string(r.FileHash) != string(s.FileHash) {
		t.Fatalf("recovered session file hash mismatch")
	}
}

func TestValidateTransferRequestWindow(t *testing.T) {
	now := time.Now().Unix()
	req := &pb.TransferRequest{Timestamp: now - 10}
	if err := ValidateTransferRequestWindow(req, now, DefaultTransferRequestTTLSeconds); err != nil {
		t.Fatalf("expected valid request window, got: %v", err)
	}

	expired := &pb.TransferRequest{Timestamp: now - DefaultTransferRequestTTLSeconds - 1}
	if err := ValidateTransferRequestWindow(expired, now, DefaultTransferRequestTTLSeconds); err == nil {
		t.Fatalf("expected expired request error")
	}
}

