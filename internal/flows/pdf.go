package flows

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
)

// Document is a PDF a flow makes from a filled template.
type Document struct {
	Title string
	// Content is light Markdown: # headings, **bold**, *italic*, - and 1.
	// lists, | tables |, --- rules and \newpage. Each line break is kept.
	Content string
	// PageSize is "A4" or "Letter"; Landscape turns the page.
	PageSize  string
	Landscape bool
	// Footer is printed at the bottom of every page, next to the page
	// number.
	Footer string
	// Accent colours headings and table headers, as #rrggbb.
	Accent string
	Author string
}

var (
	inlineRe   = regexp.MustCompile(`\*\*(.+?)\*\*|\*([^*\s][^*]*?)\*`)
	orderedRe  = regexp.MustCompile(`^(\d{1,3})[.)]\s+(.*)$`)
	tableSepRe = regexp.MustCompile(`^\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?$`)
	numberRe   = regexp.MustCompile(`^[-+]?[$€£]?\s?[\d.,  ]+\s?[%$€£]?$`)
)

// PDF lays a document out. Text uses the PDF core fonts, which cover
// Western European languages (Windows-1252); other characters print as ?.
func (d *Document) PDF() ([]byte, error) {
	orient := "P"
	if d.Landscape {
		orient = "L"
	}
	size := "A4"
	if strings.EqualFold(d.PageSize, "letter") {
		size = "Letter"
	}
	pdf := fpdf.New(orient, "mm", size, "")
	tr := pdf.UnicodeTranslatorFromDescriptor("")
	accent := [3]int{37, 99, 235}
	if c, ok := hexColor(d.Accent); ok {
		accent = c
	}
	pdf.SetTitle(d.Title, true)
	pdf.SetAuthor(d.Author, true)
	pdf.SetCreator("Bubibase", true)
	pdf.SetCreationDate(time.Now())
	pdf.SetMargins(18, 18, 18)
	pdf.SetAutoPageBreak(true, 20)
	pdf.AliasNbPages("{nb}")
	pdf.SetFooterFunc(func() {
		pdf.SetY(-13)
		pdf.SetFont("Helvetica", "", 8)
		pdf.SetTextColor(120, 120, 120)
		w, _ := pdf.GetPageSize()
		l, _, r, _ := pdf.GetMargins()
		if d.Footer != "" {
			pdf.CellFormat((w-l-r)*0.75, 5, tr(oneLine(d.Footer)), "", 0, "L", false, 0, "")
		}
		pdf.SetX(l + (w-l-r)*0.75)
		pdf.CellFormat((w-l-r)*0.25, 5, fmt.Sprintf("Page %d of {nb}", pdf.PageNo()), "", 0, "R", false, 0, "")
	})
	pdf.AddPage()
	pageW, _ := pdf.GetPageSize()
	left, _, right, _ := pdf.GetMargins()
	width := pageW - left - right

	text := func() { pdf.SetTextColor(30, 30, 30) }
	if d.Title != "" {
		pdf.SetFont("Helvetica", "B", 20)
		pdf.SetTextColor(accent[0], accent[1], accent[2])
		pdf.MultiCell(width, 9, tr(d.Title), "", "L", false)
		pdf.Ln(2)
		pdf.SetDrawColor(accent[0], accent[1], accent[2])
		pdf.SetLineWidth(0.6)
		pdf.Line(left, pdf.GetY(), left+width, pdf.GetY())
		pdf.Ln(5)
	}

	// inline writes text with **bold** and *italic* runs.
	inline := func(s string, size float64, base string, lh float64) {
		at := 0
		for _, m := range inlineRe.FindAllStringSubmatchIndex(s, -1) {
			if m[0] > at {
				pdf.SetFont("Helvetica", base, size)
				pdf.Write(lh, tr(s[at:m[0]]))
			}
			style, inner := "B", ""
			switch {
			case m[2] >= 0:
				inner = s[m[2]:m[3]]
			default:
				style, inner = "I", s[m[4]:m[5]]
			}
			if base != "" && !strings.Contains(style, base) {
				style += base
			}
			pdf.SetFont("Helvetica", style, size)
			pdf.Write(lh, tr(inner))
			at = m[1]
		}
		pdf.SetFont("Helvetica", base, size)
		pdf.Write(lh, tr(s[at:]))
	}

	lines := strings.Split(strings.ReplaceAll(d.Content, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " \t")
		trimmed := strings.TrimSpace(line)
		text()
		switch {
		case trimmed == "":
			pdf.Ln(3)
		case trimmed == `\newpage`:
			pdf.AddPage()
		case trimmed == "---" || trimmed == "***":
			pdf.Ln(2)
			pdf.SetDrawColor(200, 200, 200)
			pdf.SetLineWidth(0.3)
			pdf.Line(left, pdf.GetY(), left+width, pdf.GetY())
			pdf.Ln(4)
		case strings.HasPrefix(trimmed, "#"):
			level := len(trimmed) - len(strings.TrimLeft(trimmed, "#"))
			sizes := []float64{16, 13.5, 11.5, 10.5}
			if level > len(sizes) {
				level = len(sizes)
			}
			pdf.Ln(2)
			pdf.SetTextColor(accent[0], accent[1], accent[2])
			if level > 1 {
				pdf.SetTextColor(40, 40, 40)
			}
			inline(strings.TrimSpace(strings.TrimLeft(trimmed, "#")), sizes[level-1], "B", sizes[level-1]*0.5)
			pdf.Ln(sizes[level-1]*0.5 + 1.5)
		case strings.HasPrefix(trimmed, "|"):
			var rows []string
			for ; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
				rows = append(rows, strings.TrimSpace(lines[i]))
			}
			i--
			table(pdf, tr, rows, width, accent)
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || orderedRe.MatchString(trimmed):
			mark, body := "•", strings.TrimSpace(trimmed[2:])
			if m := orderedRe.FindStringSubmatch(trimmed); m != nil {
				mark, body = m[1]+".", m[2]
			}
			indent := float64(len(line)-len(strings.TrimLeft(line, " \t"))) / 2 * 5
			pdf.SetFont("Helvetica", "", 10.5)
			pdf.SetX(left + indent)
			pdf.CellFormat(6, 5.5, tr(mark), "", 0, "L", false, 0, "")
			pdf.SetLeftMargin(left + indent + 6)
			inline(body, 10.5, "", 5.5)
			pdf.SetLeftMargin(left)
			pdf.Ln(6)
		default:
			inline(line, 10.5, "", 5.5)
			pdf.Ln(5.5)
		}
	}
	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		return nil, fmt.Errorf("making the PDF failed: %w", err)
	}
	return out.Bytes(), nil
}

func cells(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimPrefix(row, "|")
	row = strings.TrimSuffix(row, "|")
	parts := strings.Split(row, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// table draws a Markdown table: a header row, a --- row, then body rows.
// Columns share the width by their content. The --- row aligns columns
// (---: right, :---: centre); without it numbers align right. A cell
// written **like this** is bold.
func table(pdf *fpdf.Fpdf, tr func(string) string, rows []string, width float64, accent [3]int) {
	var head []string
	var body [][]string
	var aligns []string
	for i, r := range rows {
		if i == 1 && tableSepRe.MatchString(r) {
			for _, c := range cells(r) {
				switch {
				case strings.HasPrefix(c, ":") && strings.HasSuffix(c, ":"):
					aligns = append(aligns, "C")
				case strings.HasSuffix(c, ":"):
					aligns = append(aligns, "R")
				case strings.HasPrefix(c, ":"):
					aligns = append(aligns, "L")
				default:
					aligns = append(aligns, "")
				}
			}
			continue
		}
		if i == 0 && len(rows) > 1 && tableSepRe.MatchString(rows[1]) {
			head = cells(r)
			continue
		}
		body = append(body, cells(r))
	}
	cols := len(head)
	for _, r := range body {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols == 0 {
		return
	}
	pad := func(r []string) []string {
		for len(r) < cols {
			r = append(r, "")
		}
		return r
	}
	head = pad(head)
	bold := func(v string) (string, bool) {
		if len(v) > 4 && strings.HasPrefix(v, "**") && strings.HasSuffix(v, "**") {
			return v[2 : len(v)-2], true
		}
		return v, false
	}
	// Widths follow the longest text of each column, within limits.
	want := make([]float64, cols)
	for _, r := range append([][]string{head}, body...) {
		for c, v := range pad(r) {
			v, b := bold(v)
			style := ""
			if b {
				style = "B"
			}
			pdf.SetFont("Helvetica", style, 9.5)
			if w := pdf.GetStringWidth(tr(v)) + 4; w > want[c] {
				want[c] = w
			}
		}
	}
	total := 0.0
	for c := range want {
		if want[c] < 14 {
			want[c] = 14
		}
		if want[c] > width*0.6 {
			want[c] = width * 0.6
		}
		total += want[c]
	}
	for c := range want {
		want[c] = want[c] / total * width
	}
	align := make([]string, cols)
	for c := range align {
		if c < len(aligns) && aligns[c] != "" {
			align[c] = aligns[c]
			continue
		}
		numeric := len(body) > 0
		for _, r := range body {
			v, _ := bold(pad(r)[c])
			if v != "" && !numberRe.MatchString(v) {
				numeric = false
			}
		}
		align[c] = "L"
		if numeric {
			align[c] = "R"
		}
	}
	_, pageH := pdf.GetPageSize()
	left, _, _, bottom := pdf.GetMargins()
	const lh = 5.0
	draw := func(r []string, header bool, shade bool) {
		lines := 1
		split := make([][]string, cols)
		styles := make([]string, cols)
		for c, v := range r {
			v, b := bold(v)
			if header || b {
				styles[c] = "B"
			}
			pdf.SetFont("Helvetica", styles[c], 9.5)
			split[c] = wrap(pdf, tr, v, want[c]-3)
			if len(split[c]) > lines {
				lines = len(split[c])
			}
		}
		h := float64(lines)*lh + 2
		if pdf.GetY()+h > pageH-bottom-2 {
			pdf.AddPage()
		}
		y := pdf.GetY()
		x := left
		for c := range r {
			switch {
			case header:
				pdf.SetFillColor(accent[0], accent[1], accent[2])
				pdf.SetTextColor(255, 255, 255)
			case shade:
				pdf.SetFillColor(245, 246, 248)
				pdf.SetTextColor(30, 30, 30)
			default:
				pdf.SetFillColor(255, 255, 255)
				pdf.SetTextColor(30, 30, 30)
			}
			pdf.Rect(x, y, want[c], h, "F")
			pdf.SetFont("Helvetica", styles[c], 9.5)
			for k, l := range split[c] {
				pdf.SetXY(x+1.5, y+1+float64(k)*lh)
				pdf.CellFormat(want[c]-3, lh, l, "", 0, align[c], false, 0, "")
			}
			x += want[c]
		}
		pdf.SetDrawColor(225, 228, 232)
		pdf.SetLineWidth(0.2)
		pdf.Line(left, y+h, left+width, y+h)
		pdf.SetXY(left, y+h)
	}
	pdf.Ln(1)
	if strings.Join(head, "") != "" {
		draw(head, true, false)
	}
	for i, r := range body {
		draw(pad(r), false, i%2 == 1)
	}
	pdf.Ln(3)
}

func hexColor(s string) ([3]int, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return [3]int{}, false
	}
	var c [3]int
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
		if err != nil {
			return [3]int{}, false
		}
		c[i] = int(v)
	}
	return c, true
}

// wrap breaks text into lines that fit width, measuring the translated
// text (fpdf's SplitText needs Latin-1 input). Lines come back translated.
func wrap(pdf *fpdf.Fpdf, tr func(string) string, text string, width float64) []string {
	var out []string
	fits := func(s string) bool { return pdf.GetStringWidth(tr(s)) <= width }
	for _, para := range strings.Split(text, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			try := word
			if line != "" {
				try = line + " " + word
			}
			if fits(try) {
				line = try
				continue
			}
			if line != "" {
				out = append(out, tr(line))
				line = ""
			}
			// A word wider than the cell is cut.
			for !fits(word) {
				r := []rune(word)
				n := len(r) - 1
				for n > 1 && !fits(string(r[:n])) {
					n--
				}
				out = append(out, tr(string(r[:n])))
				word = string(r[n:])
			}
			line = word
		}
		out = append(out, tr(line))
	}
	return out
}
