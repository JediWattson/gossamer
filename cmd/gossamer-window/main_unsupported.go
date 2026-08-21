//go:build !cgo || !darwin || !arm64

package main

import (
	"fmt"
	"os"
)

func main() {
	_, _ = fmt.Fprintln(os.Stderr, "gossamer-window requires macOS on Apple Silicon with cgo")
	os.Exit(2)
}
