package transfer

import (
	"bytes"
	"fmt"
	"slices"
	"testing"

	"github.com/nyshthefantastic/burnt-peanut-network-core/crypto"
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

type memoryFileStorage struct {
	chunks map[string][]byte
}

func newMemoryFileStorage() *memoryFileStorage {
	return &memoryFileStorage{chunks: make(map[string][]byte)}
}

func key(fileHash []byte, chunkIndex uint32) string {
	return fmt.Sprintf("%x:%d", fileHash, chunkIndex)
}

func (m *memoryFileStorage) ReadChunk(fileHash []byte, chunkIndex uint32) ([]byte, error) {
	data, ok := m.chunks[key(fileHash, chunkIndex)]
	if !ok {
		return nil, fmt.Errorf("chunk not found")
	}
	return append([]byte(nil), data...), nil
}

func (m *memoryFileStorage) WriteChunk(fileHash []byte, chunkIndex uint32, data []byte) error {
	m.chunks[key(fileHash, chunkIndex)] = append([]byte(nil), data...)
	return nil
}

func (m *memoryFileStorage) HasChunk(fileHash []byte, chunkIndex uint32) (bool, error) {
	_, ok := m.chunks[key(fileHash, chunkIndex)]
	return ok, nil
}

func TestBuildBatch(t *testing.T) {
	st := newMemoryFileStorage()
	fileHash := []byte("file-hash")
	_ = st.WriteChunk(fileHash, 0, []byte("a"))
	_ = st.WriteChunk(fileHash, 1, []byte("b"))

	batch, err := BuildBatch(fileHash, []uint32{0, 1}, 2, st)
	if err != nil {
		t.Fatalf("BuildBatch failed: %v", err)
	}

	if len(batch.GetChunks()) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(batch.GetChunks()))
	}
	if batch.GetChunks()[0].GetChunkIndex() != 0 || batch.GetChunks()[1].GetChunkIndex() != 1 {
		t.Fatalf("chunk indices not preserved")
	}
}

func TestBuildBatchRejectsOverProtocolLimit(t *testing.T) {
	st := newMemoryFileStorage()
	_, err := BuildBatch([]byte("file-hash"), []uint32{0}, 65, st)
	if err == nil {
		t.Fatalf("expected error for maxChunks > 64")
	}
}

func TestVerifyAndStoreBatch(t *testing.T) {
	st := newMemoryFileStorage()
	fileHash := []byte("file-hash")
	ch0 := []byte("chunk-0")
	ch1 := []byte("chunk-1")
	h0 := crypto.Hash(ch0)
	h1 := crypto.Hash(ch1)

	meta := &pb.FileMeta{
		FileHash:    fileHash,
		ChunkHashes: [][]byte{h0[:], h1[:]},
	}
	batch := &pb.ChunkBatch{
		FileHash: fileHash,
		Chunks: []*pb.ChunkData{
			{ChunkIndex: 0, Data: ch0},
			{ChunkIndex: 1, Data: ch1},
		},
	}

	verified, err := VerifyAndStoreBatch(batch, meta, st)
	if err != nil {
		t.Fatalf("VerifyAndStoreBatch failed: %v", err)
	}
	if len(verified) != 2 || verified[0] != 0 || verified[1] != 1 {
		t.Fatalf("unexpected verified indices: %v", verified)
	}

	data0, _ := st.ReadChunk(fileHash, 0)
	if !bytes.Equal(data0, ch0) {
		t.Fatalf("stored chunk 0 mismatch")
	}
}

func TestVerifyAndStoreBatchHashMismatch(t *testing.T) {
	st := newMemoryFileStorage()
	fileHash := []byte("file-hash")
	ch0 := []byte("chunk-0")
	h0 := crypto.Hash([]byte("different"))

	meta := &pb.FileMeta{
		FileHash:    fileHash,
		ChunkHashes: [][]byte{h0[:]},
	}
	batch := &pb.ChunkBatch{
		FileHash: fileHash,
		Chunks: []*pb.ChunkData{
			{ChunkIndex: 0, Data: ch0},
		},
	}

	_, err := VerifyAndStoreBatch(batch, meta, st)
	if err == nil {
		t.Fatalf("expected hash mismatch error")
	}
}

func TestMultiSourcePlanTracksProvidersAndRanges(t *testing.T) {
	plan := NewMultiSourcePlan()
	if err := plan.AddPeerRanges("peer-a", []ChunkRange{{Start: 0, End: 2}}); err != nil {
		t.Fatalf("add peer-a ranges: %v", err)
	}
	if err := plan.AddPeerRanges("peer-b", []ChunkRange{{Start: 2, End: 4}}); err != nil {
		t.Fatalf("add peer-b ranges: %v", err)
	}

	providers := plan.ProvidersForChunk(2)
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers for chunk 2, got %d", len(providers))
	}
	if !slices.Contains(providers, "peer-a") || !slices.Contains(providers, "peer-b") {
		t.Fatalf("unexpected providers for chunk 2: %v", providers)
	}

	ranges := plan.RangesForPeer("peer-a")
	if len(ranges) != 1 || ranges[0].Start != 0 || ranges[0].End != 2 {
		t.Fatalf("unexpected ranges for peer-a: %v", ranges)
	}
}

func TestMultiSourcePlanMerge(t *testing.T) {
	base := NewMultiSourcePlan()
	other := NewMultiSourcePlan()

	if err := base.AddPeerRanges("peer-a", []ChunkRange{{Start: 0, End: 1}}); err != nil {
		t.Fatalf("seed base: %v", err)
	}
	if err := other.AddPeerRanges("peer-b", []ChunkRange{{Start: 1, End: 2}}); err != nil {
		t.Fatalf("seed other: %v", err)
	}

	if err := base.Merge(other); err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	providers := base.ProvidersForChunk(1)
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers for chunk 1 after merge, got %d", len(providers))
	}
}
