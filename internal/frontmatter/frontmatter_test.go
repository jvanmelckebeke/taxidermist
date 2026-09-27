package frontmatter

import "testing"

func TestBlock(t *testing.T) {
	cases := []struct {
		text, block string
		opened      bool
		err         bool
	}{
		{"---\na: 1\n---\nbody", "a: 1", true, false},
		{"---  \r\na: 1\r\n---\r\nbody", "a: 1", true, false},
		{"---\na: 1\n--- \n", "a: 1", true, false},
		{"---\n---\n", "", true, false},
		{"# title\n---\na: 1\n---\n", "", false, false},
		{"----\na: 1\n----\n", "", false, false},
		{"---\na: 1\nno close", "", true, true},
		{"---\na: x---y\n---\n", "a: x---y", true, false},
		{"---", "", true, true},
	}
	for _, c := range cases {
		block, opened, err := Block(c.text)
		if block != c.block || opened != c.opened || (err != nil) != c.err {
			t.Errorf("Block(%q) = %q, %v, %v; want %q, %v, err=%v", c.text, block, opened, err, c.block, c.opened, c.err)
		}
	}
}
