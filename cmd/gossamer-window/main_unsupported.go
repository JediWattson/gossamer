//go:build !v8 || !cgo || !darwin || !arm64

package main

import (
	"fmt"
	"os"
)

func main() {
	_, _ = fmt.Fprintln(os.Stderr, "gossamer-window requires macOS on Apple Silicon, cgo, and the v8 build tag; use tools/v8/window.sh")
	os.Exit(2)
}
