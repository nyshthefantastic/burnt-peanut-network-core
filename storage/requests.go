package storage

import (
	"encoding/binary"
	"errors"

	"github.com/nyshthefantastic/burnt-peanut-network-core/crypto"

	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
	"google.golang.org/protobuf/proto"
)


func (s *Store) InsertRequest(request *pb.TransferRequest) error {
	if request == nil {
		return errors.New("request is required")
	}
	// as the hash is not in the proto struct we need to generate it
	data, err := proto.Marshal(request)
	if err != nil {
		return err
	}

	hash := crypto.Hash(data)

	// as the chunk indices are an array of uint32 we need to serialize them to a single blob
	chunkIndicesBlob := marshalUint32s(request.ChunkIndices)

	_, err = s.writer.Exec(`
		INSERT INTO transfer_requests (hash, requester_pubkey, file_hash, 
			chunk_indices, nonce, timestamp, signature, status) 
		VALUES (?, ?, ?, ?, ?, ?, ?, 'pending')`,
		hash[:],
		request.RequesterPubkey,
		request.FileHash,
		chunkIndicesBlob,
		request.Nonce,
		request.Timestamp,
		request.Signature,
	)
	return err
}

func (s *Store) GetRequest(hash []byte) (*pb.TransferRequest, error) {
	if hash == nil {
		return nil, errors.New("hash is required")
	}

	row := s.reader.QueryRow(`
		SELECT requester_pubkey, file_hash, chunk_indices, nonce, 
			timestamp, signature 
		FROM transfer_requests 
		WHERE hash = ?`, hash)

	return scanRequest(row)
}

/* maxAge is in seconds. Delete requests where timestamp is older than the cutoff value which is 5 minutes according to our design.

max age = current time in seconds - 300 seconds (5 minutes)

*/

func (s *Store) ExpireOldRequests(maxAge int64) error {

	_, err := s.writer.Exec(
		"DELETE FROM transfer_requests WHERE timestamp < ?", maxAge)
	return err
}

func (s *Store) UpdateStatus(hash []byte, status string) error {
	if hash == nil {
		return errors.New("hash is required")
	}
	if status == "" {
		return errors.New("status is required")
	}

	_, err := s.writer.Exec(
		"UPDATE transfer_requests SET status = ? WHERE hash = ?",
		status, hash)
	return err
}



func scanRequest(scanner interface{ Scan(...any) error }) (*pb.TransferRequest, error) {
	var req pb.TransferRequest
	var chunkIndicesBlob []byte

	err := scanner.Scan(
		&req.RequesterPubkey,
		&req.FileHash,
		&chunkIndicesBlob,
		&req.Nonce,
		&req.Timestamp,
		&req.Signature,
	)
	if err != nil {
		return nil, err
	}

	req.ChunkIndices = unmarshalUint32s(chunkIndicesBlob)
	return &req, nil
}


// serialize array of uint32 to array of bytes (each uint32 is 4 bytes)
func marshalUint32s(values []uint32) []byte {
	if len(values) == 0 {
		return nil
	}
	buf := make([]byte, len(values)*4)
	for i, v := range values {
		binary.BigEndian.PutUint32(buf[i*4:], v)
	}
	return buf
}

// deserialize array of bytes to array of uint32
func unmarshalUint32s(data []byte) []uint32 {
	if len(data) == 0 {
		return nil
	}
	values := make([]uint32, len(data)/4)
	for i := range values {
		values[i] = binary.BigEndian.Uint32(data[i*4:])
	}
	return values
}
