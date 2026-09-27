package pyyaml

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// PyYAML's implicit resolvers (yaml/resolver.py), tried in the order it registers them.
var (
	reBool  = regexp.MustCompile(`^(?:yes|Yes|YES|no|No|NO|true|True|TRUE|false|False|FALSE|on|On|ON|off|Off|OFF)$`)
	reFloat = regexp.MustCompile(`^(?:[-+]?(?:[0-9][0-9_]*)\.[0-9_]*(?:[eE][-+][0-9]+)?` +
		`|\.[0-9][0-9_]*(?:[eE][-+][0-9]+)?` +
		`|[-+]?[0-9][0-9_]*(?::[0-5]?[0-9])+\.[0-9_]*` +
		`|[-+]?\.(?:inf|Inf|INF)` +
		`|\.(?:nan|NaN|NAN))$`)
	reInt = regexp.MustCompile(`^(?:[-+]?0b[0-1_]+` +
		`|[-+]?0[0-7_]+` +
		`|[-+]?(?:0|[1-9][0-9_]*)` +
		`|[-+]?0x[0-9a-fA-F_]+` +
		`|[-+]?[1-9][0-9_]*(?::[0-5]?[0-9])+)$`)
	reNull      = regexp.MustCompile(`^(?:~|null|Null|NULL|)$`)
	reTimestamp = regexp.MustCompile(`^(?:[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]` +
		`|[0-9][0-9][0-9][0-9]-[0-9][0-9]?-[0-9][0-9]?(?:[Tt]|[ \t]+)[0-9][0-9]?:[0-9][0-9]:[0-9][0-9](?:\.[0-9]*)?(?:[ \t]*(?:Z|[-+][0-9][0-9]?(?::[0-9][0-9])?))?)$`)
	// The constructor's own pattern, with groups (yaml/constructor.py timestamp_regexp).
	reTimestampParts = regexp.MustCompile(`^([0-9][0-9][0-9][0-9])-([0-9][0-9]?)-([0-9][0-9]?)` +
		`(?:(?:[Tt]|[ \t]+)([0-9][0-9]?):([0-9][0-9]):([0-9][0-9])(?:\.([0-9]*))?` +
		`(?:[ \t]*(Z|([-+])([0-9][0-9]?)(?::([0-9][0-9]))?))?)?$`)
)

// Parse reads one YAML document the way yaml.safe_load does. An empty document is Null.
func Parse(src []byte) (Value, error) {
	dec := yaml.NewDecoder(bytes.NewReader(src))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return Value{Kind: Null}, nil
		}
		return Value{}, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return Value{}, errors.New("expected a single document in the stream, but found another document")
	} else if !errors.Is(err, io.EOF) {
		return Value{}, err
	}
	if len(doc.Content) == 0 {
		return Value{Kind: Null}, nil
	}
	c := constructor{seen: map[*yaml.Node]bool{}}
	return c.construct(doc.Content[0])
}

type constructor struct {
	seen map[*yaml.Node]bool
}

func (c *constructor) construct(n *yaml.Node) (Value, error) {
	if n.Kind == yaml.AliasNode {
		if c.seen[n.Alias] {
			return Value{}, fmt.Errorf("line %d: recursive alias", n.Line)
		}
		c.seen[n.Alias] = true
		defer delete(c.seen, n.Alias)
		return c.construct(n.Alias)
	}
	explicit := n.Style&yaml.TaggedStyle != 0
	switch n.Kind {
	case yaml.ScalarNode:
		if explicit {
			return constructTagged(n.Tag, n.Value, n.Line)
		}
		if n.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
			return NewStr(n.Value), nil
		}
		return resolvePlain(n.Value, n.Line)
	case yaml.SequenceNode:
		if explicit && n.Tag != "!!seq" {
			return Value{}, unsupportedTag(n)
		}
		out := Value{Kind: List, Items: []Value{}}
		for _, child := range n.Content {
			v, err := c.construct(child)
			if err != nil {
				return Value{}, err
			}
			out.Items = append(out.Items, v)
		}
		return out, nil
	case yaml.MappingNode:
		if explicit && n.Tag != "!!map" {
			return Value{}, unsupportedTag(n)
		}
		pairs, err := c.flatten(n)
		if err != nil {
			return Value{}, err
		}
		out := Value{Kind: Map, Keys: []Value{}, Vals: []Value{}}
		for _, p := range pairs {
			k, err := c.construct(p[0])
			if err != nil {
				return Value{}, err
			}
			if k.Kind == List || k.Kind == Map {
				return Value{}, fmt.Errorf("line %d: found unhashable key", p[0].Line)
			}
			v, err := c.construct(p[1])
			if err != nil {
				return Value{}, err
			}
			out.Set(k, v)
		}
		return out, nil
	}
	return Value{}, fmt.Errorf("line %d: unexpected node", n.Line)
}

func unsupportedTag(n *yaml.Node) error {
	return fmt.Errorf("line %d: could not determine a constructor for the tag %s", n.Line, n.Tag)
}

func deref(n *yaml.Node) *yaml.Node {
	for n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

// flatten applies `<<` merge keys the way SafeConstructor.flatten_mapping does:
// merged pairs come first so the mapping's own keys win, and within a list of
// merged mappings the earlier one wins.
func (c *constructor) flatten(n *yaml.Node) ([][2]*yaml.Node, error) {
	var merge, own [][2]*yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Tag == "!!merge" {
			target := deref(v)
			switch target.Kind {
			case yaml.MappingNode:
				sub, err := c.flatten(target)
				if err != nil {
					return nil, err
				}
				merge = append(merge, sub...)
			case yaml.SequenceNode:
				var subs [][][2]*yaml.Node
				for _, item := range target.Content {
					item = deref(item)
					if item.Kind != yaml.MappingNode {
						return nil, fmt.Errorf("line %d: expected a mapping for merging", item.Line)
					}
					sub, err := c.flatten(item)
					if err != nil {
						return nil, err
					}
					subs = append(subs, sub)
				}
				for i := len(subs) - 1; i >= 0; i-- {
					merge = append(merge, subs[i]...)
				}
			default:
				return nil, fmt.Errorf("line %d: expected a mapping or list of mappings for merging", v.Line)
			}
			continue
		}
		own = append(own, [2]*yaml.Node{k, v})
	}
	return append(merge, own...), nil
}

func resolvePlain(s string, line int) (Value, error) {
	switch {
	case reBool.MatchString(s):
		return constructBool(s), nil
	case reFloat.MatchString(s):
		return constructFloat(s), nil
	case reInt.MatchString(s):
		return constructInt(s, line)
	case s == "<<":
		// A merge key in a value position; PyYAML has no constructor for it.
		return Value{}, fmt.Errorf("line %d: could not determine a constructor for the tag 'tag:yaml.org,2002:merge'", line)
	case reNull.MatchString(s):
		return Value{Kind: Null}, nil
	case reTimestamp.MatchString(s):
		return constructTimestamp(s, line)
	case s == "=":
		return Value{}, fmt.Errorf("line %d: could not determine a constructor for the tag 'tag:yaml.org,2002:value'", line)
	}
	return NewStr(s), nil
}

func constructTagged(tag, s string, line int) (Value, error) {
	switch tag {
	case "!!str":
		return NewStr(s), nil
	case "!!null":
		return Value{Kind: Null}, nil
	case "!!bool":
		lower := strings.ToLower(s)
		if lower == "yes" || lower == "true" || lower == "on" || lower == "no" || lower == "false" || lower == "off" {
			return constructBool(s), nil
		}
		return Value{}, fmt.Errorf("line %d: invalid bool %q", line, s)
	case "!!int":
		return constructInt(s, line)
	case "!!float":
		t := strings.ReplaceAll(strings.ToLower(s), "_", "")
		if _, err := strconv.ParseFloat(strings.TrimLeft(t, "+-"), 64); err != nil &&
			!strings.HasSuffix(t, ".inf") && t != ".nan" && !strings.Contains(t, ":") {
			return Value{}, fmt.Errorf("line %d: could not convert string to float: %q", line, s)
		}
		return constructFloat(s), nil
	case "!!timestamp":
		return constructTimestamp(s, line)
	}
	return Value{}, fmt.Errorf("line %d: could not determine a constructor for the tag %s", line, tag)
}

func constructBool(s string) Value {
	switch strings.ToLower(s) {
	case "yes", "true", "on":
		return Value{Kind: Bool, B: true}
	}
	return Value{Kind: Bool, B: false}
}

func constructInt(s string, line int) (Value, error) {
	t := strings.ReplaceAll(s, "_", "")
	neg := false
	if t != "" && (t[0] == '-' || t[0] == '+') {
		neg = t[0] == '-'
		t = t[1:]
	}
	n := new(big.Int)
	var ok bool
	switch {
	case t == "0":
		ok = true
	case strings.HasPrefix(t, "0b"):
		_, ok = n.SetString(t[2:], 2)
	case strings.HasPrefix(t, "0x"):
		_, ok = n.SetString(t[2:], 16)
	case strings.HasPrefix(t, "0"):
		_, ok = n.SetString(t[1:], 8)
	case strings.Contains(t, ":"):
		ok = true
		for _, part := range strings.Split(t, ":") {
			d, err := strconv.ParseInt(part, 10, 64)
			if err != nil {
				ok = false
				break
			}
			n.Mul(n, big.NewInt(60))
			n.Add(n, big.NewInt(d))
		}
	default:
		_, ok = n.SetString(t, 10)
	}
	if !ok {
		return Value{}, fmt.Errorf("line %d: invalid literal for int(): %q", line, s)
	}
	if neg {
		n.Neg(n)
	}
	if n.IsInt64() {
		return Value{Kind: Int, I: n.Int64()}, nil
	}
	return Value{Kind: Int, Big: n.String()}, nil
}

func constructFloat(s string) Value {
	t := strings.ToLower(strings.ReplaceAll(s, "_", ""))
	sign := 1.0
	if t != "" && (t[0] == '-' || t[0] == '+') {
		if t[0] == '-' {
			sign = -1
		}
		t = t[1:]
	}
	switch {
	case t == ".inf":
		return Value{Kind: Float, F: sign * math.Inf(1)}
	case t == ".nan":
		return Value{Kind: Float, F: math.NaN()}
	case strings.Contains(t, ":"):
		parts := strings.Split(t, ":")
		f := 0.0
		for _, p := range parts {
			d, _ := strconv.ParseFloat(p, 64)
			f = f*60 + d
		}
		return Value{Kind: Float, F: sign * f}
	}
	f, _ := strconv.ParseFloat(t, 64)
	return Value{Kind: Float, F: sign * f}
}

func constructTimestamp(s string, line int) (Value, error) {
	m := reTimestampParts.FindStringSubmatch(s)
	if m == nil {
		return Value{}, fmt.Errorf("line %d: invalid timestamp %q", line, s)
	}
	atoi := func(x string) int { n, _ := strconv.Atoi(x); return n }
	y, mo, d := atoi(m[1]), atoi(m[2]), atoi(m[3])
	if y < 1 || mo < 1 || mo > 12 || d < 1 || d > daysIn(y, mo) {
		// PyYAML raises ValueError from datetime.date() here.
		return Value{}, fmt.Errorf("line %d: %q is not a valid date", line, s)
	}
	if m[4] == "" {
		return Value{Kind: Date, T: time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)}, nil
	}
	h, mi, sec := atoi(m[4]), atoi(m[5]), atoi(m[6])
	if h > 23 || mi > 59 || sec > 59 {
		return Value{}, fmt.Errorf("line %d: %q is not a valid time", line, s)
	}
	us := 0
	if frac := m[7]; frac != "" {
		if len(frac) > 6 {
			frac = frac[:6]
		}
		us = atoi(frac + strings.Repeat("0", 6-len(frac)))
	}
	loc, hasTZ := time.UTC, false
	switch {
	case m[9] != "":
		off := atoi(m[10])*3600 + atoi(m[11])*60
		if m[9] == "-" {
			off = -off
		}
		loc, hasTZ = time.FixedZone("", off), true
	case m[8] == "Z":
		hasTZ = true
	}
	return Value{Kind: Datetime, T: time.Date(y, time.Month(mo), d, h, mi, sec, us*1000, loc), HasTZ: hasTZ}, nil
}

func daysIn(y, m int) int {
	return time.Date(y, time.Month(m)+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
