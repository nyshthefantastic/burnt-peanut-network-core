package dag

import (
	"encoding/binary"
	"github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

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

func AttachSenderSig(r *gen.ShareRecord, sig []byte) []byte   {}
func AttachReceiverSig(r *gen.ShareRecord, sig []byte) []byte {}

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
