package discovery

import (
	"crypto/rand"
	"fmt"

	"github.com/nyshthefantastic/burnt-peanut-network-core/crypto"
)

const (
	SaltSizeBytes   = 8
	AdvertPrefixLen = 4
)

type Advertisement struct {
	Salt          []byte
	HashedPrefix  []byte
}

func ComputeSaltedPrefix(fileHash []byte, salt []byte) ([]byte, error) {
	if len(fileHash) == 0 {
		return nil, fmt.Errorf("file hash is required")
	}
	if len(salt) != SaltSizeBytes {
		return nil, fmt.Errorf("salt must be %d bytes", SaltSizeBytes)
	}

	material := make([]byte, 0, len(fileHash)+len(salt))
	material = append(material, fileHash...)
	material = append(material, salt...)
	full := crypto.Hash(material)

	out := make([]byte, AdvertPrefixLen)
	copy(out, full[:AdvertPrefixLen])
	return out, nil
}

func GenerateAdvertisement(fileHash []byte) (*Advertisement, error) {
	if len(fileHash) == 0 {
		return nil, fmt.Errorf("file hash is required")
	}

	salt := make([]byte, SaltSizeBytes)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	prefix, err := ComputeSaltedPrefix(fileHash, salt)
	if err != nil {
		return nil, err
	}

	return &Advertisement{
		Salt:         salt,
		HashedPrefix: prefix,
	}, nil
}

func MatchAdvertisement(ad *Advertisement, fileHash []byte) (bool, error) {
	if ad == nil {
		return false, fmt.Errorf("advertisement is required")
	}
	if len(ad.Salt) != SaltSizeBytes {
		return false, fmt.Errorf("advertisement salt must be %d bytes", SaltSizeBytes)
	}
	if len(ad.HashedPrefix) != AdvertPrefixLen {
		return false, fmt.Errorf("advertisement prefix must be %d bytes", AdvertPrefixLen)
	}

	prefix, err := ComputeSaltedPrefix(fileHash, ad.Salt)
	if err != nil {
		return false, err
	}

	for i := 0; i < AdvertPrefixLen; i++ {
		if ad.HashedPrefix[i] != prefix[i] {
			return false, nil
		}
	}
	return true, nil
}
