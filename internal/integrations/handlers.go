package integrations

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pimpbase/pimpbase/internal/files"
	"github.com/pimpbase/pimpbase/internal/interp"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/jwt"
)

// handler runs an action that takes more than one request, or signing a
// manifest cannot describe. Only built-in manifests use them.
type handler func(ctx context.Context, cx *callCtx, a *Action, given map[string]any) (any, error)

var handlers map[string]handler

func init() {
	handlers = map[string]handler{
		"stripe.create_invoice": stripeInvoice,
		"s3.test":               s3Test,
		"s3.put_object":         s3Put,
		"s3.get_object":         s3Get,
		"s3.delete_object":      s3Delete,
		"s3.list_objects":       s3List,
		"s3.presign":            s3Presign,
		"sheets.read_range":     sheetsRead,
	}
}

// values reads every input of an action, converted, by key.
func values(a *Action, given map[string]any) (map[string]any, error) {
	out := map[string]any{}
	for _, in := range a.Inputs {
		v, err := inputValue(in, given)
		if err != nil {
			return nil, err
		}
		if empty(v) {
			if in.Required {
				return nil, invalid("%s is required", in.Label)
			}
			continue
		}
		out[in.Key] = v
	}
	return out, nil
}

func text(m map[string]any, k string) string { return strings.TrimSpace(interp.ToText(m[k])) }

// stripeInvoice makes a draft invoice, adds its lines, and finalizes
// and sends it when asked. Each call has its own idempotency key, so a
// retried step does not bill twice.
func stripeInvoice(ctx context.Context, cx *callCtx, a *Action, given map[string]any) (any, error) {
	v, err := values(a, given)
	if err != nil {
		return nil, err
	}
	key := cx.key
	post := func(part, path string, body map[string]any) (map[string]any, error) {
		cx.key = key + "|" + part
		b, err := cx.build(Request{Method: "POST", Path: path, Body: "form"}, nil, nil)
		if err != nil {
			return nil, err
		}
		b.body, b.ctype = []byte(formValues(body).Encode()), "application/x-www-form-urlencoded"
		out, err := cx.do(ctx, b)
		if err != nil {
			return nil, err
		}
		m, _ := out.(map[string]any)
		if m == nil {
			return nil, errors.New("Stripe gave an answer that is not an object")
		}
		return m, nil
	}
	inv := map[string]any{"customer": text(v, "customer"), "auto_advance": false}
	method := text(v, "collection")
	if method == "" {
		method = "send_invoice"
	}
	inv["collection_method"] = method
	if method == "send_invoice" {
		days, _ := toNumber(v["daysUntilDue"])
		if days <= 0 {
			days = 30
		}
		inv["days_until_due"] = days
	}
	for _, k := range []string{"description", "currency"} {
		if s := text(v, k); s != "" {
			inv[k] = s
		}
	}
	if md, ok := v["metadata"].(map[string]any); ok && len(md) > 0 {
		inv["metadata"] = md
	}
	invoice, err := post("invoice", "/v1/invoices", inv)
	if err != nil {
		return nil, err
	}
	id, _ := invoice["id"].(string)
	lines, _ := v["lines"].([]any)
	for i, l := range lines {
		line, _ := l.(map[string]any)
		item := map[string]any{"customer": inv["customer"], "invoice": id}
		for k, x := range line {
			item[k] = x
		}
		if _, err := post(fmt.Sprintf("line%d", i), "/v1/invoiceitems", item); err != nil {
			return nil, fmt.Errorf("adding line %d to invoice %s: %w", i+1, id, err)
		}
	}
	send, _ := v["send"].(bool)
	if send || truthy(v["finalize"]) {
		if invoice, err = post("finalize", "/v1/invoices/"+url.PathEscape(id)+"/finalize", map[string]any{}); err != nil {
			return nil, fmt.Errorf("finalizing invoice %s: %w", id, err)
		}
		if send && method == "send_invoice" {
			if invoice, err = post("send", "/v1/invoices/"+url.PathEscape(id)+"/send", map[string]any{}); err != nil {
				return nil, fmt.Errorf("sending invoice %s: %w", id, err)
			}
		}
	}
	cx.sent = fmt.Sprintf("POST /v1/invoices, %d invoice items%s", len(lines), map[bool]string{true: ", finalize and send", false: ""}[send])
	return invoice, nil
}

func truthy(v any) bool { b, _ := v.(bool); return b }

// sheetsRead reads a range; with headers, each row after the first
// becomes an object named by the first row's cells.
func sheetsRead(ctx context.Context, cx *callCtx, a *Action, given map[string]any) (any, error) {
	v, err := values(a, given)
	if err != nil {
		return nil, err
	}
	in := map[string]any{"spreadsheetId": v["spreadsheetId"], "range": v["range"]}
	specs := []*Input{
		{Key: "spreadsheetId", Label: "Spreadsheet", Kind: "text", In: "path", Setting: "spreadsheetId"},
		{Key: "range", Label: "Range", Kind: "text", In: "path", Required: true},
	}
	b, err := cx.build(Request{Method: "GET", Path: "/{spreadsheetId}/values/{range}", Body: "none"}, specs, in)
	if err != nil {
		return nil, err
	}
	body, err := cx.do(ctx, b)
	if err != nil {
		return nil, err
	}
	cells, _ := Dig(body, "values").([]any)
	if cells == nil {
		cells = []any{}
	}
	out := map[string]any{"values": cells, "range": Dig(body, "range")}
	headers, ok := v["headers"].(bool)
	if !ok {
		headers = true
	}
	if !headers || len(cells) == 0 {
		out["rows"], out["count"] = cells, len(cells)
		return out, nil
	}
	head, _ := cells[0].([]any)
	rows := []any{}
	for _, r := range cells[1:] {
		line, _ := r.([]any)
		row := map[string]any{}
		for i, h := range head {
			name := strings.TrimSpace(interp.ToText(h))
			if name == "" {
				name = fmt.Sprintf("Column %d", i+1)
			}
			if i < len(line) {
				row[name] = line[i]
			} else {
				row[name] = ""
			}
		}
		rows = append(rows, row)
	}
	out["rows"], out["count"] = rows, len(rows)
	return out, nil
}

// S3. Requests are signed with Signature V4 by files.S3Blobs, which the
// file storage uses too.

func (cx *callCtx) bucket(v map[string]any) (*files.S3Blobs, error) {
	name := text(v, "bucket")
	if name == "" {
		name = strings.TrimSpace(cx.vals["bucket"])
	}
	if name == "" {
		return nil, invalid("name the bucket, in the step or the connection")
	}
	return files.NewS3(strings.TrimSpace(cx.vals["endpoint"]), name, strings.TrimSpace(cx.vals["region"]),
		cx.vals["accessKeyId"], cx.vals["secretAccessKey"], cx.vals["pathStyle"] == "true", cx.s.Client)
}

func s3Answer(what string, status int, body []byte) error {
	var e struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	xml.Unmarshal(body, &e)
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = e.Code
	}
	if msg == "" {
		msg = strings.TrimSpace(string(body))
	}
	return &CallError{Status: status, Message: fmt.Sprintf("S3 answered %d while %s: %s", status, what, msg)}
}

func s3Test(ctx context.Context, cx *callCtx, _ *Action, _ map[string]any) (any, error) {
	b, err := cx.bucket(map[string]any{})
	if err != nil {
		return nil, err
	}
	res, err := b.Do(ctx, "GET", "", url.Values{"list-type": {"2"}, "max-keys": {"1"}}, "", nil)
	if err != nil {
		return nil, &CallError{Message: "S3 could not be reached: " + err.Error()}
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode/100 != 2 {
		return nil, s3Answer("listing the bucket", res.StatusCode, data)
	}
	return map[string]any{"bucket": b.Bucket}, nil
}

func s3Put(ctx context.Context, cx *callCtx, a *Action, given map[string]any) (any, error) {
	v, err := values(a, given)
	if err != nil {
		return nil, err
	}
	b, err := cx.bucket(v)
	if err != nil {
		return nil, err
	}
	key := strings.TrimLeft(text(v, "key"), "/")
	var data []byte
	ctype := text(v, "contentType")
	if f := v["file"]; f != nil {
		id, ok := fileID(f)
		if !ok {
			return nil, invalid("File should be a file, such as {{ steps.Pdf.file }}")
		}
		meta, d, err := cx.s.Files.Get(ctx, cx.conn.AppID, id)
		if err != nil {
			return nil, invalid("the file %s cannot be read: %s", id, err)
		}
		data = d
		if ctype == "" {
			ctype = meta.Type
		}
		if key == "" || strings.HasSuffix(key, "/") {
			key += meta.Name
		}
	} else {
		data = []byte(interp.ToText(v["content"]))
		if ctype == "" {
			ctype = "text/plain; charset=utf-8"
		}
	}
	if key == "" {
		return nil, invalid("Key is required")
	}
	res, err := b.Do(ctx, "PUT", key, nil, ctype, data)
	if err != nil {
		return nil, &CallError{Message: "S3 could not be reached: " + err.Error()}
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode/100 != 2 {
		return nil, s3Answer("storing "+key, res.StatusCode, body)
	}
	cx.sent = "PUT s3://" + b.Bucket + "/" + key
	return map[string]any{"bucket": b.Bucket, "key": key, "size": len(data), "etag": strings.Trim(res.Header.Get("ETag"), `"`), "contentType": ctype}, nil
}

func fileID(v any) (string, bool) {
	switch x := v.(type) {
	case map[string]any:
		if f, ok := x["file"].(map[string]any); ok {
			x = f
		}
		id, _ := x["id"].(string)
		return id, strings.HasPrefix(id, "fil_")
	case []any:
		if len(x) > 0 {
			return fileID(x[0])
		}
	case string:
		return x, strings.HasPrefix(x, "fil_")
	}
	return "", false
}

func s3Get(ctx context.Context, cx *callCtx, a *Action, given map[string]any) (any, error) {
	v, err := values(a, given)
	if err != nil {
		return nil, err
	}
	b, err := cx.bucket(v)
	if err != nil {
		return nil, err
	}
	key := strings.TrimLeft(text(v, "key"), "/")
	res, err := b.Do(ctx, "GET", key, nil, "", nil)
	if err != nil {
		return nil, &CallError{Message: "S3 could not be reached: " + err.Error()}
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if res.StatusCode/100 != 2 {
		return nil, s3Answer("reading "+key, res.StatusCode, data)
	}
	cx.sent = "GET s3://" + b.Bucket + "/" + key
	out := map[string]any{"key": key, "size": len(data), "contentType": res.Header.Get("Content-Type"), "etag": strings.Trim(res.Header.Get("ETag"), `"`)}
	if utf8.Valid(data) {
		out["text"] = string(data)
		var js any
		if strings.Contains(res.Header.Get("Content-Type"), "json") && json.Unmarshal(data, &js) == nil {
			out["data"] = js
		}
	} else {
		out["base64"] = base64.StdEncoding.EncodeToString(data)
	}
	return out, nil
}

func s3Delete(ctx context.Context, cx *callCtx, a *Action, given map[string]any) (any, error) {
	v, err := values(a, given)
	if err != nil {
		return nil, err
	}
	b, err := cx.bucket(v)
	if err != nil {
		return nil, err
	}
	key := strings.TrimLeft(text(v, "key"), "/")
	res, err := b.Do(ctx, "DELETE", key, nil, "", nil)
	if err != nil {
		return nil, &CallError{Message: "S3 could not be reached: " + err.Error()}
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode/100 != 2 && res.StatusCode != 404 {
		return nil, s3Answer("deleting "+key, res.StatusCode, data)
	}
	cx.sent = "DELETE s3://" + b.Bucket + "/" + key
	return map[string]any{"key": key, "deleted": true}, nil
}

func s3List(ctx context.Context, cx *callCtx, a *Action, given map[string]any) (any, error) {
	v, err := values(a, given)
	if err != nil {
		return nil, err
	}
	b, err := cx.bucket(v)
	if err != nil {
		return nil, err
	}
	q := url.Values{"list-type": {"2"}}
	if p := text(v, "prefix"); p != "" {
		q.Set("prefix", p)
	}
	max, _ := toNumber(v["max"])
	if max <= 0 || max > 1000 {
		max = 1000
	}
	q.Set("max-keys", fmt.Sprint(int(max)))
	res, err := b.Do(ctx, "GET", "", q, "", nil)
	if err != nil {
		return nil, &CallError{Message: "S3 could not be reached: " + err.Error()}
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if res.StatusCode/100 != 2 {
		return nil, s3Answer("listing", res.StatusCode, data)
	}
	var list struct {
		Truncated bool `xml:"IsTruncated"`
		Contents  []struct {
			Key          string `xml:"Key"`
			Size         int64  `xml:"Size"`
			LastModified string `xml:"LastModified"`
			ETag         string `xml:"ETag"`
		} `xml:"Contents"`
	}
	if err := xml.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("S3's list could not be read: %w", err)
	}
	rows := []any{}
	for _, c := range list.Contents {
		rows = append(rows, map[string]any{"key": c.Key, "size": c.Size, "modified": c.LastModified, "etag": strings.Trim(c.ETag, `"`)})
	}
	cx.sent = "GET s3://" + b.Bucket + "/?prefix=" + q.Get("prefix")
	return map[string]any{"rows": rows, "count": len(rows), "truncated": list.Truncated}, nil
}

func s3Presign(ctx context.Context, cx *callCtx, a *Action, given map[string]any) (any, error) {
	v, err := values(a, given)
	if err != nil {
		return nil, err
	}
	b, err := cx.bucket(v)
	if err != nil {
		return nil, err
	}
	key := strings.TrimLeft(text(v, "key"), "/")
	mins, _ := toNumber(v["minutes"])
	if mins <= 0 {
		mins = 60
	}
	if mins > 7*24*60 {
		return nil, invalid("a link lasts at most 7 days (10080 minutes)")
	}
	method := strings.ToUpper(text(v, "method"))
	if method != "PUT" {
		method = "GET"
	}
	d := time.Duration(mins) * time.Minute
	return map[string]any{"url": b.Presign(method, key, d), "method": method, "key": key, "expires": cx.s.now().UTC().Add(d).Format(time.RFC3339)}, nil
}

// Google service accounts: a signed JWT is traded for an access token,
// which is kept until it expires.

type cachedToken struct {
	src oauth2.TokenSource
	key string
}

func (s *Service) forgetToken(connID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.tokens {
		if strings.HasPrefix(k, connID+"|") {
			delete(s.tokens, k)
		}
	}
}

func (cx *callCtx) googleToken(ctx context.Context) (string, error) {
	raw := cx.vals[cx.m.Auth.Credential]
	var key struct {
		Type       string `json:"type"`
		Email      string `json:"client_email"`
		PrivateKey string `json:"private_key"`
		KeyID      string `json:"private_key_id"`
		TokenURI   string `json:"token_uri"`
	}
	if err := json.Unmarshal([]byte(raw), &key); err != nil || key.Email == "" || key.PrivateKey == "" {
		return "", invalid("the service account key should be the JSON file Google gives (with client_email and private_key)")
	}
	if key.TokenURI == "" {
		key.TokenURI = "https://oauth2.googleapis.com/token"
	}
	ck := cx.conn.ID + "|" + cx.env
	cx.s.mu.Lock()
	ct := cx.s.tokens[ck]
	if ct == nil || ct.key != raw {
		cfg := &jwt.Config{Email: key.Email, PrivateKey: []byte(key.PrivateKey), PrivateKeyID: key.KeyID, Scopes: cx.m.Auth.Scopes, TokenURL: key.TokenURI}
		ct = &cachedToken{src: oauth2.ReuseTokenSource(nil, cfg.TokenSource(context.WithValue(context.Background(), oauth2.HTTPClient, cx.s.Client))), key: raw}
		cx.s.tokens[ck] = ct
	}
	cx.s.mu.Unlock()
	t, err := ct.src.Token()
	if err != nil {
		cx.s.forgetToken(cx.conn.ID)
		var re *oauth2.RetrieveError
		if errors.As(err, &re) {
			return "", &CallError{Status: re.Response.StatusCode, Message: "Google refused the service account: " + strings.TrimSpace(string(re.Body))}
		}
		return "", &CallError{Message: "Google's token service could not be reached: " + err.Error()}
	}
	return t.AccessToken, nil
}
