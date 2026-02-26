package storage

import (
	"errors"

	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)


func (s *Store) InsertFileMeta(file *pb.FileMeta) error{}


func (s *Store) GetFileMeta(fileHash []byte) (*pb.FileMeta, error){
	if fileHash == nil {
		return nil, errors.New("file hash is required")
	}

	row := s.reader.QueryRow("SELECT file_hash, file_name, file_size, chunk_size, chunk_hashes, origin_pubkey, origin_sig, created_at FROM files WHERE file_hash = ?", fileHash)

	return scanFileMeta(row)
}


func (s *Store) ListFiles(limit int, offset int) ([]*pb.FileMeta, error){}


func (s *Store) SearchFileByName(query string) ([]*pb.FileMeta, error){}


func scanFileMeta(scanner interface{ Scan(...any) error }) (*pb.FileMeta, error){
	var file pb.FileMeta
	err := scanner.Scan(
		&file.FileHash,
		&file.FileName,
		&file.FileSize,
		&file.ChunkSize,
		&file.ChunkHashes,
		&file.OriginPubkey,
		&file.OriginSig,
		&file.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &file, nil
}