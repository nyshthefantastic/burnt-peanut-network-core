package gossip

import (
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
	"google.golang.org/protobuf/proto"
)

// ApplyByteBudget trims payload sections in priority order:
// fork evidence (highest), peer summaries, then file metadata.
// Returns proto.Clone(payload); protobuf messages must not be copied by value.
func ApplyByteBudget(payload *pb.GossipPayload, maxItems int) *pb.GossipPayload {
	if payload == nil || maxItems <= 0 {
		return payload
	}

	trimmed := proto.Clone(payload).(*pb.GossipPayload)
	remaining := maxItems

	if len(trimmed.ForkEvidence) > remaining {
		trimmed.ForkEvidence = trimmed.ForkEvidence[:remaining]
		trimmed.PeerSummaries = nil
		trimmed.SeedingFiles = nil
		return trimmed
	}
	remaining -= len(trimmed.ForkEvidence)

	if len(trimmed.PeerSummaries) > remaining {
		trimmed.PeerSummaries = trimmed.PeerSummaries[:remaining]
		trimmed.SeedingFiles = nil
		return trimmed
	}
	remaining -= len(trimmed.PeerSummaries)

	if len(trimmed.SeedingFiles) > remaining {
		trimmed.SeedingFiles = trimmed.SeedingFiles[:remaining]
	}
	return trimmed
}
