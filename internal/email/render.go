package email

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/pimpbase/pimpbase/internal/interp"
)

// Rendered is a template filled with values.
type Rendered struct {
	Subject string `json:"subject"`
	// Preheader is the preview line, as text.
	Preheader string `json:"preheader,omitempty"`
	HTML      string `json:"html"`
	Text      string `json:"text"`
	// Warnings point at fields that were left empty.
	Warnings []string `json:"warnings,omitempty"`
}

// Render fills a template. data holds its parameters; app holds the app's
// name and slug. Bound values are escaped for HTML; the raw pipe skips
// that.
func (t *Template) Render(data map[string]any, app map[string]any) (*Rendered, error) {
	return t.render(data, app, false)
}

// RenderMarked renders for the editor: each block is wrapped in an element
// with data-pb-block set to its id, so a click on the preview finds it.
func (t *Template) RenderMarked(data map[string]any, app map[string]any) (*Rendered, error) {
	return t.render(data, app, true)
}

func (t *Template) render(data map[string]any, app map[string]any, mark bool) (*Rendered, error) {
	names := map[string]any{}
	for k, v := range data {
		names[k] = v
	}
	if app == nil {
		app = map[string]any{}
	}
	names["app"] = app
	sc := interp.Map(names)
	out := &Rendered{}
	for _, p := range t.Params {
		if v, ok := data[p.Name]; !ok || v == nil || v == "" {
			out.Warnings = append(out.Warnings, fmt.Sprintf("%s is empty", p.Name))
		}
	}
	subject, err := interp.RenderBlocks(t.Subject, interp.Text, sc)
	if err != nil {
		return nil, fmt.Errorf("the subject: %w", err)
	}
	out.Subject = OneLine(subject)
	if t.Preheader != "" {
		pre, err := interp.RenderBlocks(t.Preheader, interp.Text, sc)
		if err != nil {
			return nil, fmt.Errorf("the preview line: %w", err)
		}
		out.Preheader = OneLine(pre)
	}
	r := &renderer{t: t, sc: sc, mark: mark}
	if t.Mode == "html" {
		if out.HTML, err = interp.RenderBlocks(t.HTML, interp.HTML, sc); err != nil {
			return nil, fmt.Errorf("the HTML: %w", err)
		}
	} else if out.HTML, err = r.html(); err != nil {
		return nil, err
	}
	switch {
	case strings.TrimSpace(t.Text) != "":
		if out.Text, err = interp.RenderBlocks(t.Text, interp.Text, sc); err != nil {
			return nil, fmt.Errorf("the plain text: %w", err)
		}
	case t.Mode == "html":
		out.Text = HTMLToText(out.HTML)
	default:
		if out.Text, err = r.text(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

type renderer struct {
	t    *Template
	sc   interp.Scope
	mark bool
}

func (r *renderer) style() (brand, bg, panel, fg, muted, font string, radius int) {
	s := r.t.Style
	brand, bg, panel, fg = or(s.Brand, "#2563eb"), or(s.Background, "#f4f4f5"), or(s.Panel, "#ffffff"), or(s.TextColor, "#18181b")
	muted = "#71717a"
	font = `-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif`
	if s.Font == "serif" {
		font = `Georgia,'Times New Roman',Times,serif`
	}
	radius = 8
	if s.Radius != nil {
		radius = max(0, min(*s.Radius, 40))
	}
	return
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// shown tells whether a block's Show when holds.
func (r *renderer) shown(b *Block, sc interp.Scope) (bool, error) {
	src := strings.TrimSpace(b.ShowWhen)
	if src == "" {
		return true, nil
	}
	if !strings.Contains(src, "{{") {
		src = "{{ " + src + " }}"
	}
	v, err := interp.BlocksValue(src, sc)
	if err != nil {
		return false, err
	}
	return interp.Truthy(v), nil
}

func (r *renderer) attr(src string, sc interp.Scope) (string, error) {
	return interp.RenderBlocks(strings.TrimSpace(src), interp.HTML, sc)
}

func (r *renderer) html() (string, error) {
	brand, bg, panel, fg, muted, font, radius := r.style()
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="light only"><meta name="supported-color-schemes" content="light">`)
	b.WriteString(`<style>@media (max-width:620px){.pb-pad{padding:24px 20px!important}}a{color:` + brand + `}</style></head>`)
	fmt.Fprintf(&b, `<body style="margin:0;padding:0;background:%s;-webkit-text-size-adjust:100%%">`, bg)
	if r.t.Preheader != "" {
		pre, err := interp.RenderBlocks(r.t.Preheader, interp.HTML, r.sc)
		if err != nil {
			return "", fmt.Errorf("the preview line: %w", err)
		}
		fmt.Fprintf(&b, `<div style="display:none;max-height:0;overflow:hidden;opacity:0">%s%s</div>`, pre, strings.Repeat("&#8199;&#847; ", 30))
	}
	fmt.Fprintf(&b, `<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="background:%s"><tr><td align="center" style="padding:32px 12px">`, bg)
	if r.t.Style.Logo != "" {
		src, err := r.attr(r.t.Style.Logo, r.sc)
		if err != nil {
			return "", fmt.Errorf("the logo: %w", err)
		}
		w := r.t.Style.LogoWidth
		if w <= 0 {
			w = 120
		}
		fmt.Fprintf(&b, `<table role="presentation" width="600" cellpadding="0" cellspacing="0" border="0" style="max-width:600px;width:100%%"><tr><td style="padding:0 4px 20px"><img src="%s" width="%d" alt="" style="display:block;border:0;max-width:100%%;height:auto"></td></tr></table>`, src, w)
	}
	fmt.Fprintf(&b, `<table role="presentation" width="600" cellpadding="0" cellspacing="0" border="0" style="max-width:600px;width:100%%;background:%s;border-radius:12px">`, panel)
	fmt.Fprintf(&b, `<tr><td class="pb-pad" style="padding:36px 40px;font-family:%s;font-size:15px;line-height:1.6;color:%s">`, font, fg)
	for i := range r.t.Blocks {
		bl := &r.t.Blocks[i]
		ok, err := r.shown(bl, r.sc)
		if err != nil {
			return "", fmt.Errorf("block %d's show when: %w", i+1, err)
		}
		if !ok {
			continue
		}
		if r.mark {
			fmt.Fprintf(&b, `<div data-pb-block="%s">`, html.EscapeString(bl.ID))
		}
		if err := r.block(&b, bl, brand, fg, muted, font, radius); err != nil {
			return "", fmt.Errorf("block %d (%s): %w", i+1, bl.Type, err)
		}
		if r.mark {
			b.WriteString(`</div>`)
		}
	}
	b.WriteString(`</td></tr></table>`)
	if r.t.Style.Footer != "" {
		foot, err := r.prose(r.t.Style.Footer, r.sc, brand, "margin:0 0 6px")
		if err != nil {
			return "", fmt.Errorf("the footer: %w", err)
		}
		fmt.Fprintf(&b, `<table role="presentation" width="600" cellpadding="0" cellspacing="0" border="0" style="max-width:600px;width:100%%"><tr><td style="padding:20px 24px 0;font-family:%s;font-size:12px;line-height:1.5;color:%s;text-align:center">%s</td></tr></table>`, font, muted, foot)
	}
	b.WriteString(`</td></tr></table></body></html>`)
	return b.String(), nil
}

func align(a string) string {
	if a == "" {
		return "left"
	}
	return a
}

func (r *renderer) block(b *strings.Builder, bl *Block, brand, fg, muted, font string, radius int) error {
	sc := r.sc
	al := align(bl.Align)
	switch bl.Type {
	case "heading":
		txt, err := r.inline(bl.Text, sc, brand)
		if err != nil {
			return err
		}
		size := map[int]int{1: 26, 2: 20, 3: 17}[bl.Level]
		if size == 0 {
			size = 26
		}
		lvl := max(1, min(bl.Level, 3))
		fmt.Fprintf(b, `<h%d style="margin:0 0 14px;font-size:%dpx;line-height:1.3;font-weight:700;color:%s;text-align:%s">%s</h%d>`, lvl, size, fg, al, txt, lvl)
	case "text":
		txt, err := r.prose(bl.Text, sc, brand, "margin:0 0 14px;text-align:"+al)
		if err != nil {
			return err
		}
		b.WriteString(txt)
	case "button":
		label, err := r.inline(bl.Text, sc, brand)
		if err != nil {
			return err
		}
		href, err := r.attr(bl.URL, sc)
		if err != nil {
			return err
		}
		color := or(bl.Color, brand)
		fmt.Fprintf(b, `<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="margin:6px 0 20px"><tr><td align="%s"><table role="presentation" cellpadding="0" cellspacing="0" border="0"><tr><td style="border-radius:%dpx;background:%s"><a href="%s" target="_blank" style="display:inline-block;padding:12px 24px;font-family:%s;font-size:15px;font-weight:600;line-height:1.2;color:#ffffff;text-decoration:none;border-radius:%dpx">%s</a></td></tr></table></td></tr></table>`,
			al, radius, color, href, font, radius, label)
	case "image":
		src, err := r.attr(bl.URL, sc)
		if err != nil {
			return err
		}
		if src == "" {
			return nil
		}
		alt, err := r.attr(bl.Alt, sc)
		if err != nil {
			return err
		}
		width := ""
		if bl.Width > 0 {
			width = fmt.Sprintf(` width="%d"`, min(bl.Width, 520))
		}
		img := fmt.Sprintf(`<img src="%s" alt="%s"%s style="display:inline-block;border:0;max-width:100%%;height:auto;border-radius:6px">`, src, alt, width)
		if bl.Link != "" {
			href, err := r.attr(bl.Link, sc)
			if err != nil {
				return err
			}
			img = fmt.Sprintf(`<a href="%s" target="_blank">%s</a>`, href, img)
		}
		fmt.Fprintf(b, `<div style="margin:0 0 16px;text-align:%s">%s</div>`, al, img)
	case "divider":
		b.WriteString(`<hr style="border:0;border-top:1px solid #e4e4e7;margin:22px 0">`)
	case "spacer":
		h := bl.Height
		if h <= 0 {
			h = 16
		}
		h = min(h, 200)
		fmt.Fprintf(b, `<div style="height:%dpx;line-height:%dpx;font-size:1px">&nbsp;</div>`, h, h)
	case "table":
		return r.table(b, bl, fg, muted)
	case "html":
		out, err := interp.RenderBlocks(bl.Text, interp.HTML, sc)
		if err != nil {
			return err
		}
		b.WriteString(out)
	}
	return nil
}

// rows is a table block's items with the scope of each.
func (r *renderer) rows(bl *Block) ([]interp.Scope, error) {
	src := strings.TrimSpace(bl.Items)
	if src == "" {
		return nil, nil
	}
	if !strings.Contains(src, "{{") {
		src = "{{ " + src + " }}"
	}
	v, err := interp.BlocksValue(src, r.sc)
	if err != nil {
		return nil, err
	}
	list := interp.AsList(v)
	if len(list) > 500 {
		list = list[:500]
	}
	out := make([]interp.Scope, len(list))
	for i, item := range list {
		out[i] = interp.Overlay{Names: map[string]any{"item": item, "index": float64(i), "number": float64(i + 1),
			"loop": map[string]any{"item": item, "index": float64(i), "number": float64(i + 1), "count": float64(len(list))}}, Base: r.sc}
	}
	return out, nil
}

func (r *renderer) table(b *strings.Builder, bl *Block, fg, muted string) error {
	rows, err := r.rows(bl)
	if err != nil {
		return err
	}
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="border-collapse:collapse;margin:4px 0 20px;font-size:14px">`)
	b.WriteString(`<tr>`)
	for _, c := range bl.Columns {
		fmt.Fprintf(b, `<th style="padding:8px 6px;border-bottom:2px solid #e4e4e7;text-align:%s;font-weight:600;font-size:12px;color:%s;text-transform:uppercase;letter-spacing:.04em">%s</th>`, align(c.Align), muted, html.EscapeString(c.Label))
	}
	b.WriteString(`</tr>`)
	for _, sc := range rows {
		b.WriteString(`<tr>`)
		for _, c := range bl.Columns {
			v, err := interp.RenderBlocks(c.Value, interp.HTML, sc)
			if err != nil {
				return fmt.Errorf("column %s: %w", c.Label, err)
			}
			fmt.Fprintf(b, `<td style="padding:9px 6px;border-bottom:1px solid #f0f0f2;text-align:%s;color:%s">%s</td>`, align(c.Align), fg, v)
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</table>`)
	return nil
}

var (
	bindingRe = regexp.MustCompile(`\{\{[\s\S]*?\}\}`)
	holderRe  = regexp.MustCompile("\x00(\\d+)\x00")
	boldRe    = regexp.MustCompile(`\*\*([^*\n]+?)\*\*`)
	italicRe  = regexp.MustCompile(`\*([^*\n]+?)\*`)
	linkRe    = regexp.MustCompile(`\[([^\]\n]+)\]\(([^)\s]+)\)`)
)

// protect swaps bindings for placeholders, so the words around them can be
// escaped and formatted; restore puts them back.
func protect(src string) (string, []string) {
	var saved []string
	out := bindingRe.ReplaceAllStringFunc(src, func(m string) string {
		saved = append(saved, m)
		return "\x00" + strconv.Itoa(len(saved)-1) + "\x00"
	})
	return out, saved
}

func restore(s string, saved []string) string {
	return holderRe.ReplaceAllStringFunc(s, func(m string) string {
		i, _ := strconv.Atoi(strings.Trim(m, "\x00"))
		if i < len(saved) {
			return saved[i]
		}
		return ""
	})
}

// formatInline escapes words and turns **bold**, *italic* and [links](…)
// into HTML.
func formatInline(s, brand string) string {
	s = html.EscapeString(s)
	s = linkRe.ReplaceAllStringFunc(s, func(m string) string {
		p := linkRe.FindStringSubmatch(m)
		href := html.UnescapeString(p[2])
		if !holderRe.MatchString(href) && !safeURL(href) {
			return m
		}
		return fmt.Sprintf(`<a href="%s" target="_blank" style="color:%s;text-decoration:underline">%s</a>`, html.EscapeString(href), brand, p[1])
	})
	s = boldRe.ReplaceAllString(s, "<strong>$1</strong>")
	s = italicRe.ReplaceAllString(s, "<em>$1</em>")
	return s
}

// inline renders one line of words with formatting and bindings.
func (r *renderer) inline(src string, sc interp.Scope, brand string) (string, error) {
	p, saved := protect(src)
	return interp.RenderBlocks(restore(strings.ReplaceAll(formatInline(p, brand), "\n", "<br>"), saved), interp.HTML, sc)
}

// prose renders paragraphs: blank lines part them, "- " lines make lists.
func (r *renderer) prose(src string, sc interp.Scope, brand, pStyle string) (string, error) {
	p, saved := protect(src)
	var b strings.Builder
	for _, para := range regexp.MustCompile(`\n\s*\n`).Split(strings.TrimSpace(p), -1) {
		lines := strings.Split(para, "\n")
		list := true
		for _, l := range lines {
			l = strings.TrimSpace(l)
			list = list && (strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "* "))
		}
		if list && len(lines) > 0 {
			b.WriteString(`<ul style="margin:0 0 14px;padding-left:22px">`)
			for _, l := range lines {
				fmt.Fprintf(&b, `<li style="margin:0 0 4px">%s</li>`, formatInline(strings.TrimSpace(l)[2:], brand))
			}
			b.WriteString(`</ul>`)
			continue
		}
		for i := range lines {
			lines[i] = formatInline(lines[i], brand)
		}
		fmt.Fprintf(&b, `<p style="%s">%s</p>`, pStyle, strings.Join(lines, "<br>"))
	}
	return interp.RenderBlocks(restore(b.String(), saved), interp.HTML, sc)
}

// text makes the plain-text version of a blocks template.
func (r *renderer) text() (string, error) {
	var b strings.Builder
	plain := func(src string, sc interp.Scope) (string, error) {
		s, err := interp.RenderBlocks(src, interp.Text, sc)
		if err != nil {
			return "", err
		}
		s = linkRe.ReplaceAllString(s, "$1 ($2)")
		s = boldRe.ReplaceAllString(s, "$1")
		return italicRe.ReplaceAllString(s, "$1"), nil
	}
	for i := range r.t.Blocks {
		bl := &r.t.Blocks[i]
		ok, err := r.shown(bl, r.sc)
		if err != nil {
			return "", err
		}
		if !ok {
			continue
		}
		switch bl.Type {
		case "heading", "text":
			s, err := plain(bl.Text, r.sc)
			if err != nil {
				return "", err
			}
			b.WriteString(strings.TrimSpace(s) + "\n\n")
		case "button":
			label, err := plain(bl.Text, r.sc)
			if err != nil {
				return "", err
			}
			href, err := interp.RenderBlocks(bl.URL, interp.Text, r.sc)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&b, "%s: %s\n\n", strings.TrimSpace(label), strings.TrimSpace(href))
		case "divider":
			b.WriteString("----------\n\n")
		case "table":
			rows, err := r.rows(bl)
			if err != nil {
				return "", err
			}
			var head []string
			for _, c := range bl.Columns {
				head = append(head, c.Label)
			}
			b.WriteString(strings.Join(head, " | ") + "\n")
			for _, sc := range rows {
				var cells []string
				for _, c := range bl.Columns {
					v, err := interp.RenderBlocks(c.Value, interp.Text, sc)
					if err != nil {
						return "", err
					}
					cells = append(cells, OneLine(v))
				}
				b.WriteString(strings.Join(cells, " | ") + "\n")
			}
			b.WriteString("\n")
		case "html":
			s, err := interp.RenderBlocks(bl.Text, interp.HTML, r.sc)
			if err != nil {
				return "", err
			}
			if t := HTMLToText(s); t != "" {
				b.WriteString(t + "\n\n")
			}
		}
	}
	if r.t.Style.Footer != "" {
		s, err := plain(r.t.Style.Footer, r.sc)
		if err != nil {
			return "", err
		}
		b.WriteString("--\n" + strings.TrimSpace(s) + "\n")
	}
	return strings.TrimSpace(b.String()) + "\n", nil
}

var (
	dropRe   = regexp.MustCompile(`(?is)<(head|style|script)[^>]*>.*?</(head|style|script)>`)
	hiddenRe = regexp.MustCompile(`(?is)<div[^>]*display:\s*none[^>]*>.*?</div>`)
	aRe      = regexp.MustCompile(`(?is)<a\s[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
	breakRe  = regexp.MustCompile(`(?i)<br\s*/?>|</(p|h[1-6]|tr|li|div|table|ul|ol)>`)
	cellRe   = regexp.MustCompile(`(?i)</t[dh]>`)
	liRe     = regexp.MustCompile(`(?i)<li[^>]*>`)
	tagRe    = regexp.MustCompile(`(?s)<[^>]+>`)
	blankRe  = regexp.MustCompile(`\n[ \t]*\n(\s*\n)+`)
)

// HTMLToText makes a readable plain-text version of an HTML mail.
func HTMLToText(h string) string {
	s := dropRe.ReplaceAllString(h, "")
	s = hiddenRe.ReplaceAllString(s, "")
	s = aRe.ReplaceAllStringFunc(s, func(m string) string {
		p := aRe.FindStringSubmatch(m)
		label := strings.TrimSpace(tagRe.ReplaceAllString(p[2], ""))
		href := html.UnescapeString(p[1])
		switch {
		case label == "":
			return href
		case label == href || strings.HasPrefix(href, "#"):
			return label
		}
		return label + " (" + href + ")"
	})
	s = liRe.ReplaceAllString(s, "- ")
	s = cellRe.ReplaceAllString(s, " ")
	s = breakRe.ReplaceAllString(s, "\n")
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, " ", " ")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.Join(strings.Fields(l), " ")
	}
	s = strings.Join(lines, "\n")
	s = blankRe.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
