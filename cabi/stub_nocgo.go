//go:build !cgo

package main

// Satisfies tooling when CGO is disabled; the real C ABI is built with CGO for Android/iOS.
func main() {}
