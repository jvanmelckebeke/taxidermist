// Command pyyaml-parity prints each file's frontmatter as internal/pyyaml parses it,
// in the same canonical form as canon.py, so the two outputs can be diffed.
//
//	go run ./scripts/pyyaml-parity FILE [FILE ...]
//	go run ./scripts/pyyaml-parity --scalars CASES
package main

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/jvanmelckebeke/taxidermist/internal/frontmatter"
	"github.com/jvanmelckebeke/taxidermist/internal/pyyaml"
)

func load(src string) string {
	v, err := pyyaml.Parse([]byte(src))
	if err != nil {
		return "ERROR"
	}
	return pyyaml.Canon(v)
}

func main() {
	args := os.Args[1:]
	if len(args) == 2 && args[0] == "--scalars" {
		raw, err := os.ReadFile(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
			fmt.Printf("%s\t%s\n", line, load("v: "+line))
		}
		return
	}
	for _, path := range args {
		raw, err := os.ReadFile(path)
		if err != nil || !utf8.Valid(raw) {
			fmt.Printf("%s\tnone\n", path)
			continue
		}
		block, opened, err := frontmatter.Block(string(raw))
		switch {
		case !opened:
			fmt.Printf("%s\tnone\n", path)
		case err != nil:
			fmt.Printf("%s\tERROR\n", path)
		default:
			fmt.Printf("%s\t%s\n", path, load(block))
		}
	}
}
