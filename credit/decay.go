package credit

import (
	"math"
	"time"
)

func DecayFactor(interactionTime time.Time, now time.Time, halfLife time.Duration) float64 {
	if now.Before(interactionTime) {
		return 1.0
	}
	age := now.Sub(interactionTime)
	exponent := float64(age) / float64(halfLife)
	return math.Pow(0.5, exponent)
}

func DecayedValue(rawBytes int64, interactionTime time.Time, now time.Time, halfLife time.Duration) int64 {
	factor := DecayFactor(interactionTime, now, halfLife)
	return int64(float64(rawBytes) * factor)
}
