package discovery

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/nyshthefantastic/burnt-peanut-network-core/crypto"
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

func CreateCapability(
	fileHash []byte,
	grantedTo []byte,
	grantedByPubKey []byte,
	grantedByPrivateKey []byte,
	expiresAt int64,
) (*pb.FileCapability, error) {
	if len(fileHash) == 0 {
		return nil, fmt.Errorf("file hash is required")
	}
	if len(grantedByPubKey) == 0 {
		return nil, fmt.Errorf("grantedBy pubkey is required")
	}
	if len(grantedByPrivateKey) == 0 {
		return nil, fmt.Errorf("grantedBy private key is required")
	}
	if expiresAt <= 0 {
		return nil, fmt.Errorf("expiresAt must be > 0")
	}

	cap := &pb.FileCapability{
		FileHash:  append([]byte(nil), fileHash...),
		GrantedTo: append([]byte(nil), grantedTo...),
		GrantedBy: append([]byte(nil), grantedByPubKey...),
		ExpiresAt: expiresAt,
	}

	signable := capabilitySignableBytes(cap)
	sig, err := crypto.Sign(grantedByPrivateKey, signable)
	if err != nil {
		return nil, fmt.Errorf("sign capability: %w", err)
	}
	cap.Signature = sig
	return cap, nil
}

func ValidateCapability(capability *pb.FileCapability, requesterPubKey []byte, now int64) error {
	if capability == nil {
		return fmt.Errorf("capability is required")
	}
	if len(capability.GetFileHash()) == 0 {
		return fmt.Errorf("capability file hash is required")
	}
	if len(capability.GetGrantedBy()) == 0 {
		return fmt.Errorf("capability grantedBy is required")
	}
	if len(capability.GetSignature()) == 0 {
		return fmt.Errorf("capability signature is required")
	}
	if capability.GetExpiresAt() <= now {
		return fmt.Errorf("capability expired")
	}

	// Targeted token: granted_to must match requester.
	// Bearer token: granted_to is empty.
	if len(capability.GetGrantedTo()) > 0 && !bytes.Equal(capability.GetGrantedTo(), requesterPubKey) {
		return fmt.Errorf("capability not granted to requester")
	}

	signable := capabilitySignableBytes(capability)
	ok, err := crypto.Verify(capability.GetGrantedBy(), signable, capability.GetSignature())
	if err != nil {
		return fmt.Errorf("verify capability: %w", err)
	}
	if !ok {
		return fmt.Errorf("invalid capability signature")
	}
	return nil
}

func capabilitySignableBytes(capability *pb.FileCapability) []byte {
	var out []byte
	out = append(out, capability.GetFileHash()...)
	out = append(out, capability.GetGrantedTo()...)
	out = append(out, capability.GetGrantedBy()...)
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(capability.GetExpiresAt()))
	out = append(out, ts[:]...)
	return out
}
