//go:build !linux

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "statix-agent runs on Linux only; this build exists for development.")
	os.Exit(1)
}
