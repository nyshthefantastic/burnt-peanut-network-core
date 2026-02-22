package dag

import (
	"encoding/binary"
)

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

//func appendTotals(buf []byte, t wire.CumulativeTotals) []byte {}
