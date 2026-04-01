package credit

import (
	"bytes"
	"time"

	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

type CheckpointMetrics struct {
	WitnessCount int
	ClusterCount int
	FreshCount   int
}

func ComputeCheckpoint(
	records []*pb.ShareRecord,
	devicePubKey []byte,
	now int64,
	params CreditParams,
	witnesses []*pb.CheckpointWitness,
) (*pb.Checkpoint, CheckpointMetrics, error) {
	var chainHead []byte
	var recordIndex uint64
	var cumulativeSent uint64
	var cumulativeReceived uint64

	for _, r := range records {
		if r == nil {
			continue
		}
		if bytes.Equal(r.GetSenderPubkey(), devicePubKey) {
			chainHead = r.GetId()
			recordIndex = r.GetSenderRecordIndex()
			cumulativeSent = r.GetSenderTotals().GetCumulativeSent()
			cumulativeReceived = r.GetSenderTotals().GetCumulativeReceived()
		} else if bytes.Equal(r.GetReceiverPubkey(), devicePubKey) {
			chainHead = r.GetId()
			recordIndex = r.GetReceiverRecordIndex()
			cumulativeSent = r.GetReceiverTotals().GetCumulativeSent()
			cumulativeReceived = r.GetReceiverTotals().GetCumulativeReceived()
		}
	}

	effective := ComputeEffectiveBalance(records, devicePubKey, now, now, params)
	metrics := checkpointMetrics(witnesses, records, devicePubKey)
	confidence := pb.ConfidenceLevel_CONFIDENCE_LOW
	if metrics.WitnessCount >= 5 && metrics.ClusterCount >= 3 && metrics.FreshCount >= 2 {
		confidence = pb.ConfidenceLevel_CONFIDENCE_HIGH
	}

	cp := &pb.Checkpoint{
		DevicePubkey: devicePubKey,
		ChainHead:    chainHead,
		RecordIndex:  recordIndex,
		Totals: &pb.CumulativeTotals{
			CumulativeSent:     cumulativeSent,
			CumulativeReceived: cumulativeReceived,
		},
		RawBalance: effective,
		Timestamp:  now,
		Witnesses:  witnesses,
		Confidence: confidence,
	}
	return cp, metrics, nil
}

func checkpointMetrics(witnesses []*pb.CheckpointWitness, recentRecords []*pb.ShareRecord, devicePubKey []byte) CheckpointMetrics {
	seenClusters := map[string]struct{}{}
	recentPeers := map[string]struct{}{}
	for _, r := range recentRecords {
		if r == nil {
			continue
		}
		if bytes.Equal(r.GetSenderPubkey(), devicePubKey) {
			recentPeers[string(r.GetReceiverPubkey())] = struct{}{}
		} else if bytes.Equal(r.GetReceiverPubkey(), devicePubKey) {
			recentPeers[string(r.GetSenderPubkey())] = struct{}{}
		}
	}

	metrics := CheckpointMetrics{}
	for _, w := range witnesses {
		if w == nil {
			continue
		}
		metrics.WitnessCount++
		if w.GetEncounterCluster() != "" {
			seenClusters[w.GetEncounterCluster()] = struct{}{}
		}
		if _, seen := recentPeers[string(w.GetWitnessPubkey())]; !seen {
			metrics.FreshCount++
		}
	}
	metrics.ClusterCount = len(seenClusters)
	return metrics
}

func ShouldCheckpoint(lastCheckpointAt int64, latestRecordTime int64, recordsSince int, interval int) bool {
	if interval <= 0 {
		interval = 100
	}
	if recordsSince >= interval {
		return true
	}
	// Fallback time-based checkpoint if none for 24 hours.
	if latestRecordTime > 0 && lastCheckpointAt > 0 {
		return time.Unix(latestRecordTime, 0).Sub(time.Unix(lastCheckpointAt, 0)) >= 24*time.Hour
	}
	return false
}
