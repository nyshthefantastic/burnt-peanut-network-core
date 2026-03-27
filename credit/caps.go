package credit

import (
	"bytes"
	"github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

type PeerEpoch struct {
	peer  string
	epoch int64
}

func ApplyPerPeerCaps(records []*gen.ShareRecord, devicePubKey []byte, params CreditParams) int64 {
	totals := make(map[PeerEpoch]int64)
	for _, r := range records {
		if bytes.Equal(r.SenderPubkey, devicePubKey) {
			key := PeerEpoch{
				peer:  string(r.ReceiverPubkey),
				epoch: r.Timestamp / params.EpochSeconds,
			}
			totals[key] += int64(r.BytesTotal)
		}
	}

	var total int64
	for _, rawBytes := range totals {
		if rawBytes > params.PerPeerCap {
			rawBytes = params.PerPeerCap
		}
		total += rawBytes
	}
	return total
}
