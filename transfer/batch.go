package transfer

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/nyshthefantastic/burnt-peanut-network-core/crypto"
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

const MaxChunksPerBatch = 64

// ChunkRange represents an inclusive chunk interval [Start, End].
type ChunkRange struct {
	Start uint32
	End   uint32
}

// MultiSourcePlan tracks which peers can provide which chunk indices/ranges.
// This is a data model for phase-1 planning only; scheduling logic is phase-2.
type MultiSourcePlan struct {
	mu sync.Mutex
	// peerID -> ranges advertised by that peer
	peerRanges map[string][]ChunkRange
	// chunkIndex -> peers that can provide the chunk
	chunkProviders map[uint32]map[string]struct{}
}

func NewMultiSourcePlan() *MultiSourcePlan {
	return &MultiSourcePlan{
		peerRanges:     make(map[string][]ChunkRange),
		chunkProviders: make(map[uint32]map[string]struct{}),
	}
}

func (m *MultiSourcePlan) AddPeerRanges(peerID string, ranges []ChunkRange) error {
	if m == nil {
		return fmt.Errorf("plan is nil")
	}
	if peerID == "" {
		return fmt.Errorf("peer id is required")
	}
	if len(ranges) == 0 {
		return fmt.Errorf("at least one range is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, r := range ranges {
		if r.End < r.Start {
			return fmt.Errorf("invalid chunk range: %d-%d", r.Start, r.End)
		}
		m.peerRanges[peerID] = append(m.peerRanges[peerID], r)
		for idx := r.Start; idx <= r.End; idx++ {
			if m.chunkProviders[idx] == nil {
				m.chunkProviders[idx] = make(map[string]struct{})
			}
			m.chunkProviders[idx][peerID] = struct{}{}
		}
	}
	return nil
}

func (m *MultiSourcePlan) ProvidersForChunk(chunkIndex uint32) []string {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	providerSet := m.chunkProviders[chunkIndex]
	out := make([]string, 0, len(providerSet))
	for peerID := range providerSet {
		out = append(out, peerID)
	}
	return out
}

func (m *MultiSourcePlan) RangesForPeer(peerID string) []ChunkRange {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	ranges := m.peerRanges[peerID]
	out := make([]ChunkRange, len(ranges))
	copy(out, ranges)
	return out
}

func (m *MultiSourcePlan) Merge(other *MultiSourcePlan) error {
	if m == nil {
		return fmt.Errorf("plan is nil")
	}
	if other == nil {
		return nil
	}

	for peerID, ranges := range other.snapshotPeerRanges() {
		if err := m.AddPeerRanges(peerID, ranges); err != nil {
			return err
		}
	}
	return nil
}

func (m *MultiSourcePlan) snapshotPeerRanges() map[string][]ChunkRange {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make(map[string][]ChunkRange, len(m.peerRanges))
	for peerID, ranges := range m.peerRanges {
		copied := make([]ChunkRange, len(ranges))
		copy(copied, ranges)
		out[peerID] = copied
	}
	return out
}

func BuildBatch(fileHash []byte, chunkIndices []uint32, maxChunks int, storage FileStorage) (*pb.ChunkBatch, error) {
	if storage == nil {
		return nil, fmt.Errorf("storage is required")
	}
	if len(fileHash) == 0 {
		return nil, fmt.Errorf("file hash is required")
	}
	if maxChunks <= 0 {
		return nil, fmt.Errorf("max chunks must be > 0")
	}
	if maxChunks > MaxChunksPerBatch {
		return nil, fmt.Errorf("max chunks exceeds protocol limit: %d > %d", maxChunks, MaxChunksPerBatch)
	}
	if len(chunkIndices) == 0 {
		return nil, fmt.Errorf("chunk indices are required")
	}
	if len(chunkIndices) > maxChunks {
		return nil, fmt.Errorf("requested chunks exceed maxChunks: %d > %d", len(chunkIndices), maxChunks)
	}
	if len(chunkIndices) > MaxChunksPerBatch {
		return nil, fmt.Errorf("requested chunks exceed protocol limit: %d > %d", len(chunkIndices), MaxChunksPerBatch)
	}

	chunks := make([]*pb.ChunkData, 0, len(chunkIndices))
	for _, idx := range chunkIndices {
		data, err := storage.ReadChunk(fileHash, idx)
		if err != nil {
			return nil, fmt.Errorf("read chunk %d: %w", idx, err)
		}
		chunks = append(chunks, &pb.ChunkData{
			ChunkIndex: idx,
			Data:       data,
		})
	}

	return &pb.ChunkBatch{
		FileHash: append([]byte(nil), fileHash...),
		Chunks:   chunks,
	}, nil
}

func VerifyAndStoreBatch(batch *pb.ChunkBatch, meta *pb.FileMeta, storage FileStorage) ([]uint32, error) {
	if batch == nil {
		return nil, fmt.Errorf("batch is required")
	}
	if meta == nil {
		return nil, fmt.Errorf("file metadata is required")
	}
	if storage == nil {
		return nil, fmt.Errorf("storage is required")
	}
	if !bytes.Equal(batch.GetFileHash(), meta.GetFileHash()) {
		return nil, fmt.Errorf("batch file hash does not match metadata")
	}
	if len(batch.GetChunks()) > MaxChunksPerBatch {
		return nil, fmt.Errorf("batch exceeds protocol max chunks: %d > %d", len(batch.GetChunks()), MaxChunksPerBatch)
	}

	verified := make([]uint32, 0, len(batch.GetChunks()))
	chunkHashes := meta.GetChunkHashes()

	for _, chunk := range batch.GetChunks() {
		if chunk == nil {
			return nil, fmt.Errorf("batch contains nil chunk")
		}

		idx := chunk.GetChunkIndex()
		if int(idx) >= len(chunkHashes) {
			return nil, fmt.Errorf("chunk index out of range: %d", idx)
		}

		expectedHash := chunkHashes[idx]
		computedHash := crypto.Hash(chunk.GetData())
		if !bytes.Equal(expectedHash, computedHash[:]) {
			return nil, fmt.Errorf("chunk hash mismatch at index %d", idx)
		}

		if err := storage.WriteChunk(batch.GetFileHash(), idx, chunk.GetData()); err != nil {
			return nil, fmt.Errorf("write chunk %d: %w", idx, err)
		}
		verified = append(verified, idx)
	}

	return verified, nil
}

func MissingChunkIndices(storage FileStorage, fileHash []byte, requestedIndices []uint32) ([]uint32, error) {
	if storage == nil {
		return nil, fmt.Errorf("storage is required")
	}
	if len(fileHash) == 0 {
		return nil, fmt.Errorf("file hash is required")
	}

	missing := make([]uint32, 0, len(requestedIndices))
	for _, idx := range requestedIndices {
		has, err := storage.HasChunk(fileHash, idx)
		if err != nil {
			return nil, fmt.Errorf("check chunk %d: %w", idx, err)
		}
		if !has {
			missing = append(missing, idx)
		}
	}
	return missing, nil
}
