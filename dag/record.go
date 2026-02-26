package dag

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/nyshthefantastic/burnt-peanut-network-core/crypto"
	"github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

func ValidateFileMeta(r *gen.FileMeta) error {
	message := FileMetaSignableBytes(r)
	ok, err := crypto.Verify(r.OriginPubkey, message, r.OriginSig)
	if err != nil {
		return fmt.Errorf("Origin sig verification failed: %v", err)
	}
	if !ok {
		return fmt.Errorf("Signature verification failed")
	}
	return nil
}

func ValidateTransferRequest(r *gen.TransferRequest) error {
	message := TransferRequestSignableBytes(r)
	ok, err := crypto.Verify(r.RequesterPubkey, message, r.Signature)
	if err != nil {
		return fmt.Errorf("Requester sig verification failed: %w", err)
	}
	if !ok {
		return fmt.Errorf("invalid request signature")
	}
	return nil
}

func ValidateShareRecord(r *gen.ShareRecord) error {
	message := SignableBytes(r)
	ok, err := crypto.Verify(r.SenderPubkey, message, r.SenderSig)
	if err != nil {
		return fmt.Errorf("sender sig verification failed: %w", err)
	}
	if !ok {
		return fmt.Errorf("invalid sender signature")
	}
	ok, err = crypto.Verify(r.ReceiverPubkey, message, r.ReceiverSig)

	if err != nil {
		return fmt.Errorf("receiver sig verification failed: %w", err)
	}
	if !ok {
		return fmt.Errorf("invalid receiver signature")
	}
	expectedId := crypto.Hash(message)
	if !bytes.Equal(expectedId[:], r.Id) {
		return fmt.Errorf("record id mismatch")
	}
	return nil

}

func FileMetaSignableBytes(r *gen.FileMeta) []byte {
	var buf []byte
	buf = append(buf, r.FileHash...)
	buf = append(buf, []byte(r.FileName)...)
	buf = appendUint64(buf, r.FileSize)
	buf = appendUint64(buf, r.ChunkSize)
	buf = appendUint32(buf, uint32(len(r.ChunkHashes)))
	for _, ch := range r.ChunkHashes {
		buf = append(buf, ch...)
	}
	buf = append(buf, r.OriginPubkey...)
	buf = appendUint64(buf, uint64(r.CreatedAt))
	return buf
}

func TransferRequestSignableBytes(r *gen.TransferRequest) []byte {
	var buf []byte
	buf = append(buf, r.RequesterPubkey...)
	buf = append(buf, r.FileHash...)
	buf = appendUint32(buf, uint32(len(r.ChunkIndices)))
	for _, idx := range r.ChunkIndices {
		buf = appendUint32(buf, idx)
	}
	buf = append(buf, r.Nonce...)
	buf = appendUint64(buf, uint64(r.Timestamp))
	return buf
}
func SignableBytes(r *gen.ShareRecord) []byte {
	var buf []byte
	buf = append(buf, r.SenderPubkey...)
	buf = append(buf, r.ReceiverPubkey...)
	buf = append(buf, r.PrevSender...)
	buf = append(buf, r.PrevReceiver...)
	buf = appendUint64(buf, r.SenderRecordIndex)
	buf = appendUint64(buf, r.ReceiverRecordIndex)
	if r.SenderTotals != nil {
		buf = appendTotals(buf, r.SenderTotals)
	} else {
		buf = appendUint64(buf, 0)
		buf = appendUint64(buf, 0)
	}
	if r.ReceiverTotals != nil {
		buf = appendTotals(buf, r.ReceiverTotals)
	} else {
		buf = appendUint64(buf, 0)
		buf = appendUint64(buf, 0)
	}
	buf = append(buf, r.RequestHash...)
	buf = append(buf, r.FileHash...)
	buf = appendUint32(buf, uint32(len(r.ChunkHashes)))
	for _, ch := range r.ChunkHashes {
		buf = append(buf, ch...)
	}
	buf = appendUint64(buf, r.BytesTotal)
	buf = appendUint32(buf, uint32(r.Visibility))
	buf = appendUint64(buf, uint64(r.Timestamp))
	return buf
}

func AttachSenderSig(r *gen.ShareRecord, sig []byte) {
	r.SenderSig = sig
}

func AttachReceiverSig(r *gen.ShareRecord, sig []byte) {
	r.ReceiverSig = sig
	data := SignableBytes(r)
	hash := crypto.Hash(data)
	r.Id = hash[:]
}

func appendUint64(buf []byte, value uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, value)
	return append(buf, b...)
}

func appendUint32(buf []byte, value uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, value)
	return append(buf, b...)
}

func appendTotals(buf []byte, t *gen.CumulativeTotals) []byte {
	buf = appendUint64(buf, t.CumulativeSent)
	buf = appendUint64(buf, t.CumulativeReceived)
	return buf
}
