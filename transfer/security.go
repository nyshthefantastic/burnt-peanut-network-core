package transfer

import (
	"fmt"
	"time"

	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

const DefaultTransferRequestTTLSeconds int64 = 5 * 60

func ValidateTransferRequestWindow(req *pb.TransferRequest, now int64, maxAgeSeconds int64) error {
	if req == nil {
		return fmt.Errorf("transfer request is required")
	}
	if maxAgeSeconds <= 0 {
		maxAgeSeconds = DefaultTransferRequestTTLSeconds
	}
	if req.GetTimestamp() <= 0 {
		return fmt.Errorf("transfer request timestamp is required")
	}
	if req.GetTimestamp() > now+30 {
		return fmt.Errorf("transfer request timestamp is in the future")
	}
	if now-req.GetTimestamp() > maxAgeSeconds {
		return fmt.Errorf("transfer request expired")
	}
	return nil
}

func IsTransferRequestExpired(req *pb.TransferRequest, now time.Time) bool {
	if req == nil {
		return true
	}
	return now.Unix()-req.GetTimestamp() > DefaultTransferRequestTTLSeconds
}
