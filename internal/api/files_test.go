package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bubibase/bubibase/internal/store"
	"github.com/bubibase/bubibase/internal/store/storetest"
)

// upload posts a file the way the browser's file input does.
func (c *client) upload(path, name, typ string, content []byte, wantStatus int) map[string]any {
	c.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{`form-data; name="file"; filename="` + name + `"`}
	h["Content-Type"] = []string{typ}
	part, _ := mw.CreatePart(h)
	part.Write(content)
	mw.Close()
	req, _ := http.NewRequest("POST", c.base+path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", c.base)
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != wantStatus {
		c.t.Fatalf("upload %s: status %d, want %d: %s", name, res.StatusCode, wantStatus, data)
	}
	var out map[string]any
	json.Unmarshal(data, &out)
	return out
}

func (c *client) get(path string) *http.Response {
	c.t.Helper()
	res, err := c.http.Get(c.base + path)
	if err != nil {
		c.t.Fatal(err)
	}
	return res
}

const tinyPDF = "%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Kids[]/Count 0>>endobj\ntrailer<</Root 1 0 R>>\n%%EOF\n"

func TestFiles(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		srv := httptest.NewServer(New(s, nil, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
		defer srv.Close()
		owner := newClient(t, srv.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		owner.do("POST", "/api/apps", map[string]any{"name": "Contracts"}, 201)
		base := "/api/apps/contracts"
		tbl := owner.do("POST", base+"/tables", map[string]any{
			"name": "Deals",
			"fields": []any{
				map[string]any{"name": "Name", "type": "text"},
				map[string]any{"name": "Contract", "type": "attachment"},
				map[string]any{"name": "Files", "type": "formula", "formula": `JOIN([Contract], "; ")`},
			},
		}, 201)
		tb := base + "/tables/" + tbl["id"].(string)

		pdf := owner.upload(tb+"/files", "../secret/Contract é.pdf", "application/octet-stream", []byte(tinyPDF), 201)
		if pdf["type"] != "application/pdf" || pdf["name"] != "Contract é.pdf" || pdf["size"] != float64(len(tinyPDF)) {
			t.Fatalf("upload = %v", pdf)
		}
		page := owner.upload(tb+"/files", "page.html", "text/html", []byte("<script>alert(1)</script>"), 201)
		owner.upload(tb+"/files", "empty.pdf", "application/pdf", nil, 400)

		row := owner.do("POST", tb+"/rows", map[string]any{"Name": "Acme", "Contract": []any{pdf, page}}, 201)
		files, _ := row["Contract"].([]any)
		if len(files) != 2 || files[0].(map[string]any)["url"] != pdf["url"] {
			t.Fatalf("row files = %v", row["Contract"])
		}
		if row["Files"] != "Contract é.pdf; page.html" {
			t.Errorf("formula over files = %v", row["Files"])
		}
		owner.do("POST", tb+"/rows", map[string]any{"Contract": []any{map[string]any{"name": "x.pdf"}}}, 400)
		owner.do("POST", tb+"/rows", map[string]any{"Contract": "x.pdf"}, 400)

		// PDFs open in the page; HTML is only ever downloaded.
		res := owner.get(pdf["url"].(string))
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/pdf" || string(body) != tinyPDF {
			t.Fatalf("get pdf: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
		}
		if cd := res.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "inline;") || !strings.Contains(cd, "Contract%20%C3%A9.pdf") {
			t.Errorf("disposition = %q", cd)
		}
		res = owner.get(pdf["url"].(string) + "?download=1")
		res.Body.Close()
		if !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment;") {
			t.Errorf("download disposition = %q", res.Header.Get("Content-Disposition"))
		}
		res = owner.get(page["url"].(string))
		res.Body.Close()
		if res.Header.Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment;") {
			t.Errorf("html served as %q, %q", res.Header.Get("Content-Type"), res.Header.Get("Content-Disposition"))
		}

		// Reading a file needs read access to its table; uploading needs write.
		anon := newClient(t, srv.URL)
		if res := anon.get(pdf["url"].(string)); res.StatusCode != 401 {
			t.Errorf("anonymous read = %d", res.StatusCode)
		}
		anon.upload(tb+"/files", "a.pdf", "application/pdf", []byte(tinyPDF), 401)
		owner.do("PATCH", tb, map[string]any{"permissions": map[string]any{"read": "PUBLIC", "write": "ADMIN"}}, 200)
		if res := anon.get(pdf["url"].(string)); res.StatusCode != 200 {
			t.Errorf("public read = %d", res.StatusCode)
		}
		anon.upload(tb+"/files", "a.pdf", "application/pdf", []byte(tinyPDF), 401)
		if res := owner.get(base + "/files/fil_missing"); res.StatusCode != 404 {
			t.Errorf("missing file = %d", res.StatusCode)
		}

		// CSV export lists file names; import leaves the field alone.
		csv := string(owner.raw("GET", tb+"/export", nil, 200))
		if !strings.Contains(csv, "Contract é.pdf, page.html") {
			t.Errorf("csv = %q", csv)
		}
		imp := owner.do("POST", tb+"/import", "Name,Contract\nBeta,b.pdf\n", 200)
		if imp["imported"] != 1.0 {
			t.Errorf("import = %v", imp)
		}
	})
}
