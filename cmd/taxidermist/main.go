// Command taxidermist keeps markdown frontmatter to a declared schema.
package main

import (
	"os"

	"github.com/jvanmelckebeke/taxidermist/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
