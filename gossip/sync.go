package gossip

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/nyshthefantastic/burnt-peanut-network-core/storage"
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

const defaultPeerSummaryLimit = 256
const defaultGossipMaxItems = 256

func BuildGossipPayload(store *storage.Store) (*pb.GossipPayload, error) {
	if store == nil {
		return nil, fmt.Errorf("store is required")
	}

	payload := &pb.GossipPayload{}

	selfSummary, err := buildSelfSummary(store)
	if err != nil {
		return nil, err
	}
	payload.SelfSummary = selfSummary

	peers, err := store.GetAllPeers(defaultPeerSummaryLimit)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("load peer summaries: %w", err)
	}
	payload.PeerSummaries = peers

	if selfSummary != nil {
		latest, err := store.GetLatestCheckpoint(selfSummary.GetPubkey())
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("load latest checkpoint: %w", err)
		}
		payload.LatestCheckpoint = latest

		forks, err := store.GetForkEvidence(selfSummary.GetPubkey())
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("load fork evidence: %w", err)
		}
		payload.ForkEvidence = forks
	}

	files, err := store.ListFiles(defaultPeerSummaryLimit, 0)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("load seeding files: %w", err)
	}
	payload.SeedingFiles = files

	return ApplyByteBudget(payload, defaultGossipMaxItems), nil
}

func ProcessGossipPayload(store *storage.Store, payload *pb.GossipPayload) error {
	if store == nil {
		return fmt.Errorf("store is required")
	}
	if payload == nil {
		return fmt.Errorf("gossip payload is required")
	}

	if payload.GetSelfSummary() != nil {
		if err := store.UpsertPeer(payload.GetSelfSummary()); err != nil {
			return fmt.Errorf("upsert self summary: %w", err)
		}
	}

	for _, peer := range payload.GetPeerSummaries() {
		if peer == nil {
			continue
		}
		if err := store.UpsertPeer(peer); err != nil {
			return fmt.Errorf("upsert peer summary: %w", err)
		}
	}

	if err := PropagateForkEvidence(store, payload.GetForkEvidence()); err != nil {
		return err
	}
	if err := PropagateCheckpoint(store, payload.GetLatestCheckpoint()); err != nil {
		return err
	}

	for _, meta := range payload.GetSeedingFiles() {
		if meta == nil {
			continue
		}
		if err := store.InsertFileMeta(meta); err != nil {
			return fmt.Errorf("insert seeding file metadata: %w", err)
		}
	}

	return nil
}

func buildSelfSummary(store *storage.Store) (*pb.PeerInfo, error) {
	id, err := store.GetIdentity()
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load local identity: %w", err)
	}

	hasFork, err := store.HasForkEvidence(id.Pubkey)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("check local fork evidence: %w", err)
	}

	return &pb.PeerInfo{
		Pubkey:    id.Pubkey,
		ChainHead: id.ChainHead,
		RecordIndex: id.ChainIndex,
		Totals: &pb.CumulativeTotals{
			CumulativeSent:     id.CumulativeSent,
			CumulativeReceived: id.CumulativeReceived,
		},
		LastSeen:        time.Now().Unix(),
		HasForkEvidence: hasFork,
		TransportType:   "unknown",
	}, nil
}
