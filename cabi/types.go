//go:build cgo

package main

import (
	"sync"

	"github.com/nyshthefantastic/burnt-peanut-network-core/identity"
	"github.com/nyshthefantastic/burnt-peanut-network-core/storage"
)

type handleRegistry struct {
	mu      sync.Mutex
	handles map[uintptr]interface{}
	next    uintptr
}

// this has all the state for a node instance.
type NodeContext struct {
	Store         *storage.Store
	Identity      *identity.DeviceIdentity
	Callbacks     *NativeCallbacks
	Policy        int32
	  // peerID -> ECDH private key
	SessionKeys   map[uintptr][]byte
	  // peerID -> derived shared secret
	SharedSecrets map[uintptr][]byte
}