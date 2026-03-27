package node

import (
	"context"
	"fmt"
	"sync"

	"github.com/nyshthefantastic/burnt-peanut-network-core/discovery"
	"github.com/nyshthefantastic/burnt-peanut-network-core/gossip"
	"github.com/nyshthefantastic/burnt-peanut-network-core/storage"
	"github.com/nyshthefantastic/burnt-peanut-network-core/transfer"
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

type Transport interface {
	Send(env *pb.Envelope) error
	Recv() (*pb.Envelope, error)
	PeerID() string
	Close() error
}

type Node struct {
	store     *storage.Store
	identity  *storage.Identity
	transfer  *transfer.SessionManager
	gossip    *gossip.GossipSession
	discovery *discovery.FileIndex

	transport Transport

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func New(store *storage.Store, transport Transport, maxConcurrentTransfers int) (*Node, error) {
	if store == nil {
		return nil, fmt.Errorf("store is required")
	}
	if transport == nil {
		return nil, fmt.Errorf("transport is required")
	}

	identity, err := store.GetIdentity()
	if err != nil {
		return nil, fmt.Errorf("load identity: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Node{
		store:     store,
		identity:  identity,
		transfer:  transfer.NewSessionManager(maxConcurrentTransfers),
		gossip:    gossip.NewGossipSession(store),
		discovery: discovery.NewFileIndex(),
		transport: transport,
		ctx:       ctx,
		cancel:    cancel,
	}, nil
}

func (n *Node) Start() error {
	if n == nil {
		return fmt.Errorf("node is nil")
	}

	n.wg.Add(1)
	go n.eventLoop()
	return nil
}

func (n *Node) Stop() error {
	if n == nil {
		return fmt.Errorf("node is nil")
	}
	n.cancel()
	n.wg.Wait()
	if n.transport != nil {
		return n.transport.Close()
	}
	return nil
}

func (n *Node) eventLoop() {
	defer n.wg.Done()

	for {
		select {
		case <-n.ctx.Done():
			return
		default:
		}

		env, err := n.transport.Recv()
		if err != nil {
			// In a full implementation we would log and possibly apply backoff.
			continue
		}
		if env == nil {
			continue
		}

		switch payload := env.Payload.(type) {
		case *pb.Envelope_Gossip:
			_ = gossip.ProcessGossipPayload(n.store, payload.Gossip)
		case *pb.Envelope_TransferRequest:
			// Transfer orchestration will be fleshed out in later phases.
			_ = payload.TransferRequest
		case *pb.Envelope_ShareRecord:
			_ = payload.ShareRecord
		default:
			// Unknown or unhandled payload type.
		}
	}
}
