package transfer

import (
	"fmt"

	"github.com/nyshthefantastic/burnt-peanut-network-core/storage"
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
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
		_ = checkpoint
		_ = recentRecords
		// TODO(C2.2): Implement LIGHT verification once checkpoint witness fields and full
		// checkpoint validation flow are finalized (depends on B4 checkpoint implementation).
		return false, "policy light not implemented yet"

	case pb.ServicePolicy_POLICY_STRICT:
		_ = checkpoint
		_ = recentRecords
		// TODO(C2.2): Implement STRICT verification with HIGH_CONFIDENCE checkpoint
		// thresholds (K/D/F) and full balance computation once B4 is available.
		return false, "policy strict not implemented yet"

	default:
		return false, fmt.Sprintf("unknown service policy: %v", policy)
	}
}
