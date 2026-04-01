package credit

import (
	"testing"
	"time"

	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

func TestComputeCheckpointHighConfidence(t *testing.T) {
	device := []byte("dev")
	records := []*pb.ShareRecord{
		{
			Id:               []byte("r1"),
			SenderPubkey:     device,
			ReceiverPubkey:   []byte("p1"),
			SenderRecordIndex: 1,
			BytesTotal:       100,
			Timestamp:        time.Now().Unix() - 100,
			SenderTotals:     &pb.CumulativeTotals{CumulativeSent: 100, CumulativeReceived: 0},
		},
	}
	witnesses := []*pb.CheckpointWitness{
		{WitnessPubkey: []byte("w1"), EncounterCluster: "c1"},
		{WitnessPubkey: []byte("w2"), EncounterCluster: "c2"},
		{WitnessPubkey: []byte("w3"), EncounterCluster: "c3"},
		{WitnessPubkey: []byte("w4"), EncounterCluster: "c4"},
		{WitnessPubkey: []byte("w5"), EncounterCluster: "c5"},
	}

	cp, metrics, err := ComputeCheckpoint(records, device, time.Now().Unix(), DefaultParams(), witnesses)
	if err != nil {
		t.Fatalf("compute checkpoint: %v", err)
	}
	if cp.GetConfidence() != pb.ConfidenceLevel_CONFIDENCE_HIGH {
		t.Fatalf("expected high confidence, got %v", cp.GetConfidence())
	}
	if metrics.WitnessCount != 5 {
		t.Fatalf("unexpected witness count: %d", metrics.WitnessCount)
	}
}
