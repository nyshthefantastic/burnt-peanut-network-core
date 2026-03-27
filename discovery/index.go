package discovery

import (
	"bytes"
	"fmt"
	"sync"
)

type FileIndex struct {
	mu    sync.Mutex
	files map[string]map[uint32]struct{}
}

func NewFileIndex() *FileIndex {
	return &FileIndex{
		files: make(map[string]map[uint32]struct{}),
	}
}

func (f *FileIndex) AddFile(fileHash []byte, chunkIndices []uint32) error {
	if f == nil {
		return fmt.Errorf("file index is nil")
	}
	if len(fileHash) == 0 {
		return fmt.Errorf("file hash is required")
	}
	if len(chunkIndices) == 0 {
		return fmt.Errorf("at least one chunk index is required")
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	key := string(fileHash)
	if f.files[key] == nil {
		f.files[key] = make(map[uint32]struct{})
	}
	for _, idx := range chunkIndices {
		f.files[key][idx] = struct{}{}
	}
	return nil
}

func (f *FileIndex) RemoveFile(fileHash []byte) error {
	if f == nil {
		return fmt.Errorf("file index is nil")
	}
	if len(fileHash) == 0 {
		return fmt.Errorf("file hash is required")
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.files, string(fileHash))
	return nil
}

func (f *FileIndex) HasFile(fileHash []byte) bool {
	if f == nil || len(fileHash) == 0 {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.files[string(fileHash)]
	return ok
}

func (f *FileIndex) GetAvailableChunks(fileHash []byte) []uint32 {
	if f == nil || len(fileHash) == 0 {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	indices := f.files[string(fileHash)]
	out := make([]uint32, 0, len(indices))
	for idx := range indices {
		out = append(out, idx)
	}
	return out
}

func (f *FileIndex) HasChunk(fileHash []byte, chunkIndex uint32) bool {
	if f == nil || len(fileHash) == 0 {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.files[string(fileHash)][chunkIndex]
	return ok
}

func EqualFileHash(a []byte, b []byte) bool {
	return bytes.Equal(a, b)
}
