package storage

import (
	"encoding/binary"
	"errors"

	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
	"google.golang.org/protobuf/proto"
)

/*
 Periodic snapshots of a device's chain state, co-signed by witnesses. When we meet a new device, instead of replaying their entire chain history, we show them a recent checkpoint.
*/

func (s *Store) InsertCheckpoint(checkpoint *pb.Checkpoint) error {
	if checkpoint == nil {
		return errors.New("checkpoint is required")
	}

	var cumulativeSent, cumulativeReceived uint64
	if checkpoint.Totals != nil {
		cumulativeSent = checkpoint.Totals.CumulativeSent
		cumulativeReceived = checkpoint.Totals.CumulativeReceived
	}

	// witnesses is a repeated nested message — serialize to a single blob
	witnessesBlob, err := marshalWitnesses(checkpoint.Witnesses)
	if err != nil {
		return err
	}

	_, err = s.writer.Exec(`
		INSERT INTO checkpoints (device_pubkey, chain_head, record_index, 
			cumulative_sent, cumulative_received, raw_balance, timestamp, 
			device_sig, witnesses, confidence) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		checkpoint.DevicePubkey,
		checkpoint.ChainHead,
		checkpoint.RecordIndex,
		cumulativeSent,
		cumulativeReceived,
		checkpoint.RawBalance,
		checkpoint.Timestamp,
		checkpoint.DeviceSig,
		witnessesBlob,
		checkpoint.Confidence,
	)
	return err
}

func (s *Store) GetLatestCheckpoint(pubkey []byte) (*pb.Checkpoint, error) {
	if pubkey == nil {
		return nil, errors.New("public key is required")
	}
	row := s.reader.QueryRow(`
		SELECT device_pubkey, chain_head, record_index, cumulative_sent, 
			cumulative_received, raw_balance, timestamp, device_sig, 
			witnesses, confidence 
		FROM checkpoints 
		WHERE device_pubkey = ? 
		ORDER BY record_index DESC LIMIT 1`, pubkey)

	return scanCheckpoint(row)
}

func (s *Store) GetCheckpointAtIndex(pubkey []byte, index uint64) (*pb.Checkpoint, error) {
	if pubkey == nil {
		return nil, errors.New("public key is required")
	}
	row := s.reader.QueryRow(`
		SELECT device_pubkey, chain_head, record_index, cumulative_sent, 
			cumulative_received, raw_balance, timestamp, device_sig, 
			witnesses, confidence 
		FROM checkpoints 
		WHERE device_pubkey = ? AND record_index = ?`, pubkey, index)

	return scanCheckpoint(row)
}

func (s *Store) GetCheckpointsByDevice(pubkey []byte) ([]*pb.Checkpoint, error) {
	if pubkey == nil {
		return nil, errors.New("public key is required")
	}
	rows, err := s.reader.Query(`
		SELECT device_pubkey, chain_head, record_index, cumulative_sent, 
			cumulative_received, raw_balance, timestamp, device_sig, 
			witnesses, confidence 
		FROM checkpoints 
		WHERE device_pubkey = ? 
		ORDER BY record_index ASC`, pubkey)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	checkpoints := make([]*pb.Checkpoint, 0)
	for rows.Next() {
		checkpoint, err := scanCheckpoint(rows)
		if err != nil {
			return nil, err
		}
		checkpoints = append(checkpoints, checkpoint)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return checkpoints, nil
}

func scanCheckpoint(scanner interface{ Scan(...any) error }) (*pb.Checkpoint, error) {
	var checkpoint pb.Checkpoint
	var cumulativeSent, cumulativeReceived uint64
	var witnessesBlob []byte

	err := scanner.Scan(
		&checkpoint.DevicePubkey,
		&checkpoint.ChainHead,
		&checkpoint.RecordIndex,
		&cumulativeSent,
		&cumulativeReceived,
		&checkpoint.RawBalance,
		&checkpoint.Timestamp,
		&checkpoint.DeviceSig,
		&witnessesBlob,
		&checkpoint.Confidence,
	)
	if err != nil {
		return nil, err
	}

	checkpoint.Totals = &pb.CumulativeTotals{
		CumulativeSent:     cumulativeSent,
		CumulativeReceived: cumulativeReceived,
	}

	checkpoint.Witnesses, err = unmarshalWitnesses(witnessesBlob)
	if err != nil {
		return nil, err
	}

	return &checkpoint, nil
}

// Each witness is proto.Marshal'd and prefixed with a 4-byte length.
func marshalWitnesses(witnesses []*pb.CheckpointWitness) ([]byte, error) {
	if len(witnesses) == 0 {
		return nil, nil
	}

	var out []byte
	for _, w := range witnesses {
		data, err := proto.Marshal(w)
		if err != nil {
			return nil, err
		}
		// 4-byte length prefix for each witness
		length := make([]byte, 4)
		binary.BigEndian.PutUint32(length, uint32(len(data)))
		out = append(out, length...)
		out = append(out, data...)
	}
	return out, nil
}

func unmarshalWitnesses(data []byte) ([]*pb.CheckpointWitness, error) {
	if len(data) == 0 {
		return nil, nil
	}

	var witnesses []*pb.CheckpointWitness
	offset := 0

	for offset < len(data) {
		if offset+4 > len(data) {
			return nil, errors.New("invalid witness data: truncated length")
		}
		length := binary.BigEndian.Uint32(data[offset : offset+4])
		offset += 4

		if offset+int(length) > len(data) {
			return nil, errors.New("invalid witness data: truncated payload")
		}
		w := &pb.CheckpointWitness{}
		err := proto.Unmarshal(data[offset:offset+int(length)], w)
		if err != nil {
			return nil, err
		}
		witnesses = append(witnesses, w)
		offset += int(length)
	}
	return witnesses, nil
}
