package cabi

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
	"encoding/binary"
	"time"
	"unsafe"

	"github.com/nyshthefantastic/burnt-peanut-network-core/credit"
	"github.com/nyshthefantastic/burnt-peanut-network-core/crypto"
	"github.com/nyshthefantastic/burnt-peanut-network-core/identity"
	"github.com/nyshthefantastic/burnt-peanut-network-core/storage"
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
		Store:     db,
		Identity:  dev,
		Callbacks: wrapCallbacks(callbacks),
		SessionKeys: make(map[uintptr][]byte),
		SharedSecrets: make(map[uintptr][]byte),
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

	// FLOW:
	//   1. Call node.Store.GetAllPeers(limit)
	//   2. Length-prefix each proto.Marshal'd peer
	//   3. Return via makeResult
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

	_ = node

	// NEEDS FROM ENGINEER B:
	//   dag/chain.go → GetChainHead(db, pubkey) returns (hash, index, error)
	//
	// FLOW:
	//   1. Get own pubkey from node.Identity
	//   2. Call B's GetChainHead(node.Store, pubkey) for current chain state
	//   3. Combine with identity cumulative totals
	//   4. Serialize and return via makeResult

	return makeResult(nil, nil)
}


//export ml_request_file
func ml_request_file(handle C.uintptr_t, fileHash *C.uint8_t, fileHashLen C.int32_t) C.MLResult {
	node, err := getNode(handle)
	if err != nil {
		return makeResult(nil, err)
	}

	hash := C.GoBytes(unsafe.Pointer(fileHash), fileHashLen)

	// Verify file exists in our metadata
	_, err = node.Store.GetFileMeta(hash)
	if err != nil {
		return makeResult(nil, err)
	}

	// NEEDS FROM ENGINEER C:
	//   transfer/engine.go → SessionManager.NewSession(peerID, direction)
	//   transfer/engine.go → TransferSession.RunSession(ctx)
	//
	// NEEDS FROM ENGINEER B:
	//   dag/record.go → NewTransferRequest(requester, fileHash, chunks, nonce)
	//
	// FLOW:
	//   1. Verify file exists: node.Store.GetFileMeta(hash)
	//   2. Build TransferRequest using B's NewTransferRequest
	//   3. Store request: node.Store.InsertRequest(req)
	//   4. Find a peer who has this file (from gossip/peer data)
	//   5. Create transfer session via C's SessionManager
	//   6. Session handles: handshake → policy eval → batch transfer → co-signing

	return makeResult(nil, nil)
}

//export ml_on_peer_discovered
func ml_on_peer_discovered(handle C.uintptr_t, peerID C.uintptr_t) {
	node, err := getNode(handle)
	if err != nil {
		return
	}

	_ = node
	_ = peerID

	// NEEDS FROM ENGINEER C:
	//   discovery/salted.go → MatchAdvertisement(targetFileHash, advertisedPrefixes, salt)
	//   discovery/index.go → FileIndex.GetAvailableFiles()
	//
	// FLOW:
	//   1. Native layer detected a new BLE device
	//   2. Check if their BLE advertisement matches any files we want
	//   3. If match found, initiate connection via Callbacks.StartScanning
	//   4. Store/update peer info: node.Store.UpsertPeer(peerInfo)
}

//export ml_on_peer_connected
func ml_on_peer_connected(handle C.uintptr_t, peerID C.uintptr_t) {
	node, err := getNode(handle)
	if err != nil {
		return
	}

	_ = node
	_ = peerID

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

	node.Callbacks.Send(uintptr(peerID), data)

	// NEEDS FROM ENGINEER C:
	//   transfer/engine.go → SessionManager.NewSession(peerID, direction)
	//   transfer/handshake.go → BuildHandshake(identity, policy, sessionID)
	//
	// NEEDS FROM ENGINEER A (you):
	//   crypto/ecdh.go → GenerateSessionKeypair() for encrypted channel
	//   wire/codec.go → WriteEnvelope() to send handshake message
	//
	// FLOW:
	//   1. Bluetooth/WiFi connection established
	//   2. Generate ECDH session keypair for encrypted channel
	//   3. Create transfer session via C's SessionManager
	//   4. Build HandshakeMsg with identity, policy, checkpoint
	//   5. Send via Callbacks.Send → native transport
	//   6. Session state machine takes over from here
}

//export ml_on_peer_disconnected
func ml_on_peer_disconnected(handle C.uintptr_t, peerID C.uintptr_t) {
	node, err := getNode(handle)
	if err != nil {
		return
	}

	_ = node
	_ = peerID

	// NEEDS FROM ENGINEER C:
	//   transfer/engine.go → SessionManager.RemoveSession(peerID)
	//   transfer/engine.go → CheckpointTransferState(db, session) for resume
	//
	// FLOW:
	//   1. Connection dropped (peer walked away, Bluetooth out of range)
	//   2. Save in-progress transfer state for later resume
	//   3. Clean up the transfer session
	//   4. Update peer last_seen: node.Store.UpsertPeer(updatedPeer)
}

//export ml_on_data_received
func ml_on_data_received(handle C.uintptr_t, peerID C.uintptr_t, data *C.uint8_t, dataLen C.int32_t) {
	node, err := getNode(handle)
	if err != nil {
		return
	}

	_ = node
	_ = peerID
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
	case *pb.Envelope_TransferRequest:
		if payload.TransferRequest != nil {
			_ = node.Store.InsertRequest(payload.TransferRequest)
		}
	case *pb.Envelope_ChunkBatch:
		// Chunk persistence is delegated to native chunk storage callbacks.
		batch := payload.ChunkBatch
		if batch != nil {
			for _, chunk := range batch.GetChunks() {
				_ = node.Callbacks.WriteChunk(batch.GetFileHash(), chunk.GetChunkIndex(), chunk.GetData())
			}
		}
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