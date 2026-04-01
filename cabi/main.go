//go:build cgo

package main

// Required for -buildmode=c-shared / c-archive; the shared library entry is the exported C API.
func main() {}
