// Package xmltree turns XML into the maps, lists and strings that JSON
// decodes to, so API responses in either format bind the same way, and
// writes such values back out as XML.
//
// An element becomes a key named after its local name (namespace prefixes
// are dropped). An element with only text becomes a string; one with
// attributes or children becomes a map, where attributes are "@name" keys
// and its own text, if any, is "#text". Siblings with the same name become
// a list. Values stay strings: XML does not say what is a number.
package xmltree

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

const maxDepth = 256

// Decode reads one XML document and returns {"rootName": value}.
func Decode(r io.Reader) (map[string]any, error) {
	dec := xml.NewDecoder(r)
	dec.Entity = xml.HTMLEntity
	// Documents in other encodings are passed through as they are; most
	// APIs send UTF-8.
	dec.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil }
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil, errors.New("the XML document is empty")
		}
		if err != nil {
			return nil, err
		}
		if start, ok := tok.(xml.StartElement); ok {
			v, err := element(dec, start, 1)
			if err != nil {
				return nil, err
			}
			return map[string]any{start.Name.Local: v}, nil
		}
	}
}

// element reads the rest of an element whose start tag was just read.
func element(dec *xml.Decoder, start xml.StartElement, depth int) (any, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("the XML nests more than %d elements deep", maxDepth)
	}
	m := map[string]any{}
	for _, a := range start.Attr {
		if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
			continue
		}
		m["@"+a.Name.Local] = a.Value
	}
	// lists marks names already seen twice, whose value is a list.
	lists := map[string]bool{}
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return nil, fmt.Errorf("the element <%s> is not closed", start.Name.Local)
			}
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			v, err := element(dec, t, depth+1)
			if err != nil {
				return nil, err
			}
			name := t.Name.Local
			prev, seen := m[name]
			switch {
			case !seen:
				m[name] = v
			case lists[name]:
				m[name] = append(prev.([]any), v)
			default:
				m[name] = []any{prev, v}
				lists[name] = true
			}
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			s := strings.TrimSpace(text.String())
			if len(m) == 0 {
				return s, nil
			}
			if s != "" {
				m["#text"] = s
			}
			return m, nil
		}
	}
}

// Raw is XML that Encode and the XML body writer insert as it is.
type Raw string

// Encode writes a value as XML. name wraps the value in an element; with
// no name, a map's keys become the top-level elements.
func Encode(name string, v any) string {
	var b bytes.Buffer
	if name == "" {
		if m, ok := v.(map[string]any); ok {
			children(&b, m)
			return b.String()
		}
		name = "value"
	}
	write(&b, name, v)
	return b.String()
}

func write(b *bytes.Buffer, name string, v any) {
	name = elementName(name)
	switch v := v.(type) {
	case []any:
		for _, item := range v {
			write(b, name, item)
		}
	case map[string]any:
		b.WriteString("<" + name)
		keys := sortedKeys(v)
		for _, k := range keys {
			if strings.HasPrefix(k, "@") {
				b.WriteString(" " + elementName(k[1:]) + `="`)
				xml.EscapeText(b, []byte(Text(v[k])))
				b.WriteString(`"`)
			}
		}
		b.WriteString(">")
		if t, ok := v["#text"]; ok {
			xml.EscapeText(b, []byte(Text(t)))
		}
		children(b, v)
		b.WriteString("</" + name + ">")
	case nil:
		b.WriteString("<" + name + "/>")
	default:
		b.WriteString("<" + name + ">")
		if r, ok := v.(Raw); ok {
			b.WriteString(string(r))
		} else {
			xml.EscapeText(b, []byte(Text(v)))
		}
		b.WriteString("</" + name + ">")
	}
}

func children(b *bytes.Buffer, m map[string]any) {
	for _, k := range sortedKeys(m) {
		if !strings.HasPrefix(k, "@") && k != "#text" {
			write(b, k, m[k])
		}
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// elementName makes a key usable as an element or attribute name.
func elementName(s string) string {
	var b strings.Builder
	for i, r := range s {
		ok := r == '_' || r == ':' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r > 0x7f ||
			i > 0 && (r == '-' || r == '.' || r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}

// Text formats a scalar for XML or a URL.
func Text(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case Raw:
		return string(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(v)
	}
}

// Check reports whether s is well-formed XML, for request bodies.
func Check(s string) error {
	dec := xml.NewDecoder(strings.NewReader(s))
	dec.Entity = xml.HTMLEntity
	depth, roots := 0, 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			if roots == 0 {
				return errors.New("there is no root element")
			}
			return nil
		}
		if err != nil {
			return err
		}
		switch tok.(type) {
		case xml.StartElement:
			if depth == 0 {
				if roots++; roots > 1 {
					return errors.New("an XML document has one root element")
				}
			}
			depth++
		case xml.EndElement:
			depth--
		}
	}
}
