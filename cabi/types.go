//go:build cgo

package main

import (
	"sync"

	"github.com/nyshthefantastic/burnt-peanut-network-core/identity"
	"github.com/nyshthefantastic/burnt-peanut-network-core/storage"
	"github.com/nyshthefantastic/burnt-peanut-network-core/transfer"
)

type handleRegistry struct {
	mu      sync.Mutex
	handles map[uintptr]interface{}
	next    uintptr
}

// this has all the state for a node instance.
type NodeContext struct {
	Store      *storage.Store
	Identity   *identity.DeviceIdentity
	Callbacks  *NativeCallbacks
	Policy     int32
	Transfer   *transfer.SessionManager
	ActivePeer uintptr

	// peerID -> ECDH private key
	SessionKeys map[uintptr][]byte
	// peerID -> derived shared secret
	SharedSecrets map[uintptr][]byte
	// peerID -> transport adapter bound to callback send/recv.
	PeerTransports map[uintptr]*cabiPeerTransport
	mu             sync.Mutex
}