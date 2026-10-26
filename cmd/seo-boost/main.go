package main

import (
	"os"

	"seo-boost/internal/cli"
)

var version = "dev" // diisi lewat: -ldflags "-X main.version=..."

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, version))
}
