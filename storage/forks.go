package storage

import (
	"errors"

	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
	"google.golang.org/protobuf/proto"
)




func (s *Store) InsertForkEvidence(evidence *pb.ForkEvidence) error {
	if evidence == nil {
		return errors.New("fork evidence is required")
	}

	// RecordA and RecordB are full ShareRecord structs — serialize to blobs
	recordABlob, err := proto.Marshal(evidence.RecordA)
	if err != nil {
		return err
	}

	recordBBlob, err := proto.Marshal(evidence.RecordB)
	if err != nil {
		return err
	}

	_, err = s.writer.Exec(`
		INSERT INTO fork_evidence (device_pubkey, record_a, record_b, 
			reporter_pubkey, reporter_sig, detected_at) 
		VALUES (?, ?, ?, ?, ?, ?)`,
		evidence.DevicePubkey,
		recordABlob,
		recordBBlob,
		evidence.ReporterPubkey,
		evidence.ReporterSig,
		evidence.DetectedAt,
	)
	return err
}

func (s *Store) GetForkEvidence(devicePubkey []byte) ([]*pb.ForkEvidence, error) {
	if devicePubkey == nil {
		return nil, errors.New("device public key is required")
	}

	rows, err := s.reader.Query(`
		SELECT device_pubkey, record_a, record_b, reporter_pubkey, 
			reporter_sig, detected_at 
		FROM fork_evidence 
		WHERE device_pubkey = ?`, devicePubkey)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	evidences := make([]*pb.ForkEvidence, 0)
	for rows.Next() {
		evidence, err := scanForkEvidence(rows)
		if err != nil {
			return nil, err
		}
		evidences = append(evidences, evidence)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return evidences, nil
}

func (s *Store) HasForkEvidence(devicePubkey []byte) (bool, error) {
	if devicePubkey == nil {
		return false, errors.New("device public key is required")
	}

	var count int
	err := s.reader.QueryRow(
		"SELECT COUNT(1) FROM fork_evidence WHERE device_pubkey = ?",
		devicePubkey,
	).Scan(&count)

	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func scanForkEvidence(scanner interface{ Scan(...any) error }) (*pb.ForkEvidence, error) {
	var evidence pb.ForkEvidence
	var recordABlob, recordBBlob []byte

	err := scanner.Scan(
		&evidence.DevicePubkey,
		&recordABlob,
		&recordBBlob,
		&evidence.ReporterPubkey,
		&evidence.ReporterSig,
		&evidence.DetectedAt,
	)
	if err != nil {
		return nil, err
	}

	// deserialize the two records from blobs back to structs
	evidence.RecordA = &pb.ShareRecord{}
	if err := proto.Unmarshal(recordABlob, evidence.RecordA); err != nil {
		return nil, err
	}

	evidence.RecordB = &pb.ShareRecord{}
	if err := proto.Unmarshal(recordBBlob, evidence.RecordB); err != nil {
		return nil, err
	}

	return &evidence, nil
}