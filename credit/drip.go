package credit

import "time"

func ComputeDripAllowance(createdAt time.Time, now time.Time, params CreditParams) int64 {
	if now.Before(createdAt) {
		return 0
	}
	days := now.Sub(createdAt).Hours() / 24.0
	drip := days * float64(params.DripCap)

	if drip > float64(params.DripCap) {
		drip = float64(params.DripCap)
	}

	if drip < 0 {
		return 0
	}
	return int64(drip)
}
