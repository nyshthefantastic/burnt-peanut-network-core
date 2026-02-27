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


func (s *Store) ListFiles(limit int, offset int) ([]*pb.FileMeta, error){
	if limit <= 0 {
		return nil, errors.New("limit must be positive")
	}

	if offset < 0 {
		return nil, errors.New("offset must be positive")
	}

	rows, err := s.reader.Query("SELECT file_hash, file_name, file_size, chunk_size, chunk_hashes, origin_pubkey, origin_sig, created_at FROM files ORDER BY created_at DESC LIMIT ? OFFSET ?", limit, offset)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	files := make([]*pb.FileMeta, 0)

	for rows.Next() {
		file, err := scanFileMeta(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return files, nil
}


func (s *Store) SearchFileByName(query string) ([]*pb.FileMeta, error){
	if query == "" {
		return nil, errors.New("query is required")
	}

	rows, err := s.reader.Query("SELECT file_hash, file_name, file_size, chunk_size, chunk_hashes, origin_pubkey, origin_sig, created_at FROM files WHERE file_name LIKE ?", "%"+query+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	files := make([]*pb.FileMeta, 0)
	for rows.Next() {
		file, err := scanFileMeta(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return files, nil
}


func scanFileMeta(scanner interface{ Scan(...any) error }) (*pb.FileMeta, error){
	var file pb.FileMeta
	var chunkHashesBlob []byte

	err := scanner.Scan(
		&file.FileHash,
		&file.FileName,
		&file.FileSize,
		&file.ChunkSize,
		&chunkHashesBlob,
		&file.OriginPubkey,
		&file.OriginSig,
		&file.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	file.ChunkHashes = splitHashes(chunkHashesBlob)

	return &file, nil
}