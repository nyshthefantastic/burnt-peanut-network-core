package gossip

import (
	"fmt"

	"github.com/nyshthefantastic/burnt-peanut-network-core/storage"
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

func PropagateForkEvidence(store *storage.Store, evidence []*pb.ForkEvidence) error {
	if store == nil {
		return fmt.Errorf("store is required")
	}

	for _, e := range evidence {
		if e == nil {
			continue
		}
		if err := store.InsertForkEvidence(e); err != nil {
			return fmt.Errorf("insert fork evidence: %w", err)
		}
	}
	return nil
}

func PropagateCheckpoint(store *storage.Store, checkpoint *pb.Checkpoint) error {
	if store == nil {
		return fmt.Errorf("store is required")
	}
	if checkpoint == nil {
		return nil
	}

	if err := store.InsertCheckpoint(checkpoint); err != nil {
		return fmt.Errorf("insert checkpoint: %w", err)
	}
	return nil
}
