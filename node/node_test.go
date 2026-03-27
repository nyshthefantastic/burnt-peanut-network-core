package node

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nyshthefantastic/burnt-peanut-network-core/storage"
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

type mockTransport struct {
	peerID string
	recv   []*pb.Envelope
	closed bool
}

func (m *mockTransport) Send(env *pb.Envelope) error { return nil }
func (m *mockTransport) PeerID() string              { return m.peerID }
func (m *mockTransport) Close() error {
	m.closed = true
	return nil
}

func (m *mockTransport) Recv() (*pb.Envelope, error) {
	if len(m.recv) == 0 {
		time.Sleep(2 * time.Millisecond)
		return nil, nil
	}
	env := m.recv[0]
	m.recv = m.recv[1:]
	return env, nil
}

func testStore(t *testing.T) *storage.Store {
	t.Helper()
	db := filepath.Join(t.TempDir(), "node.db")
	s, err := storage.OpenDatabase(db)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	return s
}

func TestNewValidation(t *testing.T) {
	mt := &mockTransport{peerID: "p1"}
	if _, err := New(nil, mt, 2); err == nil {
		t.Fatalf("expected error for nil store")
	}

	s := testStore(t)
	defer s.Close()
	if _, err := New(s, nil, 2); err == nil {
		t.Fatalf("expected error for nil transport")
	}
}

func TestNodeStartStopAndGossipRoute(t *testing.T) {
	s := testStore(t)
	defer s.Close()

	if err := s.InitIdentity([]byte("node-local"), nil, time.Now().Unix()); err != nil {
		t.Fatalf("init identity: %v", err)
	}

	mt := &mockTransport{
		peerID: "peer-1",
		recv: []*pb.Envelope{
			{
				Payload: &pb.Envelope_Gossip{
					Gossip: &pb.GossipPayload{
						SelfSummary: &pb.PeerInfo{
							Pubkey:      []byte("peer-remote"),
							ChainHead:   []byte("head"),
							RecordIndex: 3,
							Totals: &pb.CumulativeTotals{
								CumulativeSent:     1,
								CumulativeReceived: 2,
							},
							LastSeen:      time.Now().Unix(),
							TransportType: "ble",
						},
					},
				},
			},
		},
	}

	n, err := New(s, mt, 4)
	if err != nil {
		t.Fatalf("new node: %v", err)
	}
	if err := n.Start(); err != nil {
		t.Fatalf("start node: %v", err)
	}

	time.Sleep(30 * time.Millisecond)

	if err := n.Stop(); err != nil {
		t.Fatalf("stop node: %v", err)
	}
	if !mt.closed {
		t.Fatalf("expected transport to close on stop")
	}

	if _, err := s.GetPeer([]byte("peer-remote")); err != nil {
		t.Fatalf("expected gossip payload to upsert remote peer: %v", err)
	}
}
