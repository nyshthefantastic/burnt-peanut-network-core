package storage

import pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"

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

func GetRecord(){}

func GetRecordsByDevice(){}

func GetRecordsBetween(){}

func GetLatestRecord(){}

func CounterpartyDiversity(){}


// serialize: [][]byte → []byte
func joinHashes(hashes [][]byte) []byte {
    var out []byte
    for _, h := range hashes {
        out = append(out, h...)
    }
    return out
}