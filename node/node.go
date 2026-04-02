package node

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nyshthefantastic/burnt-peanut-network-core/credit"
	"github.com/nyshthefantastic/burnt-peanut-network-core/dag"
	"github.com/nyshthefantastic/burnt-peanut-network-core/discovery"
	"github.com/nyshthefantastic/burnt-peanut-network-core/gossip"
	"github.com/nyshthefantastic/burnt-peanut-network-core/storage"
	"github.com/nyshthefantastic/burnt-peanut-network-core/transfer"
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

type Transport interface {
	Send(env *pb.Envelope) error
	Recv() (*pb.Envelope, error)
	TryRecv() (*pb.Envelope, bool)
	PutBack(env *pb.Envelope)
	PeerID() string
	Close() error
}

type Signer interface {
	Sign(message []byte) ([]byte, error)
}

type Node struct {
	store     *storage.Store
	identity  *storage.Identity
	transfer  *transfer.SessionManager
	gossip    *gossip.GossipSession
	discovery *discovery.FileIndex

	transport Transport
	signer    Signer

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	checkpointInterval int
	lastCheckpointAt   int64
}

func New(store *storage.Store, transport Transport, maxConcurrentTransfers int, signer ...Signer) (*Node, error) {
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

	var s Signer
	if len(signer) > 0 {
		s = signer[0]
	}

	return &Node{
		store:     store,
		identity:  identity,
		transfer:  transfer.NewSessionManager(maxConcurrentTransfers),
		gossip:    gossip.NewGossipSession(store),
		discovery: discovery.NewFileIndex(),
		transport: transport,
		signer:    s,
		ctx:       ctx,
		cancel:    cancel,
		checkpointInterval: 100,
	}, nil
}

func (n *Node) Start() error {
	if n == nil {
		return fmt.Errorf("node is nil")
	}

	n.wg.Add(1)
	go n.eventLoop()
	n.wg.Add(1)
	go n.checkpointLoop()
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
			_ = n.handleTransferRequest(payload.TransferRequest)
		case *pb.Envelope_ShareRecord:
			_ = n.handleShareRecord(payload.ShareRecord)
		default:
			// Unknown or unhandled payload type.
		}
	}
}

func (n *Node) checkpointLoop() {
	defer n.wg.Done()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			records, err := n.store.GetRecordsByDevice(n.identity.Pubkey, 0, 500)
			if err != nil {
				continue
			}
			if len(records) == 0 {
				continue
			}
			lastRecord := records[len(records)-1]
			if !credit.ShouldCheckpoint(n.lastCheckpointAt, lastRecord.GetTimestamp(), len(records), n.checkpointInterval) {
				continue
			}
			cp, _, err := credit.ComputeCheckpoint(records, n.identity.Pubkey, time.Now().Unix(), credit.DefaultParams(), nil)
			if err != nil {
				continue
			}
			if err := n.store.InsertCheckpoint(cp); err == nil {
				n.lastCheckpointAt = cp.GetTimestamp()
			}
		}
	}
}

func (n *Node) handleTransferRequest(req *pb.TransferRequest) error {
	if req == nil {
		return fmt.Errorf("transfer request is nil")
	}
	if err := dag.ValidateTransferRequest(req); err != nil {
		return err
	}
	if err := transfer.ValidateTransferRequestWindow(req, time.Now().Unix(), transfer.DefaultTransferRequestTTLSeconds); err != nil {
		return err
	}

	sessionID := fmt.Sprintf("%x:%x", req.GetRequesterPubkey(), req.GetFileHash())
	s, ok := n.transfer.Get(sessionID)
	if !ok {
		s = transfer.NewSession(
			sessionID,
			transfer.DirectionInbound,
			req.GetFileHash(),
			n.transport,
			storeChainAppender{store: n.store},
			storeBalanceChecker{store: n.store},
			n.signer,
		)
		s.ID = sessionID
		s.SetPendingRequest(req)
		s.SetPolicyStore(n.store)
		_ = n.transfer.Add(s)
		go func(sess *transfer.TransferSession) {
			_ = sess.RunSession(n.ctx)
			n.transfer.RemoveCompleted()
		}(s)
	} else {
		s.SetPendingRequest(req)
	}
	return nil
}

func (n *Node) handleShareRecord(record *pb.ShareRecord) error {
	if record == nil {
		return fmt.Errorf("share record is nil")
	}
	if err := dag.ValidateShareRecord(record); err != nil {
		return err
	}
	if err := n.store.InsertRecord(record); err != nil {
		return err
	}

	// Best-effort fork detection with latest local record.
	latest, err := n.store.GetLatestRecord(record.GetSenderPubkey())
	if err == nil && latest != nil {
		if fork := dag.DetectFork(latest, record, record.GetSenderPubkey()); fork != nil {
			_ = n.store.InsertForkEvidence(fork)
		}
	}
	return nil
}

func (n *Node) AdvertiseFile(meta *pb.FileMeta, chunkIndices []uint32) (*discovery.Advertisement, error) {
	if meta == nil {
		return nil, fmt.Errorf("file metadata is required")
	}
	if err := n.discovery.AddFile(meta.GetFileHash(), chunkIndices); err != nil {
		return nil, err
	}
	if err := n.store.InsertFileMeta(meta); err != nil {
		return nil, err
	}
	return discovery.GenerateAdvertisement(meta.GetFileHash())
}

func (n *Node) MatchIncomingAdvertisement(ad *discovery.Advertisement) ([][]byte, error) {
	if ad == nil {
		return nil, fmt.Errorf("advertisement is required")
	}
	files, err := n.store.ListFiles(256, 0)
	if err != nil {
		return nil, err
	}
	out := make([][]byte, 0)
	for _, f := range files {
		ok, err := discovery.MatchAdvertisement(ad, f.GetFileHash())
		if err != nil {
			continue
		}
		if ok {
			out = append(out, f.GetFileHash())
		}
	}
	return out, nil
}

func (n *Node) AuthorizeFileRequest(capability *pb.FileCapability, requesterPubKey []byte, now int64) error {
	return discovery.ValidateCapability(capability, requesterPubKey, now)
}

type storeChainAppender struct {
	store *storage.Store
}

func (s storeChainAppender) AppendRecord(record *pb.ShareRecord) error {
	return s.store.InsertRecord(record)
}

type storeBalanceChecker struct {
	store *storage.Store
}

func (s storeBalanceChecker) EffectiveBalance(records []*pb.ShareRecord, peerPubKey []byte, peerCreatedAt int64) int64 {
	params := credit.DefaultParams()
	now := time.Now().Unix()
	if len(records) == 0 {
		recs, err := s.store.GetRecordsByDevice(peerPubKey, 0, 1000)
		if err == nil {
			records = recs
		}
	}
	return credit.ComputeEffectiveBalance(records, peerPubKey, peerCreatedAt, now, params)
}

