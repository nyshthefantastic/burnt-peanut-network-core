package crypto

import (
	"crypto/sha256"
)

func Hash(data []byte) [32]byte {
	result := sha256.Sum256(data)
	
	return result
}

func HashChunks(chunks [][]byte) [32]byte {
	appendedChunks := make([]byte, 0)
	for _, chunk := range chunks {
		appendedChunks = append(appendedChunks, chunk...)
	}
	result := Hash(appendedChunks)
	return result
}