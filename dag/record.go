package dag

import (
	"encoding/binary"
	"github.com/nyshthefantastic/burnt-peanut-network-core/crypto"
	"github.com/nyshthefantastic/burnt-peanut-network-core/wire"
)

func SignableBytes(r *wire.CumulativeTotals) []byte                              {}
func AttachedSenderSigs(r *wire.CumulativeTotals, sig crypto.Signature) [][]byte {}
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

func appendTotals(buf []byte, t wire.CumulativeTotals) []byte {
	buf = appendUint64(buf, t.CumulativeReceived)
	buf = appendUint64(buf, t.CumulativeSent)
	buf = appendUint64(buf, t.RecordIndex)
	return buf
}
