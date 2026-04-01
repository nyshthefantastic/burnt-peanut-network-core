//go:build cgo

package main

/*
every function in the core.h file must be exported to be called from C.
for that we need a go implementation for each function in the core.h file.


we need to use //export comment so that CGo makes them visible to C.

- The `//export` comment must be **directly above** the function, no blank line
- Parameters and return types must be **C types** (`C.int32_t`, `C.uintptr_t`, `*C.uint8_t`)
- We must convert between C and Go types manually

*/

/*
#define CORE_GO_EXPORTS
#include "core.h"
#include <stdlib.h>
*/
import "C"
import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"time"
	"unsafe"

	"github.com/nyshthefantastic/burnt-peanut-network-core/credit"
	"github.com/nyshthefantastic/burnt-peanut-network-core/dag"
	"github.com/nyshthefantastic/burnt-peanut-network-core/crypto"
	"github.com/nyshthefantastic/burnt-peanut-network-core/identity"
	"github.com/nyshthefantastic/burnt-peanut-network-core/storage"
	"github.com/nyshthefantastic/burnt-peanut-network-core/transfer"
	"github.com/nyshthefantastic/burnt-peanut-network-core/wire"
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"

	"google.golang.org/protobuf/proto"
)

//export ml_free
func ml_free(ptr unsafe.Pointer) {
// go has to allocate C memory for the pointer, so we need to free it when we are done with it.
// native app can call this function to free the memory.

    if ptr != nil {
        C.free(ptr)
    }
}

//export ml_node_create
func ml_node_create(dbPath *C.char, callbacks C.MLCallbacks) C.uintptr_t {
    // convert C string to Go string
	dbPathStr := C.GoString(dbPath)

	db, err := storage.OpenDatabase(dbPathStr)
	if err != nil {
		return 0
	}

	dev, err := identity.LoadIdentity(db)
	if err != nil {
		dev, err = identity.NewIdentity(db)
		if err != nil {
			db.Close()
			return 0
		}
	}

	// Create a node context holding all state
	node := &NodeContext{
		Store:          db,
		Identity:       dev,
		Callbacks:      wrapCallbacks(callbacks),
		Transfer:       transfer.NewSessionManager(4),
		SessionKeys:    make(map[uintptr][]byte),
		SharedSecrets:  make(map[uintptr][]byte),
		PeerTransports: make(map[uintptr]*cabiPeerTransport),
	}

	handle := RegisterHandle(node)
	return C.uintptr_t(handle)
}

//export ml_node_destroy
func ml_node_destroy(handle C.uintptr_t) {
	obj := GetHandle(uintptr(handle))
	if obj == nil {
		return
	}

	node, ok := obj.(*NodeContext)
	if !ok {
		return
	}

	node.Store.Close()
	ReleaseHandle(uintptr(handle))
}


//export ml_set_service_policy
func ml_set_service_policy(handle C.uintptr_t, policy C.int32_t) C.int32_t {
	node, err := getNode(handle)
	if err != nil {
		return C.int32_t(errorToCode(err))
	}

	node.Policy = int32(policy)
	return C.int32_t(ML_OK)

	// Policy value maps to protobuf ServicePolicy enum:
	//   0 = POLICY_NONE, 1 = POLICY_LIGHT, 2 = POLICY_STRICT
	// Engineer C reads this during handshake to decide verification level
}



//export ml_get_peers
func ml_get_peers(handle C.uintptr_t) C.MLResult {
	node, err := getNode(handle)
	if err != nil {
		return makeResult(nil, err)
	}

	peers, err := node.Store.GetAllPeers(100)
	if err != nil {
		return makeResult(nil, err)
	}

	// Serialize peers list
	// Each peer is marshaled individually, length-prefixed
	var out []byte
	for _, peer := range peers {
		data, err := proto.Marshal(peer)
		if err != nil {
			return makeResult(nil, err)
		}
		// length-prefix each peer
		length := make([]byte, 4)
		binary.BigEndian.PutUint32(length, uint32(len(data)))
		out = append(out, length...)
		out = append(out, data...)
	}

	return makeResult(out, nil)
}


//export ml_get_file_index
func ml_get_file_index(handle C.uintptr_t) C.MLResult {
	node, err := getNode(handle)
	if err != nil {
		return makeResult(nil, err)
	}
	files, err := node.Store.ListFiles(200, 0)
	if err != nil {
		return makeResult(nil, err)
	}

	var out []byte
	for _, file := range files {
		data, err := proto.Marshal(file)
		if err != nil {
			return makeResult(nil, err)
		}
		length := make([]byte, 4)
		binary.BigEndian.PutUint32(length, uint32(len(data)))
		out = append(out, length...)
		out = append(out, data...)
	}
	return makeResult(out, nil)
}


func makeResult(data []byte, err error) C.MLResult {
	var result C.MLResult
	result.error_code = C.int32_t(errorToCode(err))

	if err != nil || len(data) == 0 {
		result.data = nil
		result.len = 0
		return result
	}

	// Allocate C memory and copy Go bytes into it.
	// The caller must call ml_free on result data
	cData := C.CBytes(data)
	result.data = (*C.uint8_t)(cData)
	result.len = C.int32_t(len(data))
	return result
}




func getNode(handle C.uintptr_t) (*NodeContext, error) {
	obj := GetHandle(uintptr(handle))
	if obj == nil {
		return nil, codeToError(ML_ERR_INVALID_ARG)
	}
	node, ok := obj.(*NodeContext)
	if !ok {
		return nil, codeToError(ML_ERR_INTERNAL)
	}
	return node, nil
}


// functions with dependencies on other packages.

//export ml_get_balance
func ml_get_balance(handle C.uintptr_t) C.MLResult {
	node, err := getNode(handle)
	if err != nil {
		return makeResult(nil, err)
	}

	identity, err := node.Store.GetIdentity()
	if err != nil {
		return makeResult(nil, err)
	}

	records, err := node.Store.GetRecordsByDevice(identity.Pubkey, 0, 2000)
	if err != nil {
		records = nil
	}

	params := credit.DefaultParams()
	now := time.Now().Unix()
	dripAllowance := credit.ComputeDripAllowance(time.Unix(identity.CreatedAt, 0), time.Unix(now, 0), params)
	diversityWeighted := credit.DiversityWeightedCredit(records, identity.Pubkey, params.WindowSize)
	effective := credit.ComputeEffectiveBalance(records, identity.Pubkey, identity.CreatedAt, now, params)

	var cumulativeReceived int64
	for _, r := range records {
		if bytesEqual(r.GetReceiverPubkey(), identity.Pubkey) {
			cumulativeReceived += int64(r.GetBytesTotal())
		}
	}
	decayPenalty := diversityWeighted - credit.ApplyPerPeerCaps(records, identity.Pubkey, params)
	if decayPenalty < 0 {
		decayPenalty = 0
	}

	balance := &pb.Balance{
		DevicePubkey:             identity.Pubkey,
		DripAllowance:            dripAllowance,
		DiversityWeightedCredit:  diversityWeighted,
		CumulativeReceived:       cumulativeReceived,
		DecayPenalty:             decayPenalty,
		EffectiveBalance:         effective,
		ComputedAt:               now,
	}
	data, err := proto.Marshal(balance)
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(data, nil)
}


//export ml_get_chain_summary
func ml_get_chain_summary(handle C.uintptr_t) C.MLResult {
	node, err := getNode(handle)
	if err != nil {
		return makeResult(nil, err)
	}
	id, err := node.Store.GetIdentity()
	if err != nil {
		return makeResult(nil, err)
	}
	summary := &pb.PeerInfo{
		Pubkey:      id.Pubkey,
		ChainHead:   id.ChainHead,
		RecordIndex: id.ChainIndex,
		Totals: &pb.CumulativeTotals{
			CumulativeSent:     id.CumulativeSent,
			CumulativeReceived: id.CumulativeReceived,
		},
		LastSeen: time.Now().Unix(),
	}
	data, err := proto.Marshal(summary)
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(data, nil)
}


//export ml_request_file
func ml_request_file(handle C.uintptr_t, fileHash *C.uint8_t, fileHashLen C.int32_t) C.MLResult {
	node, err := getNode(handle)
	if err != nil {
		return makeResult(nil, err)
	}
	if fileHash == nil || fileHashLen <= 0 {
		return makeResult(nil, codeToError(ML_ERR_INVALID_ARG))
	}

	hash := C.GoBytes(unsafe.Pointer(fileHash), fileHashLen)

	// Verify file exists in our metadata
	meta, err := node.Store.GetFileMeta(hash)
	if err != nil {
		return makeResult(nil, err)
	}
	chunks := make([]uint32, len(meta.GetChunkHashes()))
	for i := range chunks {
		chunks[i] = uint32(i)
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return makeResult(nil, err)
	}
	req := &pb.TransferRequest{
		RequesterPubkey: node.Identity.Pubkey,
		FileHash:        hash,
		ChunkIndices:    chunks,
		Nonce:           nonce,
		Timestamp:       time.Now().Unix(),
	}
	sig, err := cabiSigner{node: node}.Sign(dag.TransferRequestSignableBytes(req))
	if err != nil {
		return makeResult(nil, err)
	}
	req.Signature = sig
	if err := node.Store.InsertRequest(req); err != nil {
		return makeResult(nil, err)
	}

	node.mu.Lock()
	peerID := node.ActivePeer
	node.mu.Unlock()
	if peerID == 0 {
		peers, pErr := node.Store.GetAllPeers(1)
		if pErr != nil || len(peers) == 0 {
			return makeResult(nil, fmt.Errorf("no active peer available"))
		}
		peerID = uint64ToPeerID(peers[0].GetLastSeen())
	}
	if err := startSession(node, peerID, req, transfer.DirectionOutbound); err != nil {
		return makeResult(nil, err)
	}
	reqBytes, err := proto.Marshal(req)
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(reqBytes, nil)
}

//export ml_on_peer_discovered
func ml_on_peer_discovered(handle C.uintptr_t, peerID C.uintptr_t) {
	node, err := getNode(handle)
	if err != nil {
		return
	}
	p := peerIDToPubkey(uintptr(peerID))
	_ = node.Store.UpsertPeer(&pb.PeerInfo{
		Pubkey:      p,
		RecordIndex: 0,
		Totals:      &pb.CumulativeTotals{},
		LastSeen:    int64(peerID),
	})
}

//export ml_on_peer_connected
func ml_on_peer_connected(handle C.uintptr_t, peerID C.uintptr_t) {
	node, err := getNode(handle)
	if err != nil {
		return
	}

	ensurePeerTransport(node, uintptr(peerID))
	node.mu.Lock()
	node.ActivePeer = uintptr(peerID)
	node.mu.Unlock()

	sessionPub, sessionPriv, err := crypto.GenerateSessionKeyPair()

	if err != nil {
		return
	}


	// store the session private key for the peer so we can use it to derive the shared secret later.
	node.SessionKeys[uintptr(peerID)] = sessionPriv


	env := &pb.Envelope{
		Payload: &pb.Envelope_Handshake{
			Handshake: &pb.HandshakeMsg{
				EphemeralPubkey: sessionPub,
				IdentityPubkey:  node.Identity.Pubkey,
				Policy:          pb.ServicePolicy(node.Policy),
			},
		},
	}

	data, err := wire.EncodeEnvelope(env)
	if err != nil {
		return
	}

	// Initial handshake advertises identity, policy, and ephemeral session key.
	node.Callbacks.Send(uintptr(peerID), data)
}

//export ml_on_peer_disconnected
func ml_on_peer_disconnected(handle C.uintptr_t, peerID C.uintptr_t) {
	node, err := getNode(handle)
	if err != nil {
		return
	}

	delete(node.SessionKeys, uintptr(peerID))
	delete(node.SharedSecrets, uintptr(peerID))
	node.mu.Lock()
	delete(node.PeerTransports, uintptr(peerID))
	if node.ActivePeer == uintptr(peerID) {
		node.ActivePeer = 0
	}
	node.mu.Unlock()
	for _, s := range node.Transfer.List() {
		if s != nil && s.PeerID == fmt.Sprintf("%d", uintptr(peerID)) {
			node.Transfer.Remove(s.ID)
		}
	}
	p := peerIDToPubkey(uintptr(peerID))
	_ = node.Store.UpsertPeer(&pb.PeerInfo{
		Pubkey:      p,
		RecordIndex: 0,
		Totals:      &pb.CumulativeTotals{},
		LastSeen:    time.Now().Unix(),
	})
}

//export ml_on_data_received
func ml_on_data_received(handle C.uintptr_t, peerID C.uintptr_t, data *C.uint8_t, dataLen C.int32_t) {
	node, err := getNode(handle)
	if err != nil {
		return
	}

	if data == nil || dataLen <= 0 {
		return
	}
	goData := C.GoBytes(unsafe.Pointer(data), dataLen)

	env, err := wire.DecodeEnvelope(goData)
	if err != nil {
		return
	}

	switch payload := env.Payload.(type) {
	case *pb.Envelope_Gossip:
		for _, peer := range payload.Gossip.GetPeerSummaries() {
			_ = node.Store.UpsertPeer(peer)
		}
		if payload.Gossip.GetSelfSummary() != nil {
			_ = node.Store.UpsertPeer(payload.Gossip.GetSelfSummary())
		}
		for _, f := range payload.Gossip.GetSeedingFiles() {
			_ = node.Store.InsertFileMeta(f)
		}
		if payload.Gossip.GetLatestCheckpoint() != nil {
			_ = node.Store.InsertCheckpoint(payload.Gossip.GetLatestCheckpoint())
		}
		for _, fe := range payload.Gossip.GetForkEvidence() {
			_ = node.Store.InsertForkEvidence(fe)
		}
		node.Callbacks.NotifyGossipReceived(uintptr(peerID))
	case *pb.Envelope_ForkEvidence:
		if payload.ForkEvidence != nil {
			_ = node.Store.InsertForkEvidence(payload.ForkEvidence)
			node.Callbacks.NotifyForkDetected(payload.ForkEvidence.GetDevicePubkey())
		}
	case *pb.Envelope_ShareRecord:
		if payload.ShareRecord != nil {
			_ = node.Store.InsertRecord(payload.ShareRecord)
		}
		ensurePeerTransport(node, uintptr(peerID)).enqueue(env)
	case *pb.Envelope_TransferRequest:
		if payload.TransferRequest != nil {
			_ = node.Store.InsertRequest(payload.TransferRequest)
			_ = startSession(node, uintptr(peerID), payload.TransferRequest, transfer.DirectionInbound)
		}
	case *pb.Envelope_ChunkBatch:
		// Chunk persistence is delegated to native chunk storage callbacks.
		batch := payload.ChunkBatch
		if batch != nil {
			for _, chunk := range batch.GetChunks() {
				_ = node.Callbacks.WriteChunk(batch.GetFileHash(), chunk.GetChunkIndex(), chunk.GetData())
			}
		}
		ensurePeerTransport(node, uintptr(peerID)).enqueue(env)
	case *pb.Envelope_Handshake:
		ensurePeerTransport(node, uintptr(peerID)).enqueue(env)
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func peerIDToPubkey(peerID uintptr) []byte {
	out := make([]byte, 8)
	binary.BigEndian.PutUint64(out, uint64(peerID))
	return out
}

func uint64ToPeerID(v int64) uintptr {
	if v < 1 {
		return 0
	}
	return uintptr(v)
}