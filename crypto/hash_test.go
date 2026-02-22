package crypto

import (
	"testing"
)

func TestHash(t *testing.T) {
	a := Hash([]byte("hello"))
    b := Hash([]byte("hello"))
    c := Hash([]byte("world"))

    if a != b {
        t.Fatalf("same input produced different hashes")
    }
    if a == c {
        t.Fatalf("different inputs produced same hash")
    }
}

func TestHashChunks(t *testing.T) {
	a := Hash([]byte("hello"))
    b := Hash([]byte("world"))

    result1 := HashChunks([][]byte{a[:], b[:]})
    result2 := HashChunks([][]byte{a[:], b[:]})
    result3 := HashChunks([][]byte{b[:], a[:]})

    if result1 != result2 {
        t.Fatalf("same order produced different hash %v == %v", result1, result2)
    }

    if result1 == result3 {
        t.Fatalf("different order produced same hash %v == %v", result1, result3)
    }
}