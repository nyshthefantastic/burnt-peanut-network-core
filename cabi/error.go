//go:build cgo

package main

import (
	"database/sql"
	"errors"
	"strings"
)

// Error codes matching the C header
const (
	ML_OK              int32 = 0
	ML_ERR_INVALID_ARG int32 = 1
	ML_ERR_NOT_FOUND   int32 = 2
	ML_ERR_DB          int32 = 3
	ML_ERR_CRYPTO      int32 = 4
	ML_ERR_EXISTS      int32 = 5
	ML_ERR_OVERFLOW    int32 = 6
	ML_ERR_INTERNAL    int32 = 7
)

// we need to map known Go errors to integer codes that C understands
func errorToCode(err error) int32 {
	if err == nil {
		return ML_OK
	}

	if errors.Is(err, sql.ErrNoRows) {
		return ML_ERR_NOT_FOUND
	}

	msg := err.Error()

	if strings.Contains(msg, "is required") || strings.Contains(msg, "invalid") {
		return ML_ERR_INVALID_ARG
	}

	if strings.Contains(msg, "not found") {
		return ML_ERR_NOT_FOUND
	}

	if strings.Contains(msg, "database") || strings.Contains(msg, "sqlite") {
		return ML_ERR_DB
	}

	if strings.Contains(msg, "key size") || strings.Contains(msg, "signature") || strings.Contains(msg, "attestation") {
		return ML_ERR_CRYPTO
	}

	if strings.Contains(msg, "already exists") || strings.Contains(msg, "UNIQUE constraint") {
		return ML_ERR_EXISTS
	}

	if strings.Contains(msg, "exceeds max size") || strings.Contains(msg, "overflow") {
		return ML_ERR_OVERFLOW
	}

	return ML_ERR_INTERNAL
}

// we need to convert an integer code back to a Go error for use in callbacks.
var errMessages = map[int32]string{
	ML_OK:              "",
	ML_ERR_INVALID_ARG: "invalid argument",
	ML_ERR_NOT_FOUND:   "not found",
	ML_ERR_DB:          "database error",
	ML_ERR_CRYPTO:      "crypto error",
	ML_ERR_EXISTS:      "already exists",
	ML_ERR_OVERFLOW:    "overflow",
	ML_ERR_INTERNAL:    "internal error",
}

func codeToError(code int32) error {
	if code == ML_OK {
		return nil
	}

	msg, ok := errMessages[code]
	if !ok {
		return errors.New("unknown error code")
	}
	return errors.New(msg)
}