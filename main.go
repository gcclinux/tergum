// Tergum v3.0 - Encrypted, deduplicated backup system.
// A single binary acts as client, server, or both based on subcommand.
//
// Build with:
//
//	CGO_ENABLED=0 go build -ldflags="-s -w" -o tergum ./
package main

import (
	"os"

	"github.com/ricardopadilha/tergum/cmd"
)

func main() {
	exitCode := cmd.Execute()
	os.Exit(exitCode)
}
