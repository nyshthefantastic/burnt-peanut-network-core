package gossip

import pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"

// ApplyByteBudget trims payload sections in priority order:
// fork evidence (highest), peer summaries, then file metadata.
func ApplyByteBudget(payload *pb.GossipPayload, maxItems int) *pb.GossipPayload {
	if payload == nil || maxItems <= 0 {
		return payload
	}

	trimmed := *payload
	remaining := maxItems

	if len(trimmed.ForkEvidence) > remaining {
		trimmed.ForkEvidence = trimmed.ForkEvidence[:remaining]
		trimmed.PeerSummaries = nil
		trimmed.SeedingFiles = nil
		return &trimmed
	}
	remaining -= len(trimmed.ForkEvidence)

	if len(trimmed.PeerSummaries) > remaining {
		trimmed.PeerSummaries = trimmed.PeerSummaries[:remaining]
		trimmed.SeedingFiles = nil
		return &trimmed
	}
	remaining -= len(trimmed.PeerSummaries)

	if len(trimmed.SeedingFiles) > remaining {
		trimmed.SeedingFiles = trimmed.SeedingFiles[:remaining]
	}
	return &trimmed
}
