package credit

import (
	"fmt"
	"time"
)

const MB = 1024 * 1024

type CreditParams struct {
	DripRate        int64
	DripCap         int64
	DiversityWindow int
	HalfLife        time.Duration
	PerPeerCap      int64
	EpochLen        time.Duration
}

func DefaultParams() CreditParams {
	return CreditParams{
		DripRate:        2 * MB,
		DripCap:         50 * MB,
		DiversityWindow: 50,
		HalfLife:        time.Hour * 24 * 90,
		PerPeerCap:      500 * MB,
		EpochLen:        time.Hour * 24 * 7,
	}
}
func (c CreditParams) Validate() error {
	if c.DripRate < 0 {
		return fmt.Errorf("DripRate cannot be negative")
	}
	if c.DripCap < 0 {
		return fmt.Errorf("DripCap cannot be negative")
	}
	if c.DiversityWindow < 0 {
		return fmt.Errorf("DiversityWindow cannot be negative")
	}
	if c.HalfLife <= 0 {
		return fmt.Errorf("HalfLife cannot be negative")
	}
	if c.PerPeerCap < 0 {
		return fmt.Errorf("PerPeerCap cannot be negative")
	}
	if c.EpochLen <= 0 {
		return fmt.Errorf("EpochLen cannot be negative")
	}
	return nil
}
