package credit

import (
	"bytes"
	"time"

	"github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

func ComputeEffectiveBalance(records []*gen.ShareRecord, devicePubKey []byte, deviceCreatedAt int64, now int64, params CreditParams) int64 {
	dripAllowance := ComputeDripAllowance(time.Unix(deviceCreatedAt, 0), time.Unix(now, 0), params)
	diversityWeightedCredits := DiversityWeightedCredit(records, devicePubKey, params.WindowSize)
	var cumulativeReceived int64
	for _, r := range records {
		if bytes.Equal(r.ReceiverPubkey, devicePubKey) {
			cumulativeReceived += int64(r.BytesTotal)
		}
	}
	var rawCredit int64
	var decayedCredit int64
	for _, r := range records {
		if bytes.Equal(r.SenderPubkey, devicePubKey) {
			raw := int64(r.BytesTotal)
			rawCredit += raw
			decayed := DecayedValue(raw, time.Unix(r.Timestamp, 0), time.Unix(now, 0), time.Duration(params.HalfLifeSeconds)*time.Second)
			decayedCredit += decayed
		}
	}
	decayPenalty := rawCredit - decayedCredit
	effectiveBalance := dripAllowance + diversityWeightedCredits - cumulativeReceived - decayPenalty
	return effectiveBalance
}
