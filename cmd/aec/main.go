package main

import (
	"os"

	"github.com/ohnotnow/agent-edit-checker-go/internal/aec"
)

func main() {
	os.Exit(aec.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
