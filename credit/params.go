package credit

import (
	"fmt"
)

const MB = 1024 * 1024

type CreditParams struct {
	DripRate        int64
	MaxBalance      int64
	WindowSize      int32
	HalfLifeSeconds int64
	PerPeerCap      int64
	EpochSeconds    int64
}

func DefaultParams() CreditParams {
	return CreditParams{
		DripRate:        2 * MB,
		MaxBalance:      50 * MB,
		WindowSize:      50,
		HalfLifeSeconds: 7776000, // 90 days
		PerPeerCap:      500 * MB,
		EpochSeconds:    604800, // 1 week
	}
}
func (c CreditParams) Validate() error {
	if c.DripRate < 0 {
		return fmt.Errorf("DripRate cannot be negative")
	}
	if c.MaxBalance < 0 {
		return fmt.Errorf("MaxBalance cannot be negative")
	}
	if c.WindowSize < 0 {
		return fmt.Errorf("DiversityWindow cannot be negative")
	}
	if c.HalfLifeSeconds <= 0 {
		return fmt.Errorf("HalfLife cannot be negative")
	}
	if c.PerPeerCap < 0 {
		return fmt.Errorf("PerPeerCap cannot be negative")
	}
	if c.EpochSeconds <= 0 {
		return fmt.Errorf("EpochLen cannot be negative")
	}
	return nil
}
