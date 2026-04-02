package transfer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/nyshthefantastic/burnt-peanut-network-core/crypto"
	"github.com/nyshthefantastic/burnt-peanut-network-core/dag"
	"github.com/nyshthefantastic/burnt-peanut-network-core/storage"
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
	"google.golang.org/protobuf/proto"
)

type TransferState string

const (
	StateIdle        TransferState = "IDLE"
	StateHandshake   TransferState = "HANDSHAKE"
	StateVerifying   TransferState = "VERIFYING"
	StateTransferring TransferState = "TRANSFERRING"
	StateCoSigning   TransferState = "CO_SIGNING"
	StateGossiping   TransferState = "GOSSIPING"
	StateComplete    TransferState = "COMPLETE"
	StateRejected    TransferState = "REJECTED"
	StateFailed      TransferState = "FAILED"
)

const maxVerificationPayloadBytes = 512 * 1024

type SessionDirection string

const (
	DirectionOutbound SessionDirection = "OUTBOUND"
	DirectionInbound  SessionDirection = "INBOUND"
)

type TransferSession struct {
	ID        string
	PeerID    string
	Direction SessionDirection
	FileHash  []byte

	State TransferState

	transport Transport
	chain     ChainAppender
	balance   BalanceChecker
	signer    Signer
	storage   FileStorage
	policyStore *storage.Store

	localPubKey     []byte
	pendingRequest  *pb.TransferRequest

	// Outbound (requester): after the signed TransferRequest is sent on the wire, wait for
	// ChunkBatch before co-signing. Prevents jumping to CoSigning while Recv would steal batches.
	outboundChunkRequestSent bool

	mu         sync.Mutex
	cancelFunc context.CancelFunc
	err        error
}

func NewSession(
	peerID string,
	direction SessionDirection,
	fileHash []byte,
	transport Transport,
	chain ChainAppender,
	balance BalanceChecker,
	signer Signer,
) *TransferSession {
	return &TransferSession{
		ID:        peerID,
		PeerID:    peerID,
		Direction: direction,
		FileHash:  append([]byte(nil), fileHash...),
		State:     StateIdle,
		transport: transport,
		chain:     chain,
		balance:   balance,
		signer:    signer,
	}
}

func (s *TransferSession) SetLocalPubKey(pubKey []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.localPubKey = append([]byte(nil), pubKey...)
}

func (s *TransferSession) SetFileStorage(storage FileStorage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storage = storage
}

func (s *TransferSession) SetPendingRequest(req *pb.TransferRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingRequest = req
}

func (s *TransferSession) SetPolicyStore(store *storage.Store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policyStore = store
}

// RebindPeer updates the logical peer id after a BLE link migrates (e.g. central GATT drops but
// peripheral link remains, mapped to a different numeric peer id on the same device).
func (s *TransferSession) RebindPeer(newPeerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PeerID = newPeerID
}

func (s *TransferSession) TransitionTo(next TransferState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !isValidTransition(s.State, next) {
		return fmt.Errorf("invalid state transition: %s -> %s", s.State, next)
	}

	s.State = next
	return nil
}

func isValidTransition(current TransferState, next TransferState) bool {
	switch current {
	case StateIdle:
		return next == StateHandshake
	case StateHandshake:
		return next == StateVerifying || next == StateRejected || next == StateFailed
	case StateVerifying:
		return next == StateTransferring || next == StateRejected || next == StateFailed
	case StateTransferring:
		return next == StateTransferring || next == StateCoSigning || next == StateComplete || next == StateFailed
	case StateCoSigning:
		return next == StateGossiping || next == StateFailed
	case StateGossiping:
		return next == StateComplete || next == StateFailed
	default:
		return false
	}
}

func (s *TransferSession) RunSession(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)

	s.mu.Lock()
	s.cancelFunc = cancel
	s.err = nil
	s.mu.Unlock()

	defer cancel()

	if s.pendingRequest != nil {
		if err := s.TransitionTo(StateHandshake); err != nil {
			s.setErr(err)
			return err
		}
		if err := s.TransitionTo(StateVerifying); err != nil {
			s.setErr(err)
			return err
		}
		if err := s.TransitionTo(StateTransferring); err != nil {
			s.setErr(err)
			return err
		}
	} else {
		if err := s.TransitionTo(StateHandshake); err != nil {
			s.setErr(err)
			return err
		}
	}

	for {
		select {
		case <-runCtx.Done():
			err := runCtx.Err()
			if errors.Is(err, context.Canceled) {
				s.setErr(err)
				return err
			}
			s.setErr(err)
			return err
		default:
		}

		current := s.getState()

		switch current {
		case StateHandshake:
			next, err := s.handleHandshake(runCtx)
			if err != nil {
				s.setErr(err)
				_ = s.TransitionTo(StateFailed)
				return err
			}
			if err := s.TransitionTo(next); err != nil {
				s.setErr(err)
				return err
			}
		case StateVerifying:
			next, err := s.handleVerifying(runCtx)
			if err != nil {
				s.setErr(err)
				_ = s.TransitionTo(StateFailed)
				return err
			}
			if err := s.TransitionTo(next); err != nil {
				s.setErr(err)
				return err
			}
		case StateTransferring:
			next, err := s.handleTransferring(runCtx)
			if err != nil {
				s.setErr(err)
				_ = s.TransitionTo(StateFailed)
				return err
			}
			if err := s.TransitionTo(next); err != nil {
				s.setErr(err)
				return err
			}
		case StateCoSigning:
			next, err := s.handleCoSigning(runCtx)
			if err != nil {
				s.setErr(err)
				_ = s.TransitionTo(StateFailed)
				return err
			}
			if err := s.TransitionTo(next); err != nil {
				s.setErr(err)
				return err
			}
		case StateGossiping:
			next, err := s.handleGossiping(runCtx)
			if err != nil {
				s.setErr(err)
				_ = s.TransitionTo(StateFailed)
				return err
			}
			if err := s.TransitionTo(next); err != nil {
				s.setErr(err)
				return err
			}
		case StateComplete, StateRejected, StateFailed:
			return s.getErr()
		default:
			err := fmt.Errorf("unhandled session state: %s", current)
			s.setErr(err)
			_ = s.TransitionTo(StateFailed)
			return err
		}
	}
}

func (s *TransferSession) handleHandshake(_ context.Context) (TransferState, error) {
	return StateVerifying, nil
}

func (s *TransferSession) handleVerifying(_ context.Context) (TransferState, error) {
	if s.transport == nil {
		return StateFailed, fmt.Errorf("verifying requires transport")
	}

	env, err := s.transport.Recv()
	if err != nil {
		return StateFailed, fmt.Errorf("failed to receive verification payload: %w", err)
	}
	if env == nil {
		return StateFailed, fmt.Errorf("received nil verification envelope")
	}

	handshake := env.GetHandshake()
	if handshake == nil {
		return StateFailed, fmt.Errorf("verification envelope missing handshake payload")
	}

	if size := proto.Size(handshake); size > maxVerificationPayloadBytes {
		return StateRejected, fmt.Errorf(
			"verification payload too large: %d > %d",
			size,
			maxVerificationPayloadBytes,
		)
	}

	peerPub, peerPolicy, err := ProcessHandshake(handshake)
	if err != nil {
		return StateRejected, fmt.Errorf("invalid handshake payload: %w", err)
	}

	// New-device fallback: allow drip-only path when no checkpoint/records exist.
	if handshake.GetLatestCheckpoint() == nil && len(handshake.GetRecordsSinceCheckpoint()) == 0 {
		if s.balance == nil {
			return StateRejected, fmt.Errorf("new-device verify requires balance checker")
		}
		if s.balance.EffectiveBalance(nil, peerPub, 0) > 0 {
			return StateTransferring, nil
		}
		// Fresh BLE / MVP nodes have no ledger rows yet; drip is 0. Identity still passed
		// ProcessHandshake. Rejecting here prevents any first-hop file transfer (see session logs:
		// UI shows "request OK" but no NativeHooks send / no writeChunk on peer).
		return StateTransferring, nil
	}

	recordsForPolicy := handshake.GetRecordsSinceCheckpoint()
	if s.policyStore != nil && len(recordsForPolicy) == 0 {
		from := uint64(0)
		if cp := handshake.GetLatestCheckpoint(); cp != nil {
			from = cp.GetRecordIndex()
		}
		if fetched, fetchErr := s.policyStore.GetRecordsByDevice(peerPub, from, 200); fetchErr == nil {
			recordsForPolicy = fetched
		}
	}
	approved, reason := EvaluatePolicy(s.policyStore, peerPub, peerPolicy, handshake.GetLatestCheckpoint(), recordsForPolicy)
	if approved {
		return StateTransferring, nil
	}
	return StateRejected, fmt.Errorf("policy rejected: %s", reason)
}

func (s *TransferSession) handleTransferring(_ context.Context) (TransferState, error) {
	if s.pendingRequest == nil || s.storage == nil {
		return StateCoSigning, nil
	}

	missing, err := MissingChunkIndices(s.storage, s.pendingRequest.FileHash, s.pendingRequest.ChunkIndices)
	if err != nil {
		return StateFailed, fmt.Errorf("resume chunk check failed: %w", err)
	}

	// Inbound = we received a TransferRequest; we are the sender and must have every chunk locally.
	if s.Direction == DirectionInbound {
		if len(missing) > 0 {
			return StateFailed, fmt.Errorf("sender missing %d requested chunks locally", len(missing))
		}
		if len(s.pendingRequest.ChunkIndices) > 0 {
			indices := s.pendingRequest.ChunkIndices
			if len(indices) > MaxChunksPerBatch {
				indices = append([]uint32(nil), s.pendingRequest.ChunkIndices[:MaxChunksPerBatch]...)
			}
			batch, berr := BuildBatch(s.pendingRequest.FileHash, indices, len(indices), s.storage)
			if berr != nil {
				return StateFailed, fmt.Errorf("build chunk batch: %w", berr)
			}
			if s.transport != nil {
				if err := s.transport.Send(&pb.Envelope{
					Payload: &pb.Envelope_ChunkBatch{ChunkBatch: batch},
				}); err != nil {
					return StateFailed, fmt.Errorf("send chunk batch: %w", err)
				}
			}
		}
		return StateComplete, nil
	}

	// Outbound: requester — receive chunk data from peer before co-signing.
	if len(missing) == 0 {
		return StateComplete, nil
	}

	if !s.outboundChunkRequestSent {
		s.pendingRequest.ChunkIndices = missing
		if s.signer != nil {
			signable := dag.TransferRequestSignableBytes(s.pendingRequest)
			sig, err := s.signer.Sign(signable)
			if err != nil {
				return StateFailed, fmt.Errorf("sign transfer request failed: %w", err)
			}
			s.pendingRequest.Signature = sig
		}
		if s.transport != nil {
			if err := s.transport.Send(&pb.Envelope{
				Payload: &pb.Envelope_TransferRequest{TransferRequest: s.pendingRequest},
			}); err != nil {
				return StateFailed, fmt.Errorf("send transfer request failed: %w", err)
			}
		}
		s.outboundChunkRequestSent = true
		return StateTransferring, nil
	}

	// Wait until chunks are present (cabi may persist ChunkBatch before enqueue; Recv may duplicate — idempotent writes).
	for {
		missing, err = MissingChunkIndices(s.storage, s.pendingRequest.FileHash, s.pendingRequest.ChunkIndices)
		if err != nil {
			return StateFailed, fmt.Errorf("resume chunk check failed: %w", err)
		}
		if len(missing) == 0 {
			for {
				env, ok := s.transport.TryRecv()
				if !ok {
					break
				}
				if env == nil {
					continue
				}
				if batch := env.GetChunkBatch(); batch != nil && s.storage != nil {
					for _, ch := range batch.GetChunks() {
						if err := s.storage.WriteChunk(batch.GetFileHash(), ch.GetChunkIndex(), ch.GetData()); err != nil {
							return StateFailed, fmt.Errorf("store received chunk: %w", err)
						}
					}
				} else {
					s.transport.PutBack(env)
					break
				}
			}
			return StateComplete, nil
		}
		if s.transport == nil {
			return StateFailed, fmt.Errorf("transfer requires transport while waiting for chunks")
		}
		env, err := s.transport.Recv()
		if err != nil {
			return StateFailed, fmt.Errorf("waiting for chunk delivery: %w", err)
		}
		if env == nil {
			return StateFailed, fmt.Errorf("transport returned nil envelope while waiting for chunks")
		}
		if batch := env.GetChunkBatch(); batch != nil && s.storage != nil {
			for _, ch := range batch.GetChunks() {
				if err := s.storage.WriteChunk(batch.GetFileHash(), ch.GetChunkIndex(), ch.GetData()); err != nil {
					return StateFailed, fmt.Errorf("store received chunk: %w", err)
				}
			}
		}
	}
}

func (s *TransferSession) handleCoSigning(_ context.Context) (TransferState, error) {
	if s.transport == nil {
		return StateFailed, fmt.Errorf("co-signing requires transport")
	}
	if s.signer == nil {
		return StateFailed, fmt.Errorf("co-signing requires signer")
	}
	if s.chain == nil {
		return StateFailed, fmt.Errorf("co-signing requires chain appender")
	}

	env, err := s.transport.Recv()
	if err != nil {
		return StateFailed, fmt.Errorf("failed to receive co-sign record: %w", err)
	}
	if env == nil || env.GetShareRecord() == nil {
		return StateFailed, fmt.Errorf("co-sign envelope missing share record")
	}
	record := env.GetShareRecord()

	if len(record.GetRequestHash()) == 0 {
		return StateRejected, fmt.Errorf("share record missing request hash")
	}
	if len(s.FileHash) > 0 && !bytes.Equal(record.GetFileHash(), s.FileHash) {
		return StateRejected, fmt.Errorf("share record file hash mismatch")
	}
	if s.pendingRequest != nil {
		reqBytes, err := proto.Marshal(s.pendingRequest)
		if err != nil {
			return StateFailed, fmt.Errorf("marshal pending request failed: %w", err)
		}
		reqHash := crypto.Hash(reqBytes)
		if !bytes.Equal(record.GetRequestHash(), reqHash[:]) {
			return StateRejected, fmt.Errorf("share record request hash does not match pending request")
		}
	}

	signable := dag.SignableBytes(record)
	localSig, err := s.signer.Sign(signable)
	if err != nil {
		return StateFailed, fmt.Errorf("sign share record failed: %w", err)
	}

	// Direction decides local role for signature attachment.
	if s.Direction == DirectionInbound {
		dag.AttachSenderSig(record, localSig)
	} else {
		dag.AttachReceiverSig(record, localSig)
	}

	if err := s.transport.Send(&pb.Envelope{
		Payload: &pb.Envelope_ShareRecord{ShareRecord: record},
	}); err != nil {
		return StateFailed, fmt.Errorf("send co-signed record failed: %w", err)
	}

	if len(record.GetSenderSig()) == 0 || len(record.GetReceiverSig()) == 0 {
		finalEnv, err := s.transport.Recv()
		if err != nil {
			return StateFailed, fmt.Errorf("receive final co-signed record failed: %w", err)
		}
		if finalEnv == nil || finalEnv.GetShareRecord() == nil {
			return StateFailed, fmt.Errorf("final co-signed envelope missing share record")
		}
		record = finalEnv.GetShareRecord()
	}

	if err := dag.ValidateShareRecord(record); err != nil {
		return StateFailed, fmt.Errorf("share record validation failed: %w", err)
	}

	if err := s.chain.AppendRecord(record); err != nil {
		return StateFailed, fmt.Errorf("append record failed: %w", err)
	}

	return StateGossiping, nil
}

func (s *TransferSession) handleGossiping(_ context.Context) (TransferState, error) {
	return StateComplete, nil
}

func (s *TransferSession) getState() TransferState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.State
}

func (s *TransferSession) setErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *TransferSession) getErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}
