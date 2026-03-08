package cabi

import "sync"

type handleRegistry struct {
	mu      sync.Mutex
	handles map[uintptr]interface{}
	next    uintptr
}