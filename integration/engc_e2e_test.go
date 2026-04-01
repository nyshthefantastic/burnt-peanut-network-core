package integration

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	mlcrypto "github.com/nyshthefantastic/burnt-peanut-network-core/crypto"
	"github.com/nyshthefantastic/burnt-peanut-network-core/dag"
	"github.com/nyshthefantastic/burnt-peanut-network-core/discovery"
	"github.com/nyshthefantastic/burnt-peanut-network-core/gossip"
	"github.com/nyshthefantastic/burnt-peanut-network-core/node"
	"github.com/nyshthefantastic/burnt-peanut-network-core/storage"
	"github.com/nyshthefantastic/burnt-peanut-network-core/transfer"
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
	"google.golang.org/protobuf/proto"
)

type testTransport struct {
	peerID string
	recv   []*pb.Envelope
	sent   []*pb.Envelope
	closed bool
}

func (t *testTransport) Send(env *pb.Envelope) error {
	t.sent = append(t.sent, env)
	return nil
}

func (t *testTransport) Recv() (*pb.Envelope, error) {
	if len(t.recv) == 0 {
		time.Sleep(2 * time.Millisecond)
		return nil, nil
	}
	env := t.recv[0]
	t.recv = t.recv[1:]
	return env, nil
}

func (t *testTransport) PeerID() string { return t.peerID }

func (t *testTransport) Close() error {
	t.closed = true
	return nil
}

type testSigner struct {
	priv []byte
}

func (s *testSigner) Sign(message []byte) ([]byte, error) {
	return mlcrypto.Sign(s.priv, message)
}

type testBalance struct {
	value int64
}

func (b *testBalance) EffectiveBalance(_ []*pb.ShareRecord, _ []byte, _ int64) int64 {
	return b.value
}

type testChainAppender struct {
	records []*pb.ShareRecord
}

func (c *testChainAppender) AppendRecord(record *pb.ShareRecord) error {
	c.records = append(c.records, record)
	return nil
}

type testFileStorage struct {
	chunks map[string][]byte
}

func newTestFileStorage() *testFileStorage {
	return &testFileStorage{chunks: make(map[string][]byte)}
}

func chunkKey(fileHash []byte, idx uint32) string {
	return fmt.Sprintf("%x:%d", fileHash, idx)
}

func (s *testFileStorage) ReadChunk(fileHash []byte, chunkIndex uint32) ([]byte, error) {
	v, ok := s.chunks[chunkKey(fileHash, chunkIndex)]
	if !ok {
		return nil, fmt.Errorf("missing chunk")
	}
	return append([]byte(nil), v...), nil
}

func (s *testFileStorage) WriteChunk(fileHash []byte, chunkIndex uint32, data []byte) error {
	s.chunks[chunkKey(fileHash, chunkIndex)] = append([]byte(nil), data...)
	return nil
}

func (s *testFileStorage) HasChunk(fileHash []byte, chunkIndex uint32) (bool, error) {
	_, ok := s.chunks[chunkKey(fileHash, chunkIndex)]
	return ok, nil
}

func openStore(t *testing.T, name string) *storage.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	store, err := storage.OpenDatabase(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	return store
}

func TestEngC_TransferResumeCoSign_E2E(t *testing.T) {
	localPub, localPriv, err := mlcrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate local keys: %v", err)
	}
	peerPub, peerPriv, err := mlcrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate peer keys: %v", err)
	}

	fileHash := []byte("file-hash")
	req := &pb.TransferRequest{
		RequesterPubkey: localPub,
		FileHash:        fileHash,
		ChunkIndices:    []uint32{0, 1, 2},
		Nonce:           []byte("nonce-123456789012"),
		Timestamp:       12345,
	}

	// Expected resume request after chunk-0 already exists.
	expectedReq := proto.Clone(req).(*pb.TransferRequest)
	expectedReq.ChunkIndices = []uint32{1, 2}
	expectedSig, err := mlcrypto.Sign(localPriv, dag.TransferRequestSignableBytes(expectedReq))
	if err != nil {
		t.Fatalf("sign expected resume request: %v", err)
	}
	expectedReq.Signature = expectedSig
	reqBytes, _ := proto.Marshal(expectedReq)
	reqHash := mlcrypto.Hash(reqBytes)

	record := &pb.ShareRecord{
		SenderPubkey:       peerPub,
		ReceiverPubkey:     localPub,
		SenderRecordIndex:  1,
		ReceiverRecordIndex: 1,
		RequestHash:        reqHash[:],
		FileHash:           fileHash,
		BytesTotal:         512,
		Timestamp:          999,
		Visibility:         pb.Visibility_VISIBILITY_PUBLIC,
	}
	peerSig, err := mlcrypto.Sign(peerPriv, dag.SignableBytes(record))
	if err != nil {
		t.Fatalf("sign peer record: %v", err)
	}
	dag.AttachSenderSig(record, peerSig)

	tr := &testTransport{
		peerID: "peer-1",
		recv: []*pb.Envelope{
			{
				Payload: &pb.Envelope_Handshake{
					Handshake: &pb.HandshakeMsg{
						SessionId:      []byte("s1"),
						IdentityPubkey: peerPub,
						Policy:         pb.ServicePolicy_POLICY_LIGHT,
					},
				},
			},
			{
				Payload: &pb.Envelope_ShareRecord{
					ShareRecord: record,
				},
			},
		},
	}

	fs := newTestFileStorage()
	_ = fs.WriteChunk(fileHash, 0, []byte("already-have"))
	chain := &testChainAppender{}
	signer := &testSigner{priv: localPriv}

	session := transfer.NewSession(
		"peer-1",
		transfer.DirectionOutbound,
		fileHash,
		tr,
		chain,
		&testBalance{value: 1},
		signer,
	)
	session.SetFileStorage(fs)
	session.SetPendingRequest(req)
	session.SetLocalPubKey(localPub)

	if err := session.RunSession(t.Context()); err != nil {
		t.Fatalf("run transfer session: %v", err)
	}
	if len(chain.records) != 1 {
		t.Fatalf("expected one appended record, got %d", len(chain.records))
	}

	var sentReq *pb.TransferRequest
	for _, env := range tr.sent {
		if env.GetTransferRequest() != nil {
			sentReq = env.GetTransferRequest()
			break
		}
	}
	if sentReq == nil {
		t.Fatalf("expected resumed transfer request to be sent")
	}
	if len(sentReq.GetChunkIndices()) != 2 || sentReq.GetChunkIndices()[0] != 1 || sentReq.GetChunkIndices()[1] != 2 {
		t.Fatalf("unexpected resumed chunk indices: %v", sentReq.GetChunkIndices())
	}
}

func TestEngC_GossipDiscoveryNode_E2E(t *testing.T) {
	// discovery
	idx := discovery.NewFileIndex()
	fileHash := []byte("discovery-file")
	if err := idx.AddFile(fileHash, []uint32{0, 1}); err != nil {
		t.Fatalf("discovery add file: %v", err)
	}
	ad, err := discovery.GenerateAdvertisement(fileHash)
	if err != nil {
		t.Fatalf("generate advertisement: %v", err)
	}
	match, err := discovery.MatchAdvertisement(ad, fileHash)
	if err != nil || !match {
		t.Fatalf("expected ad match, got match=%v err=%v", match, err)
	}

	ownerPub, ownerPriv, _ := mlcrypto.GenerateKeyPair()
	reqPub, _, _ := mlcrypto.GenerateKeyPair()
	capability, err := discovery.CreateCapability(fileHash, reqPub, ownerPub, ownerPriv, time.Now().Unix()+60)
	if err != nil {
		t.Fatalf("create capability: %v", err)
	}
	if err := discovery.ValidateCapability(capability, reqPub, time.Now().Unix()); err != nil {
		t.Fatalf("validate capability: %v", err)
	}

	// gossip + node routing
	storeA := openStore(t, "a.db")
	defer storeA.Close()
	storeB := openStore(t, "b.db")
	defer storeB.Close()

	if err := storeA.InitIdentity([]byte("node-a"), nil, time.Now().Unix()); err != nil {
		t.Fatalf("init identity A: %v", err)
	}
	if err := storeB.UpsertPeer(&pb.PeerInfo{
		Pubkey:      []byte("peer-from-b"),
		ChainHead:   []byte("h"),
		RecordIndex: 1,
		Totals:      &pb.CumulativeTotals{CumulativeSent: 1, CumulativeReceived: 2},
		LastSeen:    time.Now().Unix(),
	}); err != nil {
		t.Fatalf("seed peer on B: %v", err)
	}

	payloadB, err := gossip.BuildGossipPayload(storeB)
	if err != nil {
		t.Fatalf("build gossip payload B: %v", err)
	}

	tr := &testTransport{
		peerID: "peer-b",
		recv: []*pb.Envelope{
			{Payload: &pb.Envelope_Gossip{Gossip: payloadB}},
		},
	}
	session := gossip.NewGossipSession(storeA)
	if err := session.RunGossip("peer-b", tr); err != nil {
		t.Fatalf("run gossip session: %v", err)
	}
	if _, err := storeA.GetPeer([]byte("peer-from-b")); err != nil {
		t.Fatalf("expected peer replicated from gossip: %v", err)
	}

	nodeTransport := &testTransport{
		peerID: "peer-b",
		recv: []*pb.Envelope{
			{Payload: &pb.Envelope_Gossip{Gossip: &pb.GossipPayload{
				SelfSummary: &pb.PeerInfo{
					Pubkey:      []byte("peer-via-node"),
					ChainHead:   []byte("hh"),
					RecordIndex: 3,
					Totals:      &pb.CumulativeTotals{CumulativeSent: 3, CumulativeReceived: 4},
					LastSeen:    time.Now().Unix(),
				},
			}}},
		},
	}

	n, err := node.New(storeA, nodeTransport, 4)
	if err != nil {
		t.Fatalf("new node: %v", err)
	}
	if err := n.Start(); err != nil {
		t.Fatalf("start node: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := n.Stop(); err != nil {
		t.Fatalf("stop node: %v", err)
	}

	if err := n.AuthorizeFileRequest(capability, reqPub, time.Now().Unix()); err != nil {
		t.Fatalf("node capability authorization failed: %v", err)
	}

	peer, err := storeA.GetPeer([]byte("peer-via-node"))
	if err != nil {
		t.Fatalf("expected node to process incoming gossip: %v", err)
	}
	if !bytes.Equal(peer.GetPubkey(), []byte("peer-via-node")) {
		t.Fatalf("unexpected peer from node routing")
	}
}
