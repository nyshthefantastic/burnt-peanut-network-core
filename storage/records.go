package storage

import (
	"errors"

	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

// this is a method on the Store struct.
func (s *Store) InsertRecord(record *pb.ShareRecord) error {
	/*
the record come as a protobuf msg. 
but we need to insert it as a sqlite row.
	*/


	// if the sender or receiver totals are nil, we need to set them to 0.

	var senderCumulativeSent, senderCumulativeReceived, receiverCumulativeSent, receiverCumulativeReceived uint64

	if record.SenderTotals != nil {
		senderCumulativeSent = record.SenderTotals.CumulativeSent
		senderCumulativeReceived = record.SenderTotals.CumulativeReceived
	}
	if record.ReceiverTotals != nil {
		receiverCumulativeSent = record.ReceiverTotals.CumulativeSent
		receiverCumulativeReceived = record.ReceiverTotals.CumulativeReceived
	}

	// as the chunk hashes are bytes array we need to join them into a single hashe as the sqlite doesnt have an array type.
	chunkHashes := joinHashes(record.ChunkHashes)

	_, err := s.writer.Exec(
		"INSERT INTO share_records (id, sender_pubkey, receiver_pubkey, prev_sender, prev_receiver, sender_record_index, receiver_record_index, sender_cumulative_sent, sender_cumulative_received, receiver_cumulative_sent, receiver_cumulative_received, request_hash, chunk_hashes, bytes_total, timestamp, sender_sig, receiver_sig) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		record.Id,
		record.SenderPubkey,
		record.ReceiverPubkey,
		record.PrevSender,
		record.PrevReceiver,
		record.SenderRecordIndex,
		record.ReceiverRecordIndex,
		senderCumulativeSent,
		senderCumulativeReceived,
		receiverCumulativeSent,
		receiverCumulativeReceived,
		record.RequestHash,
		chunkHashes,
		record.BytesTotal,
		record.Timestamp,
		record.SenderSig,
		record.ReceiverSig,
	)
	
	return err
}

func (s *Store) GetRecord(id []byte) (*pb.ShareRecord, error) {

	row := s.reader.QueryRow("SELECT id, sender_pubkey, receiver_pubkey, prev_sender, prev_receiver, sender_record_index, receiver_record_index, sender_cumulative_sent, sender_cumulative_received, receiver_cumulative_sent, receiver_cumulative_received, request_hash, chunk_hashes, bytes_total, timestamp, sender_sig, receiver_sig FROM share_records WHERE id = ?", id)

	return scanRecord(row)
}

func (s *Store) GetRecordsByDevice(devicePublicKey []byte, fromIndex uint64, limit int) ([]*pb.ShareRecord, error){
	if devicePublicKey == nil {
		return nil, errors.New("device public key is required")
	}

	if limit <= 0 {
		return nil, errors.New("limit must be positive")
	}
	rows, err := s.reader.Query("SELECT id, sender_pubkey, receiver_pubkey, prev_sender, prev_receiver, sender_record_index, receiver_record_index, sender_cumulative_sent, sender_cumulative_received, receiver_cumulative_sent, receiver_cumulative_received, request_hash, chunk_hashes, bytes_total, timestamp, sender_sig, receiver_sig FROM share_records WHERE (sender_pubkey = ? OR receiver_pubkey = ?) AND (sender_record_index >= ? OR receiver_record_index >= ?) ORDER BY timestamp ASC LIMIT ?", devicePublicKey, devicePublicKey, fromIndex, fromIndex, limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	records := make([]*pb.ShareRecord, 0)

	for rows.Next() {
		record, err := scanRecord(rows)
	if err != nil {	
		return nil, err
	}
		records = append(records, record)
	}

	return records, nil
}

func (s *Store) GetLatestRecord() (*pb.ShareRecord, error){}

func (s *Store) CounterpartyDiversity(){}


// serialize: [][]byte → []byte --> when inserting into the database we need to join the chunk hashes into a single hash.
func joinHashes(hashes [][]byte) []byte {
    var out []byte
    for _, h := range hashes {
        out = append(out, h...)
    }
    return out
}

// deserialize: []byte → [][]byte --> when retrieving and sending back from the database we need to split the chunk hashes into an array of hashes.
func splitHashes(data []byte) [][]byte {
    var hashes [][]byte
    for i := 0; i < len(data); i += 32 {
        hashes = append(hashes, data[i:i+32])
    }
    return hashes
}

func scanRecord(scanner interface{ Scan(...any) error }) (*pb.ShareRecord, error) {
    var record pb.ShareRecord
    var senderCumulativeSent, senderCumulativeReceived, receiverCumulativeSent, receiverCumulativeReceived uint64
    var chunkHashesBlob []byte
    err := scanner.Scan(
        &record.Id,
        &record.SenderPubkey,
        &record.ReceiverPubkey,
        &record.PrevSender,
        &record.PrevReceiver,
        &record.SenderRecordIndex,
        &record.ReceiverRecordIndex,
        &senderCumulativeSent,
        &senderCumulativeReceived,
        &receiverCumulativeSent,
        &receiverCumulativeReceived,
        &record.RequestHash,
        &chunkHashesBlob,
        &record.BytesTotal,
        &record.Timestamp,
        &record.SenderSig,
        &record.ReceiverSig,
    )
    if err != nil {
        return nil, err
    }
	record.ChunkHashes = splitHashes(chunkHashesBlob)

    record.SenderTotals = &pb.CumulativeTotals{
        CumulativeSent: senderCumulativeSent,
        CumulativeReceived: senderCumulativeReceived,
    }
    record.ReceiverTotals = &pb.CumulativeTotals{
        CumulativeSent: receiverCumulativeSent,
        CumulativeReceived: receiverCumulativeReceived,
    }
    return &record, nil
}