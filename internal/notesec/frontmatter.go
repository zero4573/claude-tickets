package notesec

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zero4573/tix/internal/mdsec"
)

var keyLine = regexp.MustCompile(`^([A-Za-z0-9_][A-Za-z0-9_.-]*):(?:[ \t]+(.*))?$`)

// field is one top-level frontmatter key: its lines [first, last) in the
// split block and the text after the colon.
type field struct {
	key         string
	first, last int
	value       string
}

// fmBlock is the frontmatter as lines (each keeping its ending): the
// opening and closing --- lines are lines[0] and lines[len-1].
type fmBlock struct {
	lines  []string
	fields []field
}

func splitKeep(s string) []string {
	var out []string
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
	return out
}

func trimEOL(l string) string { return strings.TrimRight(l, "\r\n") }

func parseFM(src []byte) *fmBlock {
	d := mdsec.Parse(src)
	if d.Frontmatter == nil {
		return nil
	}
	b := &fmBlock{lines: splitKeep(string(src[d.Frontmatter.Start:d.Frontmatter.End]))}
	n := len(b.lines)
	for i := 1; i < n-1; i++ {
		if m := keyLine.FindStringSubmatch(trimEOL(b.lines[i])); m != nil {
			if k := len(b.fields); k > 0 {
				b.fields[k-1].last = i
			}
			b.fields = append(b.fields, field{key: m[1], first: i, value: strings.TrimSpace(m[2])})
		}
	}
	if k := len(b.fields); k > 0 {
		b.fields[k-1].last = n - 1
	}
	// trailing blank / comment lines belong to no field's value
	for k := range b.fields {
		f := &b.fields[k]
		for f.last-1 > f.first && strings.TrimSpace(b.lines[f.last-1]) == "" {
			f.last--
		}
	}
	return b
}

func (b *fmBlock) find(key string) *field {
	for i := range b.fields {
		if b.fields[i].key == key {
			return &b.fields[i]
		}
	}
	return nil
}

func (b *fmBlock) nl() string {
	if strings.HasSuffix(b.lines[0], "\r\n") {
		return "\r\n"
	}
	return "\n"
}

// raw is the field's value as written: the text after the colon, or the
// block list / multi-line lines.
func (b *fmBlock) raw(f *field) string {
	if f.last-f.first == 1 {
		return f.value
	}
	var parts []string
	if f.value != "" {
		parts = append(parts, f.value)
	}
	for _, l := range b.lines[f.first+1 : f.last] {
		parts = append(parts, trimEOL(l))
	}
	return strings.Join(parts, "\n")
}

// KV is a frontmatter field with its raw value.
type KV struct{ Key, Raw string }

// Fields lists the top-level frontmatter fields (nil without frontmatter).
func Fields(src []byte) []KV {
	b := parseFM(src)
	if b == nil {
		return nil
	}
	var out []KV
	for i := range b.fields {
		out = append(out, KV{b.fields[i].key, b.raw(&b.fields[i])})
	}
	return out
}

// Field is one field's raw value; ok is false when it is missing.
func Field(src []byte, key string) (string, bool) {
	b := parseFM(src)
	if b == nil {
		return "", false
	}
	f := b.find(key)
	if f == nil {
		return "", false
	}
	return b.raw(f), true
}

// FieldsJSON decodes the frontmatter for --json: scalars as strings, lists
// as arrays.
func FieldsJSON(src []byte) (map[string]any, error) {
	out := map[string]any{}
	d := mdsec.Parse(src)
	if d.Frontmatter == nil {
		return out, nil
	}
	block := src[d.Frontmatter.Start:d.Frontmatter.End]
	i := bytes.IndexByte(block, '\n') + 1
	j := bytes.LastIndex(bytes.TrimRight(block, "\r\n"), []byte("\n"))
	if j < i {
		return out, nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(block[i:j+1], &root); err != nil {
		return nil, fmt.Errorf("frontmatter is not valid YAML: %w", err)
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return out, nil
	}
	m := root.Content[0]
	for k := 0; k+1 < len(m.Content); k += 2 {
		out[m.Content[k].Value] = nodeValue(m.Content[k+1])
	}
	return out, nil
}

func nodeValue(n *yaml.Node) any {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag == "!!null" {
			return ""
		}
		return n.Value
	case yaml.SequenceNode:
		l := []any{}
		for _, c := range n.Content {
			l = append(l, nodeValue(c))
		}
		return l
	case yaml.MappingNode:
		m := map[string]any{}
		for k := 0; k+1 < len(n.Content); k += 2 {
			m[n.Content[k].Value] = nodeValue(n.Content[k+1])
		}
		return m
	case yaml.AliasNode:
		return nodeValue(n.Alias)
	}
	return nil
}

// quote makes v safe as a plain YAML scalar, quoting it when it isn't.
func quote(v string) string {
	if v == "" {
		return `""`
	}
	risky := strings.ContainsAny(v[:1], "[{&*!|>'\"%@`#,?") ||
		strings.HasPrefix(v, "- ") || v == "-" ||
		strings.Contains(v, ": ") || strings.Contains(v, " #") ||
		strings.HasSuffix(v, ":") || v != strings.TrimSpace(v) || strings.ContainsAny(v, "\n\r\t")
	if !risky {
		return v
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(v) + `"`
}

func unquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"') {
		var s string
		if err := yaml.Unmarshal([]byte(v), &s); err == nil {
			return s
		}
		return v[1 : len(v)-1]
	}
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	return v
}

func (b *fmBlock) src() []byte { return []byte(strings.Join(b.lines, "")) }

// replace swaps the lines [first,last) for repl (nil inserts at first).
func (b *fmBlock) replace(first, last int, repl []string) {
	lines := append([]string{}, b.lines[:first]...)
	lines = append(lines, repl...)
	b.lines = append(lines, b.lines[last:]...)
}

func edit(src []byte, fn func(b *fmBlock) (changed bool, err error)) ([]byte, error) {
	d := mdsec.Parse(src)
	b := parseFM(src)
	if b == nil {
		nl := nlOf(src)
		b = &fmBlock{lines: []string{"---" + nl, "---" + nl}}
		changed, err := fn(b)
		if err != nil || !changed {
			return src, err
		}
		return append(b.src(), src...), nil
	}
	changed, err := fn(b)
	if err != nil || !changed {
		return src, err
	}
	return splice(src, d.Frontmatter.Start, d.Frontmatter.End, b.src()), nil
}

// insertAt is where a new field goes: before the closing --- line.
func (b *fmBlock) insertAt() int { return len(b.lines) - 1 }

// SetField sets a scalar field, adding it when missing.
func SetField(src []byte, key, value string) ([]byte, error) {
	if err := checkKey(key); err != nil {
		return nil, err
	}
	if strings.ContainsAny(value, "\r\n") {
		return nil, fmt.Errorf("--set takes a one-line value")
	}
	return edit(src, func(b *fmBlock) (bool, error) {
		line := key + ": " + quote(value) + b.nl()
		if f := b.find(key); f != nil {
			if f.last-f.first == 1 && trimEOL(b.lines[f.first]) == trimEOL(line) {
				return false, nil
			}
			b.replace(f.first, f.last, []string{line})
			return true, nil
		}
		b.replace(b.insertAt(), b.insertAt(), []string{line})
		return true, nil
	})
}

// block-list helpers

func blockItems(b *fmBlock, f *field) (idx []int, items []string, indent string) {
	for i := f.first + 1; i < f.last; i++ {
		t := strings.TrimLeft(trimEOL(b.lines[i]), " \t")
		if t == "-" || strings.HasPrefix(t, "- ") {
			if len(idx) == 0 {
				l := trimEOL(b.lines[i])
				indent = l[:len(l)-len(t)]
			}
			idx = append(idx, i)
			items = append(items, unquote(stripTrailingComment(strings.TrimSpace(strings.TrimPrefix(t, "-")))))
		}
	}
	return
}

func stripTrailingComment(v string) string {
	var q byte
	for i := 0; i < len(v); i++ {
		switch c := v[i]; {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '#' && i > 0 && (v[i-1] == ' ' || v[i-1] == '\t'):
			return strings.TrimSpace(v[:i])
		}
	}
	return v
}

func flowItems(v string) (items []string, ok bool) {
	v = strings.TrimSpace(stripTrailingComment(v))
	if len(v) < 2 || v[0] != '[' || v[len(v)-1] != ']' {
		return nil, false
	}
	inner := strings.TrimSpace(v[1 : len(v)-1])
	if inner == "" {
		return nil, true
	}
	var q byte
	start := 0
	for i := 0; i < len(inner); i++ {
		switch c := inner[i]; {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == ',':
			items = append(items, unquote(inner[start:i]))
			start = i + 1
		}
	}
	return append(items, unquote(inner[start:])), true
}

func flowLine(key string, items []string, nl string) string {
	q := make([]string, len(items))
	for i, it := range items {
		q[i] = quote(it)
		if strings.ContainsAny(it, ",]") && q[i] == it {
			q[i] = `"` + it + `"`
		}
	}
	return key + ": [" + strings.Join(q, ", ") + "]" + nl
}

// AddItem adds an item to a list field (flow or block style kept), making
// the field when missing. Adding an item already present changes nothing.
func AddItem(src []byte, key, item string) ([]byte, error) {
	if err := checkKey(key); err != nil {
		return nil, err
	}
	return edit(src, func(b *fmBlock) (bool, error) {
		f := b.find(key)
		if f == nil {
			b.replace(b.insertAt(), b.insertAt(), []string{flowLine(key, []string{item}, b.nl())})
			return true, nil
		}
		if items, ok := flowItems(f.value); ok && f.last-f.first == 1 {
			for _, it := range items {
				if it == item {
					return false, nil
				}
			}
			b.replace(f.first, f.last, []string{flowLine(key, append(items, item), b.nl())})
			return true, nil
		}
		idx, items, indent := blockItems(b, f)
		if f.value != "" {
			return false, fmt.Errorf("field %q is not a list", key)
		}
		for _, it := range items {
			if it == item {
				return false, nil
			}
		}
		line := indent + "- " + quote(item) + b.nl()
		at := f.last
		if len(idx) > 0 {
			at = idx[len(idx)-1] + 1
		} else {
			line = "- " + quote(item) + b.nl()
		}
		b.replace(at, at, []string{line})
		return true, nil
	})
}

// RemoveItem removes an item from a list field; a missing item or field
// changes nothing.
func RemoveItem(src []byte, key, item string) ([]byte, error) {
	if err := checkKey(key); err != nil {
		return nil, err
	}
	return edit(src, func(b *fmBlock) (bool, error) {
		f := b.find(key)
		if f == nil {
			return false, nil
		}
		if items, ok := flowItems(f.value); ok && f.last-f.first == 1 {
			var keep []string
			for _, it := range items {
				if it != item {
					keep = append(keep, it)
				}
			}
			if len(keep) == len(items) {
				return false, nil
			}
			b.replace(f.first, f.last, []string{flowLine(key, keep, b.nl())})
			return true, nil
		}
		idx, items, _ := blockItems(b, f)
		if f.value != "" {
			return false, fmt.Errorf("field %q is not a list", key)
		}
		changed := false
		for k := len(idx) - 1; k >= 0; k-- {
			if items[k] == item {
				b.replace(idx[k], idx[k]+1, nil)
				changed = true
			}
		}
		return changed, nil
	})
}

// ListField is the items of a list field (flow or block), or a scalar as
// its one item; nil when missing.
func ListField(src []byte, key string) []string {
	b := parseFM(src)
	if b == nil {
		return nil
	}
	f := b.find(key)
	if f == nil {
		return nil
	}
	if items, ok := flowItems(f.value); ok {
		return items
	}
	if f.value != "" {
		return []string{unquote(stripTrailingComment(f.value))}
	}
	_, items, _ := blockItems(b, f)
	return items
}

// UsageError is a bad argument (tix exits 2).
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

var keyOnly = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

// checkKey rejects a field key that is not a plain YAML key, so a key can't
// smuggle a newline, a colon or padding into the written line.
func checkKey(key string) error {
	if !keyOnly.MatchString(key) {
		return &UsageError{fmt.Sprintf("invalid frontmatter field name %q", key)}
	}
	return nil
}
