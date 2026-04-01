package transfer

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nyshthefantastic/burnt-peanut-network-core/storage"
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

func testPolicyStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.OpenDatabase(filepath.Join(t.TempDir(), "policy.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	return s
}

func makeRecord(sender, receiver []byte, idx uint64, prev []byte, bytesTotal uint64, ts int64) *pb.ShareRecord {
	return &pb.ShareRecord{
		Id:                []byte{byte(idx)},
		SenderPubkey:      sender,
		ReceiverPubkey:    receiver,
		PrevSender:        prev,
		SenderRecordIndex: idx,
		BytesTotal:        bytesTotal,
		Timestamp:         ts,
	}
}

func TestEvaluatePolicyLightApprove(t *testing.T) {
	store := testPolicyStore(t)
	defer store.Close()

	device := []byte("peer")
	cp := &pb.Checkpoint{
		DevicePubkey: device,
		Timestamp:    time.Now().Unix() - 3600,
		Witnesses: []*pb.CheckpointWitness{
			{WitnessPubkey: []byte("w1"), EncounterCluster: "c1"},
		},
	}
	records := []*pb.ShareRecord{
		makeRecord(device, []byte("a"), 1, nil, 2000, time.Now().Unix()-100),
		makeRecord(device, []byte("b"), 2, []byte{1}, 1500, time.Now().Unix()-50),
	}

	ok, reason := EvaluatePolicy(store, device, pb.ServicePolicy_POLICY_LIGHT, cp, records)
	if !ok {
		t.Fatalf("expected approve, got reject: %s", reason)
	}
}

func TestEvaluatePolicyStrictRejectOnLowConfidence(t *testing.T) {
	store := testPolicyStore(t)
	defer store.Close()

	device := []byte("peer")
	cp := &pb.Checkpoint{
		DevicePubkey: device,
		Timestamp:    time.Now().Unix() - 3600,
		Confidence:   pb.ConfidenceLevel_CONFIDENCE_LOW,
		Witnesses: []*pb.CheckpointWitness{
			{WitnessPubkey: []byte("w1"), EncounterCluster: "c1"},
			{WitnessPubkey: []byte("w2"), EncounterCluster: "c2"},
			{WitnessPubkey: []byte("w3"), EncounterCluster: "c3"},
			{WitnessPubkey: []byte("w4"), EncounterCluster: "c4"},
			{WitnessPubkey: []byte("w5"), EncounterCluster: "c5"},
		},
	}
	ok, _ := EvaluatePolicy(store, device, pb.ServicePolicy_POLICY_STRICT, cp, nil)
	if ok {
		t.Fatalf("expected strict policy reject on low confidence")
	}
}

func TestEvaluatePolicyRejectsOnForkEvidence(t *testing.T) {
	store := testPolicyStore(t)
	defer store.Close()

	device := []byte("peer")
	err := store.InsertForkEvidence(&pb.ForkEvidence{
		DevicePubkey:   device,
		RecordA:        &pb.ShareRecord{Id: []byte("a")},
		RecordB:        &pb.ShareRecord{Id: []byte("b")},
		ReporterPubkey: []byte("r"),
		ReporterSig:    []byte("s"),
		DetectedAt:     time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("seed fork evidence: %v", err)
	}

	ok, _ := EvaluatePolicy(store, device, pb.ServicePolicy_POLICY_NONE, nil, nil)
	if ok {
		t.Fatalf("expected reject when fork evidence exists")
	}
}
