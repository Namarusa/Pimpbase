package email

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/pimpbase/pimpbase/internal/interp"
)

// Template is an email an app (or the workspace) sends: a subject and a
// body built from blocks or written as HTML, with a plain-text fallback.
// Its texts hold {{ }} bindings over its parameters, which a flow's Send
// email step fills, and over app.name and app.slug.
type Template struct {
	ID    string `json:"id"`
	AppID string `json:"appId"`
	Name  string `json:"name"`
	// Key names a system email: invite, password_reset or sign_in_link.
	Key         string `json:"key,omitempty"`
	Description string `json:"description"`
	Subject     string `json:"subject"`
	// Preheader is the preview line mail apps show after the subject.
	Preheader string `json:"preheader"`
	// Mode is "blocks" (the visual editor) or "html".
	Mode   string  `json:"mode"`
	Blocks []Block `json:"blocks"`
	Style  Style   `json:"style"`
	HTML   string  `json:"html"`
	// Text is the plain-text version; empty makes it from the content.
	Text   string  `json:"text"`
	Params []Param `json:"params"`

	Version   int       `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	UpdatedBy string    `json:"updatedBy"`
}

// Block is one part of a template's body.
type Block struct {
	ID string `json:"id"`
	// Type is heading, text, button, image, divider, spacer, table or html.
	Type string `json:"type"`
	// Text is a heading's or text's words (text blocks take **bold**,
	// *italic*, [links](https://…) and "- " lists), a button's label, or
	// the HTML of an html block.
	Text  string `json:"text,omitempty"`
	Level int    `json:"level,omitempty"`
	// URL is where a button goes, or an image's address.
	URL string `json:"url,omitempty"`
	// Link makes an image a link.
	Link  string `json:"link,omitempty"`
	Alt   string `json:"alt,omitempty"`
	Width int    `json:"width,omitempty"`
	// Align is left, center or right.
	Align string `json:"align,omitempty"`
	// Color is a button's color; empty uses the brand color.
	Color  string `json:"color,omitempty"`
	Height int    `json:"height,omitempty"`
	// Items is the list a table repeats a row for, such as {{ lines }};
	// columns read the current item as {{ loop.item.Name }}.
	Items   string   `json:"items,omitempty"`
	Columns []Column `json:"columns,omitempty"`
	// ShowWhen, when set, shows the block only while it is true, such as
	// {{ order.Paid }}.
	ShowWhen string `json:"showWhen,omitempty"`
}

// Column is one column of a table block.
type Column struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Align string `json:"align,omitempty"`
}

// Style is the look of a blocks template.
type Style struct {
	Brand      string `json:"brand,omitempty"`
	Background string `json:"background,omitempty"`
	Panel      string `json:"panel,omitempty"`
	TextColor  string `json:"textColor,omitempty"`
	// Font is sans (the default) or serif.
	Font      string `json:"font,omitempty"`
	Logo      string `json:"logo,omitempty"`
	LogoWidth int    `json:"logoWidth,omitempty"`
	// Footer is small text under the panel, such as the address.
	Footer string `json:"footer,omitempty"`
	Radius *int   `json:"radius,omitempty"`
}

// Param is a value a template reads, filled by whoever sends it. Sample
// is used for previews.
type Param struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // text, number, boolean, list, object
	Description string `json:"description,omitempty"`
	Sample      any    `json:"sample,omitempty"`
}

var (
	BlockTypes = []string{"heading", "text", "button", "image", "divider", "spacer", "table", "html"}
	identRe    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)
	colorRe    = regexp.MustCompile(`^#[0-9a-fA-F]{3}([0-9a-fA-F]{3})?$`)
	reserved   = map[string]bool{"app": true, "loop": true, "item": true, "index": true, "number": true}
)

const (
	maxBlocks = 200
	maxParams = 50
	maxSize   = 512 << 10
)

// InvalidError messages are safe to show.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &InvalidError{fmt.Sprintf(format, args...)}
}

// Check validates a template and fills in defaults.
func (t *Template) Check() error {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" || len(t.Name) > 80 {
		return invalid("a template needs a name of up to 80 characters")
	}
	if t.Mode == "" {
		t.Mode = "blocks"
	}
	if t.Mode != "blocks" && t.Mode != "html" {
		return invalid("a template is made of blocks or written as HTML")
	}
	if t.Blocks == nil {
		t.Blocks = []Block{}
	}
	if t.Params == nil {
		t.Params = []Param{}
	}
	if len(t.Blocks) > maxBlocks {
		return invalid("a template has at most %d blocks", maxBlocks)
	}
	if len(t.Params) > maxParams {
		return invalid("a template has at most %d fields", maxParams)
	}
	text := func(where, src string) error {
		if err := interp.CheckBlocks(src); err != nil {
			return invalid("%s: %s", where, err)
		}
		return nil
	}
	for _, f := range []struct{ where, src string }{{"the subject", t.Subject}, {"the preview line", t.Preheader}, {"the HTML", t.HTML}, {"the plain text", t.Text}, {"the footer", t.Style.Footer}} {
		if err := text(f.where, f.src); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for i := range t.Blocks {
		b := &t.Blocks[i]
		known := false
		for _, k := range BlockTypes {
			known = known || k == b.Type
		}
		if !known {
			return invalid("the block type %q is unknown", b.Type)
		}
		if b.ID == "" || seen[b.ID] {
			b.ID = fmt.Sprintf("b%d", i+1)
			for seen[b.ID] {
				b.ID += "x"
			}
		}
		seen[b.ID] = true
		where := fmt.Sprintf("block %d (%s)", i+1, b.Type)
		for _, src := range []string{b.Text, b.URL, b.Link, b.Alt, b.Items, b.ShowWhen} {
			if err := text(where, src); err != nil {
				return err
			}
		}
		for _, c := range b.Columns {
			if err := text(where, c.Value); err != nil {
				return err
			}
		}
		if b.Color != "" && !colorRe.MatchString(b.Color) {
			return invalid("%s: the color is a hex value such as #2563eb", where)
		}
		for _, u := range []string{b.URL, b.Link} {
			if !safeURL(u) {
				return invalid("%s: addresses start with https://, http://, mailto: or tel:, or are a {{ binding }}", where)
			}
		}
		switch b.Align {
		case "", "left", "center", "right":
		default:
			return invalid("%s: align is left, center or right", where)
		}
		if b.Type == "table" && len(b.Columns) > 8 {
			return invalid("%s: a table has at most 8 columns", where)
		}
	}
	for _, c := range []string{t.Style.Brand, t.Style.Background, t.Style.Panel, t.Style.TextColor} {
		if c != "" && !colorRe.MatchString(c) {
			return invalid("colors are hex values such as #2563eb")
		}
	}
	if !safeURL(t.Style.Logo) {
		return invalid("the logo address starts with https:// or http://, or is a {{ binding }}")
	}
	names := map[string]bool{}
	for i := range t.Params {
		p := &t.Params[i]
		p.Name = strings.TrimSpace(p.Name)
		if !identRe.MatchString(p.Name) {
			return invalid("field names use letters, digits and underscores, and start with a letter, such as customer")
		}
		if reserved[p.Name] {
			return invalid("%s is a name templates use already; choose another field name", p.Name)
		}
		if names[strings.ToLower(p.Name)] {
			return invalid("the field %s is listed twice", p.Name)
		}
		names[strings.ToLower(p.Name)] = true
		switch p.Type {
		case "":
			p.Type = "text"
		case "text", "number", "boolean", "list", "object":
		default:
			return invalid("a field is text, number, boolean, list or object")
		}
	}
	return nil
}

// safeURL allows web, mail and phone addresses, page anchors, and
// addresses that start with a binding.
func safeURL(u string) bool {
	u = strings.TrimSpace(u)
	if u == "" || strings.HasPrefix(u, "{{") || strings.HasPrefix(u, "#") {
		return true
	}
	p, err := url.Parse(u)
	if err != nil {
		return false
	}
	switch strings.ToLower(p.Scheme) {
	case "http", "https", "mailto", "tel":
		return true
	}
	return false
}

// SampleData is the template's parameters filled with their samples.
func (t *Template) SampleData() map[string]any {
	out := map[string]any{}
	for _, p := range t.Params {
		if p.Sample != nil {
			out[p.Name] = p.Sample
		}
	}
	return out
}
