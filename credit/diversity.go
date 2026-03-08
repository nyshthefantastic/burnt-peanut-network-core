package credit

import (
	"bytes"

	"github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

func DiversityWeightedCredit(records []*gen.ShareRecord, devicePubKey []byte, windowSize int32) int64 {
	if int32(len(records)) > windowSize {
		records = records[len(records)-int(windowSize):]
	}

	counts := make(map[string]int)
	for _, r := range records {
		if bytes.Equal(r.SenderPubkey, devicePubKey) {
			peer := string(r.ReceiverPubkey)
			counts[peer]++
		}
	}
	var total int64
	for _, r := range records {
		if bytes.Equal(r.SenderPubkey, devicePubKey) {
			peer := string(r.ReceiverPubkey)
			count := counts[peer]
			total += int64(r.BytesTotal) / int64(count)
		}
	}
	return total
}
