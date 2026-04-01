package transfer

import (
	"bytes"
	"fmt"
	"time"

	"github.com/nyshthefantastic/burnt-peanut-network-core/credit"
	"github.com/nyshthefantastic/burnt-peanut-network-core/dag"
	"github.com/nyshthefantastic/burnt-peanut-network-core/storage"
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

const (
	MinStrictWitnesses = 5
	MinStrictClusters  = 3
	MinStrictFresh     = 2
)

func EvaluatePolicy(
	store *storage.Store,
	peerPubkey []byte,
	policy pb.ServicePolicy,
	checkpoint *pb.Checkpoint,
	recentRecords []*pb.ShareRecord,
) (approved bool, reason string) {
	if len(peerPubkey) == 0 {
		return false, "peer public key is required"
	}
	if store == nil {
		return false, "store is required"
	}

	hasForkEvidence, err := store.HasForkEvidence(peerPubkey)
	if err != nil {
		return false, fmt.Sprintf("fork evidence check failed: %v", err)
	}
	if hasForkEvidence {
		return false, "peer has fork evidence"
	}

	switch policy {
	case pb.ServicePolicy_POLICY_NONE:
		return true, "approved: policy none"

	case pb.ServicePolicy_POLICY_LIGHT:
		if checkpoint == nil {
			return false, "light policy requires checkpoint"
		}
		if len(checkpoint.GetWitnesses()) < 1 {
			return false, "light policy requires at least one witness"
		}
		if err := dag.VerifyChainSegment(recentRecords, peerPubkey); err != nil {
			return false, fmt.Sprintf("light chain verification failed: %v", err)
		}
		balance := credit.ComputeEffectiveBalance(
			recentRecords,
			peerPubkey,
			checkpoint.GetTimestamp(),
			time.Now().Unix(),
			credit.DefaultParams(),
		)
		if balance <= 0 {
			return false, "light policy requires positive balance"
		}
		return true, "approved: policy light"

	case pb.ServicePolicy_POLICY_STRICT:
		if checkpoint == nil {
			return false, "strict policy requires checkpoint"
		}
		if checkpoint.GetConfidence() != pb.ConfidenceLevel_CONFIDENCE_HIGH {
			return false, "strict policy requires high confidence checkpoint"
		}
		if len(checkpoint.GetWitnesses()) < MinStrictWitnesses {
			return false, "strict policy requires at least 5 witnesses"
		}
		if distinctClusters(checkpoint.GetWitnesses()) < MinStrictClusters {
			return false, "strict policy requires at least 3 witness clusters"
		}
		if freshWitnessCount(checkpoint.GetWitnesses(), recentRecords, peerPubkey) < MinStrictFresh {
			return false, "strict policy requires at least 2 fresh witnesses"
		}
		if err := dag.VerifyChainSegment(recentRecords, peerPubkey); err != nil {
			return false, fmt.Sprintf("strict chain verification failed: %v", err)
		}

		params := credit.DefaultParams()
		diversity := credit.DiversityWeightedCredit(recentRecords, peerPubkey, params.WindowSize)
		capped := credit.ApplyPerPeerCaps(recentRecords, peerPubkey, params)
		effective := credit.ComputeEffectiveBalance(
			recentRecords,
			peerPubkey,
			checkpoint.GetTimestamp(),
			time.Now().Unix(),
			params,
		)
		if effective <= 0 {
			return false, "strict policy requires positive effective balance"
		}
		if diversity <= 0 {
			return false, "strict policy requires non-zero diversity credit"
		}
		if capped <= 0 {
			return false, "strict policy requires non-zero capped credit"
		}
		return true, "approved: policy strict"

	default:
		return false, fmt.Sprintf("unknown service policy: %v", policy)
	}
}

func distinctClusters(witnesses []*pb.CheckpointWitness) int {
	clusters := map[string]struct{}{}
	for _, w := range witnesses {
		if w == nil {
			continue
		}
		if w.GetEncounterCluster() == "" {
			continue
		}
		clusters[w.GetEncounterCluster()] = struct{}{}
	}
	return len(clusters)
}

func freshWitnessCount(witnesses []*pb.CheckpointWitness, recentRecords []*pb.ShareRecord, devicePubkey []byte) int {
	recentPeers := map[string]struct{}{}
	for _, r := range recentRecords {
		if r == nil {
			continue
		}
		if bytes.Equal(r.GetSenderPubkey(), devicePubkey) {
			recentPeers[string(r.GetReceiverPubkey())] = struct{}{}
		} else if bytes.Equal(r.GetReceiverPubkey(), devicePubkey) {
			recentPeers[string(r.GetSenderPubkey())] = struct{}{}
		}
	}

	fresh := 0
	for _, w := range witnesses {
		if w == nil || len(w.GetWitnessPubkey()) == 0 {
			continue
		}
		if _, seen := recentPeers[string(w.GetWitnessPubkey())]; !seen {
			fresh++
		}
	}
	return fresh
}
