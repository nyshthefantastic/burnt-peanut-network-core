package dag

import (
	"bytes"
	"fmt"

	"github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

func VerifyChainSegment(records []*gen.ShareRecord, devicePubkey []byte) error {
	if len(records) < 2 {
		return nil
	}
	for i := 0; i < len(records)-1; i++ {
		curr := records[i]
		next := records[i+1]
		_, currIndex := deviceChainFields(curr, devicePubkey)
		nextPrev, nextIndex := deviceChainFields(next, devicePubkey)
		if !bytes.Equal(nextPrev, curr.Id) {
			return fmt.Errorf("broken chain link at index %d", i)
		}
		if nextIndex != currIndex+1 {
			return fmt.Errorf("chain index gap at position %d", i)
		}
	}
	return nil
}

func DetectFork(a *gen.ShareRecord, b *gen.ShareRecord, devicePubKey []byte) *gen.ForkEvidence {
	_, indexA := deviceChainFields(a, devicePubKey)
	_, indexB := deviceChainFields(b, devicePubKey)
	if indexA == indexB {
		if bytes.Equal(a.Id, b.Id) {
			return nil
		}
		return &gen.ForkEvidence{
			DevicePubkey: devicePubKey,
			RecordA:      a,
			RecordB:      b,
		}
	}
	return nil
}

func deviceChainFields(r *gen.ShareRecord, devicePubkey []byte) (prevHash []byte, index uint64) {
	if bytes.Equal(r.SenderPubkey, devicePubkey) {
		return r.PrevSender, r.SenderRecordIndex
	}
	return r.PrevReceiver, r.ReceiverRecordIndex
}
