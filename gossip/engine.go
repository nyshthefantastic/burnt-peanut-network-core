package gossip

import (
	"fmt"

	"github.com/nyshthefantastic/burnt-peanut-network-core/storage"
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

type Transport interface {
	Send(env *pb.Envelope) error
	Recv() (*pb.Envelope, error)
	PeerID() string
}

type GossipSession struct {
	store *storage.Store
}

func NewGossipSession(store *storage.Store) *GossipSession {
	return &GossipSession{store: store}
}

func (s *GossipSession) RunGossip(peerID string, transport Transport) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("gossip session store is required")
	}
	if peerID == "" {
		return fmt.Errorf("peer id is required")
	}
	if transport == nil {
		return fmt.Errorf("transport is required")
	}

	payload, err := BuildGossipPayload(s.store)
	if err != nil {
		return err
	}

	if err := transport.Send(&pb.Envelope{
		Payload: &pb.Envelope_Gossip{Gossip: payload},
	}); err != nil {
		return fmt.Errorf("send gossip payload: %w", err)
	}

	resp, err := transport.Recv()
	if err != nil {
		return fmt.Errorf("receive gossip payload: %w", err)
	}
	if resp == nil || resp.GetGossip() == nil {
		return fmt.Errorf("missing gossip response payload")
	}

	if err := ProcessGossipPayload(s.store, resp.GetGossip()); err != nil {
		return fmt.Errorf("process gossip payload: %w", err)
	}

	return nil
}
