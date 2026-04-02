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

//export ml_share_file
func ml_share_file(handle C.uintptr_t, fileData *C.uint8_t, length C.int32_t, fileName *C.char) C.int32_t {
	node, err := getNode(handle)
	if err != nil {
		fmt.Printf("[cabi][share] getNode failed handle=%d err=%v\n", uintptr(handle), err)
		return C.int32_t(errorToCode(err))
	}
	if fileData == nil || length <= 0 || fileName == nil {
		fmt.Printf("[cabi][share] invalid args handle=%d fileDataNil=%v length=%d fileNameNil=%v\n", uintptr(handle), fileData == nil, int32(length), fileName == nil)
		return C.int32_t(ML_ERR_INVALID_ARG)
	}

	data := C.GoBytes(unsafe.Pointer(fileData), length)
	name := C.GoString(fileName)
	if len(data) == 0 || name == "" {
		fmt.Printf("[cabi][share] invalid converted args dataLen=%d nameEmpty=%v\n", len(data), name == "")
		return C.int32_t(ML_ERR_INVALID_ARG)
	}
	fmt.Printf("[cabi][share] begin name=%q bytes=%d\n", name, len(data))

	const chunkSize = 64 * 1024
	fileHash := crypto.Hash(data)
	originSig, sigErr := cabiSigner{node: node}.Sign(fileHash[:])
	if sigErr != nil {
		fmt.Printf("[cabi][share] origin signature failed err=%v\n", sigErr)
		return C.int32_t(ML_ERR_CRYPTO)
	}
	chunkHashes := make([][]byte, 0, (len(data)+chunkSize-1)/chunkSize)
	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}
		chunk := data[i:end]
		chunkHash := crypto.Hash(chunk)
		chunkHashes = append(chunkHashes, chunkHash[:])
		chunkIndex := uint32(i / chunkSize)
		if code := node.Callbacks.WriteChunk(fileHash[:], chunkIndex, chunk); code != ML_OK {
			// Some native layers may report a non-OK code even though the chunk was written.
			// Verify presence before treating this as a hard failure.
			hasChunk := node.Callbacks.HasChunk(fileHash[:], chunkIndex)
			fmt.Printf("[cabi][share] WriteChunk non-OK index=%d size=%d code=%d hasChunk=%v\n", chunkIndex, len(chunk), code, hasChunk)
			if !hasChunk {
				return C.int32_t(code)
			}
		}
	}

	meta := &pb.FileMeta{
		FileHash:    fileHash[:],
		FileName:    name,
		FileSize:    uint64(len(data)),
		ChunkSize:   chunkSize,
		ChunkHashes: chunkHashes,
		OriginPubkey: node.Identity.Pubkey,
		OriginSig:    originSig,
		CreatedAt:    time.Now().Unix(),
	}
	if err := node.Store.InsertFileMeta(meta); err != nil {
		fmt.Printf("[cabi][share] InsertFileMeta failed hash=%x err=%v\n", fileHash[:], err)
		return C.int32_t(errorToCode(err))
	}
	fmt.Printf("[cabi][share] success hash=%x chunks=%d\n", fileHash[:], len(chunkHashes))

	return C.int32_t(ML_OK)
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
		fmt.Printf("[cabi][request] getNode failed handle=%d err=%v\n", uintptr(handle), err)
		return makeResult(nil, err)
	}
	if fileHash == nil || fileHashLen <= 0 {
		fmt.Printf("[cabi][request] invalid args handle=%d fileHashNil=%v fileHashLen=%d\n", uintptr(handle), fileHash == nil, int32(fileHashLen))
		return makeResult(nil, codeToError(ML_ERR_INVALID_ARG))
	}

	hash := C.GoBytes(unsafe.Pointer(fileHash), fileHashLen)
	fmt.Printf("[cabi][request] begin hash=%x len=%d\n", hash, len(hash))

	// Verify file exists in our metadata
	meta, err := node.Store.GetFileMeta(hash)
	if err != nil {
		fmt.Printf("[cabi][request] GetFileMeta failed hash=%x err=%v\n", hash, err)
		return makeResult(nil, err)
	}
	chunks := make([]uint32, len(meta.GetChunkHashes()))
	for i := range chunks {
		chunks[i] = uint32(i)
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		fmt.Printf("[cabi][request] nonce generation failed err=%v\n", err)
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
		fmt.Printf("[cabi][request] sign failed err=%v\n", err)
		return makeResult(nil, err)
	}
	req.Signature = sig
	if err := node.Store.InsertRequest(req); err != nil {
		fmt.Printf("[cabi][request] InsertRequest failed err=%v\n", err)
		return makeResult(nil, err)
	}

	node.mu.Lock()
	peerID := node.ActivePeer
	if peerID != 0 {
		if _, ok := node.PeerTransports[peerID]; !ok {
			peerID = 0
			node.ActivePeer = 0
		}
	}
	if peerID == 0 {
		if id := pickFallbackPeerIDLocked(node); id != 0 {
			node.ActivePeer = id
			peerID = id
		}
	}
	node.mu.Unlock()
	if peerID == 0 {
		peers, pErr := node.Store.GetAllPeers(1)
		if pErr != nil || len(peers) == 0 {
			fmt.Printf("[cabi][request] no active peer and no fallback peers err=%v\n", pErr)
			return makeResult(nil, fmt.Errorf("no active peer available"))
		}
		peerID = uint64ToPeerID(peers[0].GetLastSeen())
	}
	if err := startSession(node, peerID, req, transfer.DirectionOutbound); err != nil {
		fmt.Printf("[cabi][request] startSession failed peerID=%d err=%v\n", peerID, err)
		return makeResult(nil, err)
	}
	reqBytes, err := proto.Marshal(req)
	if err != nil {
		fmt.Printf("[cabi][request] marshal failed err=%v\n", err)
		return makeResult(nil, err)
	}
	fmt.Printf("[cabi][request] success peerID=%d reqBytes=%d chunks=%d\n", peerID, len(reqBytes), len(chunks))
	return makeResult(reqBytes, nil)
}

//export ml_request_file_with_chunk_count
func ml_request_file_with_chunk_count(handle C.uintptr_t, fileHash *C.uint8_t, fileHashLen C.int32_t, chunkCount C.int32_t) C.MLResult {
	node, err := getNode(handle)
	if err != nil {
		fmt.Printf("[cabi][request2] getNode failed handle=%d err=%v\n", uintptr(handle), err)
		return makeResult(nil, err)
	}
	if fileHash == nil || fileHashLen <= 0 || chunkCount <= 0 {
		fmt.Printf("[cabi][request2] invalid args handle=%d fileHashNil=%v fileHashLen=%d chunkCount=%d\n", uintptr(handle), fileHash == nil, int32(fileHashLen), int32(chunkCount))
		return makeResult(nil, codeToError(ML_ERR_INVALID_ARG))
	}

	hash := C.GoBytes(unsafe.Pointer(fileHash), fileHashLen)
	fmt.Printf("[cabi][request2] begin hash=%x len=%d chunkCount=%d\n", hash, len(hash), int32(chunkCount))

	chunks := make([]uint32, int32(chunkCount))
	for i := range chunks {
		chunks[i] = uint32(i)
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		fmt.Printf("[cabi][request2] nonce generation failed err=%v\n", err)
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
		fmt.Printf("[cabi][request2] sign failed err=%v\n", err)
		return makeResult(nil, err)
	}
	req.Signature = sig
	if err := node.Store.InsertRequest(req); err != nil {
		fmt.Printf("[cabi][request2] InsertRequest failed err=%v\n", err)
		return makeResult(nil, err)
	}

	node.mu.Lock()
	peerID := node.ActivePeer
	if peerID != 0 {
		if _, ok := node.PeerTransports[peerID]; !ok {
			peerID = 0
			node.ActivePeer = 0
		}
	}
	if peerID == 0 {
		if id := pickFallbackPeerIDLocked(node); id != 0 {
			node.ActivePeer = id
			peerID = id
		}
	}
	node.mu.Unlock()
	if peerID == 0 {
		peers, pErr := node.Store.GetAllPeers(1)
		if pErr != nil || len(peers) == 0 {
			fmt.Printf("[cabi][request2] no active peer and no fallback peers err=%v\n", pErr)
			return makeResult(nil, fmt.Errorf("no active peer available"))
		}
		peerID = uint64ToPeerID(peers[0].GetLastSeen())
	}
	if err := startSession(node, peerID, req, transfer.DirectionOutbound); err != nil {
		fmt.Printf("[cabi][request2] startSession failed peerID=%d err=%v\n", peerID, err)
		return makeResult(nil, err)
	}
	reqBytes, err := proto.Marshal(req)
	if err != nil {
		fmt.Printf("[cabi][request2] marshal failed err=%v\n", err)
		return makeResult(nil, err)
	}
	fmt.Printf("[cabi][request2] success peerID=%d reqBytes=%d chunks=%d\n", peerID, len(reqBytes), len(chunks))
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

	pid := uintptr(peerID)
	ensurePeerTransport(node, pid)

	var merge []*cabiPeerTransport
	var canT *cabiPeerTransport
	var canID uintptr
	node.mu.Lock()
	canID = maxTransportPeerID(node)
	canT = node.PeerTransports[canID]
	if canT == nil {
		node.mu.Unlock()
		return
	}
	node.ActivePeer = canID
	if len(node.PeerTransports) > 1 {
		for id, t := range node.PeerTransports {
			if id != canID && t != nil {
				merge = append(merge, t)
			}
		}
	}
	node.mu.Unlock()
	for _, t := range merge {
		if t != nil && canT != nil && t != canT {
			t.linkDelegate(canT)
		}
	}

	sessionPub, sessionPriv, err := crypto.GenerateSessionKeyPair()

	if err != nil {
		return
	}


	// store the session private key for the peer so we can use it to derive the shared secret later.
	node.SessionKeys[pid] = sessionPriv

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
	node.Callbacks.Send(pid, data)
}

//export ml_on_peer_disconnected
func ml_on_peer_disconnected(handle C.uintptr_t, peerID C.uintptr_t) {
	node, err := getNode(handle)
	if err != nil {
		return
	}
	delete(node.SessionKeys, uintptr(peerID))
	delete(node.SharedSecrets, uintptr(peerID))
	dropped := uintptr(peerID)
	node.mu.Lock()
	oldT := node.PeerTransports[dropped]
	delete(node.PeerTransports, dropped)
	if node.ActivePeer == dropped {
		node.ActivePeer = 0
	}
	survT, survID := pickSurvivorTransportLocked(node)
	var mergeOthers []*cabiPeerTransport
	if survT != nil && len(node.PeerTransports) > 1 {
		for id, t := range node.PeerTransports {
			if id != survID && t != nil {
				mergeOthers = append(mergeOthers, t)
			}
		}
	}
	if survT != nil && node.ActivePeer == 0 {
		node.ActivePeer = survID
	}
	node.mu.Unlock()

	if oldT != nil && survT != nil && oldT != survT {
		oldT.linkDelegate(survT)
		droppedStr := fmt.Sprintf("%d", dropped)
		survStr := fmt.Sprintf("%d", survID)
		for _, s := range node.Transfer.List() {
			if s != nil && s.PeerID == droppedStr {
				s.RebindPeer(survStr)
			}
		}
	} else if survT == nil {
		for _, s := range node.Transfer.List() {
			if s != nil && s.PeerID == fmt.Sprintf("%d", dropped) {
				node.Transfer.Remove(s.ID)
			}
		}
	}
	for _, ot := range mergeOthers {
		if ot != nil && survT != nil && ot != survT {
			ot.linkDelegate(survT)
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
		fmt.Printf("[cabi] ml_on_data_received decode failed peer=%d bytes=%d err=%v\n", uintptr(peerID), len(goData), err)
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
			if err := startSession(node, uintptr(peerID), payload.TransferRequest, transfer.DirectionInbound); err != nil {
				fmt.Printf("[cabi] startSession inbound failed peer=%d hash=%x err=%v\n", uintptr(peerID), payload.TransferRequest.GetFileHash(), err)
			}
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

// maxTransportPeerID returns the largest peer id key, or 0 if the map is empty.
// Dual GATT often yields a higher id for the inbound (GATT server) address; merging into
// max keeps inbound chunk delivery aligned with the canonical transport.
// Caller must hold node.mu.
func maxTransportPeerID(node *NodeContext) uintptr {
	if len(node.PeerTransports) == 0 {
		return 0
	}
	maxID := uintptr(0)
	for id := range node.PeerTransports {
		if id > maxID {
			maxID = id
		}
	}
	return maxID
}

// pickSurvivorTransportLocked selects the canonical transport after removing a peer.
// Prefers node.ActivePeer when still present; otherwise the maximum id (matches merge policy).
// Caller must hold node.mu.
func pickSurvivorTransportLocked(node *NodeContext) (*cabiPeerTransport, uintptr) {
	if len(node.PeerTransports) == 0 {
		return nil, 0
	}
	survID := maxTransportPeerID(node)
	if node.ActivePeer != 0 {
		if _, ok := node.PeerTransports[node.ActivePeer]; ok {
			survID = node.ActivePeer
		}
	}
	return node.PeerTransports[survID], survID
}

// pickFallbackPeerIDLocked picks a peer id for outbound requests when ActivePeer is unset.
// Caller must hold node.mu.
func pickFallbackPeerIDLocked(node *NodeContext) uintptr {
	return maxTransportPeerID(node)
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