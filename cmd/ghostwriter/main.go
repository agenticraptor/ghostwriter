// Command ghostwriter narrates the changes your AI coding agent made and lets
// you accept or reject each one before it lands.
// See https://github.com/agenticraptor/ghostwriter.
package main

import (
	"os"

	"github.com/agenticraptor/ghostwriter/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
