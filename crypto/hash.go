package crypto

import (
	"crypto/sha256"
)

func Hash(data []byte) [32]byte {
	result := sha256.Sum256(data)
	
	return result
}