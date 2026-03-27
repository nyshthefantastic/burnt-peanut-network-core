package gossip

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nyshthefantastic/burnt-peanut-network-core/storage"
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

type mockTransport struct {
	peerID string
	sent   []*pb.Envelope
	recv   []*pb.Envelope
}

func (m *mockTransport) Send(env *pb.Envelope) error {
	m.sent = append(m.sent, env)
	return nil
}

func (m *mockTransport) Recv() (*pb.Envelope, error) {
	if len(m.recv) == 0 {
		return nil, nil
	}
	env := m.recv[0]
	m.recv = m.recv[1:]
	return env, nil
}

func (m *mockTransport) PeerID() string {
	return m.peerID
}

func openTestStore(t *testing.T) *storage.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "gossip.db")
	store, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	return store
}

func TestBuildGossipPayloadIncludesPeerSummaries(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	peer := &pb.PeerInfo{
		Pubkey:      []byte("peer-1"),
		ChainHead:   []byte("head"),
		RecordIndex: 1,
		Totals: &pb.CumulativeTotals{
			CumulativeSent:     10,
			CumulativeReceived: 20,
		},
		LastSeen:        time.Now().Unix(),
		HasForkEvidence: false,
		TransportType:   "ble",
	}
	if err := store.UpsertPeer(peer); err != nil {
		t.Fatalf("seed peer: %v", err)
	}

	payload, err := BuildGossipPayload(store)
	if err != nil {
		t.Fatalf("build gossip payload: %v", err)
	}
	if payload == nil {
		t.Fatalf("payload is nil")
	}
	if len(payload.GetPeerSummaries()) == 0 {
		t.Fatalf("expected at least one peer summary")
	}
}

func TestProcessGossipPayloadPropagatesData(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	checkpoint := &pb.Checkpoint{
		DevicePubkey: []byte("peer-ckpt"),
		ChainHead:    []byte("ckpt-head"),
		RecordIndex:  7,
		Totals: &pb.CumulativeTotals{
			CumulativeSent:     100,
			CumulativeReceived: 200,
		},
		RawBalance: 50,
		Timestamp:  time.Now().Unix(),
		DeviceSig:  []byte("sig"),
	}

	fork := &pb.ForkEvidence{
		DevicePubkey:  []byte("peer-fork"),
		RecordA:       &pb.ShareRecord{Id: []byte("a")},
		RecordB:       &pb.ShareRecord{Id: []byte("b")},
		ReporterPubkey: []byte("rep"),
		ReporterSig:   []byte("rep-sig"),
		DetectedAt:    time.Now().Unix(),
	}

	payload := &pb.GossipPayload{
		SelfSummary: &pb.PeerInfo{
			Pubkey:      []byte("peer-self"),
			ChainHead:   []byte("head"),
			RecordIndex: 2,
			Totals: &pb.CumulativeTotals{
				CumulativeSent:     1,
				CumulativeReceived: 2,
			},
			LastSeen:        time.Now().Unix(),
			HasForkEvidence: false,
			TransportType:   "wifi",
		},
		PeerSummaries: []*pb.PeerInfo{
			{
				Pubkey:      []byte("peer-2"),
				ChainHead:   []byte("head2"),
				RecordIndex: 3,
				Totals: &pb.CumulativeTotals{
					CumulativeSent:     3,
					CumulativeReceived: 4,
				},
				LastSeen:        time.Now().Unix(),
				HasForkEvidence: false,
				TransportType:   "ble",
			},
		},
		ForkEvidence:     []*pb.ForkEvidence{fork},
		LatestCheckpoint: checkpoint,
	}

	if err := ProcessGossipPayload(store, payload); err != nil {
		t.Fatalf("process gossip payload: %v", err)
	}

	if _, err := store.GetPeer([]byte("peer-self")); err != nil {
		t.Fatalf("expected self summary peer upserted: %v", err)
	}
	if _, err := store.GetPeer([]byte("peer-2")); err != nil {
		t.Fatalf("expected peer summary upserted: %v", err)
	}

	hasFork, err := store.HasForkEvidence([]byte("peer-fork"))
	if err != nil {
		t.Fatalf("check fork evidence: %v", err)
	}
	if !hasFork {
		t.Fatalf("expected fork evidence to be stored")
	}

	latest, err := store.GetLatestCheckpoint([]byte("peer-ckpt"))
	if err != nil {
		t.Fatalf("load latest checkpoint: %v", err)
	}
	if latest.GetRecordIndex() != checkpoint.GetRecordIndex() {
		t.Fatalf("unexpected checkpoint record index: got %d want %d", latest.GetRecordIndex(), checkpoint.GetRecordIndex())
	}
}

func TestRunGossipExchange(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	session := NewGossipSession(store)
	remotePayload := &pb.GossipPayload{
		SelfSummary: &pb.PeerInfo{
			Pubkey:      []byte("remote-peer"),
			ChainHead:   []byte("remote-head"),
			RecordIndex: 11,
			Totals: &pb.CumulativeTotals{
				CumulativeSent:     9,
				CumulativeReceived: 10,
			},
			LastSeen:      time.Now().Unix(),
			TransportType: "ble",
		},
	}

	transport := &mockTransport{
		peerID: "remote-peer",
		recv: []*pb.Envelope{
			{Payload: &pb.Envelope_Gossip{Gossip: remotePayload}},
		},
	}

	if err := session.RunGossip("remote-peer", transport); err != nil {
		t.Fatalf("run gossip: %v", err)
	}
	if len(transport.sent) != 1 {
		t.Fatalf("expected exactly one outgoing gossip envelope, got %d", len(transport.sent))
	}
	if transport.sent[0].GetGossip() == nil {
		t.Fatalf("expected outgoing envelope to contain gossip payload")
	}

	if _, err := store.GetPeer([]byte("remote-peer")); err != nil {
		t.Fatalf("expected remote peer summary applied: %v", err)
	}
}
