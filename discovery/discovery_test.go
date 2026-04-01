package discovery

import (
	"testing"
	"time"

	"github.com/nyshthefantastic/burnt-peanut-network-core/crypto"
)

func TestFileIndexBasicFlow(t *testing.T) {
	idx := NewFileIndex()
	fileHash := []byte("file-1")

	if err := idx.AddFile(fileHash, []uint32{0, 2, 5}); err != nil {
		t.Fatalf("add file: %v", err)
	}
	if !idx.HasFile(fileHash) {
		t.Fatalf("expected file to exist")
	}
	if !idx.HasChunk(fileHash, 2) {
		t.Fatalf("expected chunk 2 to exist")
	}

	if err := idx.RemoveFile(fileHash); err != nil {
		t.Fatalf("remove file: %v", err)
	}
	if idx.HasFile(fileHash) {
		t.Fatalf("expected file to be removed")
	}
}

func TestAdvertisementMatch(t *testing.T) {
	fileHash := []byte("file-hash")
	ad, err := GenerateAdvertisement(fileHash)
	if err != nil {
		t.Fatalf("generate advertisement: %v", err)
	}
	ok, err := MatchAdvertisement(ad, fileHash)
	if err != nil {
		t.Fatalf("match advertisement: %v", err)
	}
	if !ok {
		t.Fatalf("expected advertisement to match")
	}
}

func TestCapabilityCreateAndValidate(t *testing.T) {
	grantedByPub, grantedByPriv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate owner keys: %v", err)
	}
	requesterPub, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate requester keys: %v", err)
	}

	now := time.Now().Unix()
	cap, err := CreateCapability(
		[]byte("file-hash"),
		requesterPub,
		grantedByPub,
		grantedByPriv,
		now+60,
	)
	if err != nil {
		t.Fatalf("create capability: %v", err)
	}

	if err := ValidateCapability(cap, requesterPub, now); err != nil {
		t.Fatalf("validate capability: %v", err)
	}

	if err := ValidateCapability(cap, []byte("other"), now); err == nil {
		t.Fatalf("expected targeted capability to reject other requester")
	}
	if err := ValidateCapability(cap, requesterPub, now+120); err == nil {
		t.Fatalf("expected expired capability rejection")
	}
}
