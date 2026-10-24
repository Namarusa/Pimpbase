package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/mail"
	"net/textproto"
	"strings"
	"time"
)

// Providers that take mail over HTTP. Each needs an API key; Mailgun also
// needs the sending domain and its region.

// APIMailer sends mail through a provider's HTTP API.
type APIMailer struct {
	Provider string // sendgrid, mailgun, postmark, resend
	APIKey   string
	// Domain and Region are Mailgun's: the sending domain, "us" or "eu".
	Domain, Region string
	From           string
	Client         *http.Client
	// BaseURL replaces the provider's address, for tests.
	BaseURL string
}

// ProviderNames are the providers' display names.
var ProviderNames = map[string]string{
	"smtp": "SMTP", "sendgrid": "SendGrid", "mailgun": "Mailgun", "postmark": "Postmark", "resend": "Resend", "log": "Log only",
}

func (a *APIMailer) base(def string) string {
	if a.BaseURL != "" {
		return strings.TrimRight(a.BaseURL, "/")
	}
	return def
}

func (a *APIMailer) Send(ctx context.Context, m *Mail) (*Receipt, error) {
	if m.From == "" {
		m.From = a.From
	}
	if _, err := mail.ParseAddress(m.From); err != nil {
		return nil, errorf("the sender %q is not an email address", m.From)
	}
	if _, err := m.recipients(); err != nil {
		return nil, err
	}
	if a.APIKey == "" {
		return nil, errorf("%s needs an API key", ProviderNames[a.Provider])
	}
	var req *http.Request
	var err error
	switch a.Provider {
	case "sendgrid":
		req, err = a.sendgrid(ctx, m)
	case "mailgun":
		req, err = a.mailgun(ctx, m)
	case "postmark":
		req, err = a.postmark(ctx, m)
	case "resend":
		req, err = a.resend(ctx, m)
	default:
		return nil, errorf("the mail provider %q is unknown", a.Provider)
	}
	if err != nil {
		return nil, err
	}
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, errorf("%s could not be reached: %s", ProviderNames[a.Provider], err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return nil, errorf("%s refused the mail (%d): %s", ProviderNames[a.Provider], res.StatusCode, providerError(body))
	}
	rc := &Receipt{Via: ProviderNames[a.Provider], Note: "sent through " + ProviderNames[a.Provider]}
	var out map[string]any
	json.Unmarshal(body, &out)
	switch a.Provider {
	case "sendgrid":
		rc.ID = res.Header.Get("X-Message-Id")
	case "mailgun", "resend":
		rc.ID, _ = out["id"].(string)
	case "postmark":
		rc.ID, _ = out["MessageID"].(string)
		if code, _ := out["ErrorCode"].(float64); code != 0 {
			return nil, errorf("Postmark refused the mail: %s", providerError(body))
		}
	}
	return rc, nil
}

// providerError finds the message in a provider's error answer.
func providerError(body []byte) string {
	var v map[string]any
	if json.Unmarshal(body, &v) == nil {
		for _, k := range []string{"message", "Message", "error"} {
			if s, ok := v[k].(string); ok && s != "" {
				return s
			}
		}
		if errs, ok := v["errors"].([]any); ok && len(errs) > 0 {
			if e, ok := errs[0].(map[string]any); ok {
				if s, ok := e["message"].(string); ok {
					return s
				}
			}
		}
	}
	s := strings.TrimSpace(string(body))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

func jsonRequest(ctx context.Context, url string, body any) (*http.Request, error) {
	js, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(js))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// named splits an address into its parts.
func named(addr string) (email, name string) {
	p, err := mail.ParseAddress(addr)
	if err != nil {
		return addr, ""
	}
	return p.Address, p.Name
}

func (a *APIMailer) sendgrid(ctx context.Context, m *Mail) (*http.Request, error) {
	list := func(addrs []string) []map[string]string {
		var out []map[string]string
		for _, x := range addrs {
			e, n := named(x)
			item := map[string]string{"email": e}
			if n != "" {
				item["name"] = n
			}
			out = append(out, item)
		}
		return out
	}
	pers := map[string]any{"to": list(m.To)}
	if len(m.Cc) > 0 {
		pers["cc"] = list(m.Cc)
	}
	if len(m.Bcc) > 0 {
		pers["bcc"] = list(m.Bcc)
	}
	fe, fn := named(m.From)
	from := map[string]string{"email": fe}
	if fn != "" {
		from["name"] = fn
	}
	var content []map[string]string
	if m.Text != "" {
		content = append(content, map[string]string{"type": "text/plain", "value": m.Text})
	}
	if m.HTML != "" {
		content = append(content, map[string]string{"type": "text/html", "value": m.HTML})
	}
	if len(content) == 0 {
		content = append(content, map[string]string{"type": "text/plain", "value": " "})
	}
	body := map[string]any{"personalizations": []any{pers}, "from": from, "subject": OneLine(m.Subject), "content": content}
	if m.ReplyTo != "" {
		e, n := named(m.ReplyTo)
		body["reply_to"] = map[string]string{"email": e, "name": n}
	}
	if len(m.Attachments) > 0 {
		var at []map[string]string
		for _, x := range m.Attachments {
			at = append(at, map[string]string{"content": base64.StdEncoding.EncodeToString(x.Data), "filename": x.Name, "type": typeOr(x.Type), "disposition": "attachment"})
		}
		body["attachments"] = at
	}
	req, err := jsonRequest(ctx, a.base("https://api.sendgrid.com")+"/v3/mail/send", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.APIKey)
	return req, nil
}

func (a *APIMailer) mailgun(ctx context.Context, m *Mail) (*http.Request, error) {
	if a.Domain == "" {
		return nil, errorf("Mailgun needs the sending domain")
	}
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	field := func(k, v string) {
		if v != "" {
			w.WriteField(k, v)
		}
	}
	field("from", m.From)
	for _, x := range m.To {
		field("to", x)
	}
	for _, x := range m.Cc {
		field("cc", x)
	}
	for _, x := range m.Bcc {
		field("bcc", x)
	}
	field("subject", OneLine(m.Subject))
	field("text", m.Text)
	field("html", m.HTML)
	field("h:Reply-To", m.ReplyTo)
	for _, x := range m.Attachments {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="attachment"; filename=%q`, x.Name))
		h.Set("Content-Type", typeOr(x.Type))
		part, err := w.CreatePart(h)
		if err != nil {
			return nil, err
		}
		part.Write(x.Data)
	}
	w.Close()
	host := "https://api.mailgun.net"
	if strings.EqualFold(a.Region, "eu") {
		host = "https://api.eu.mailgun.net"
	}
	req, err := http.NewRequestWithContext(ctx, "POST", a.base(host)+"/v3/"+a.Domain+"/messages", &b)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.SetBasicAuth("api", a.APIKey)
	return req, nil
}

func (a *APIMailer) postmark(ctx context.Context, m *Mail) (*http.Request, error) {
	body := map[string]any{
		"From": m.From, "To": strings.Join(m.To, ", "), "Subject": OneLine(m.Subject), "MessageStream": "outbound",
	}
	if len(m.Cc) > 0 {
		body["Cc"] = strings.Join(m.Cc, ", ")
	}
	if len(m.Bcc) > 0 {
		body["Bcc"] = strings.Join(m.Bcc, ", ")
	}
	if m.HTML != "" {
		body["HtmlBody"] = m.HTML
	}
	if m.Text != "" {
		body["TextBody"] = m.Text
	}
	if m.ReplyTo != "" {
		body["ReplyTo"] = m.ReplyTo
	}
	if len(m.Attachments) > 0 {
		var at []map[string]string
		for _, x := range m.Attachments {
			at = append(at, map[string]string{"Name": x.Name, "Content": base64.StdEncoding.EncodeToString(x.Data), "ContentType": typeOr(x.Type)})
		}
		body["Attachments"] = at
	}
	req, err := jsonRequest(ctx, a.base("https://api.postmarkapp.com")+"/email", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Postmark-Server-Token", a.APIKey)
	return req, nil
}

func (a *APIMailer) resend(ctx context.Context, m *Mail) (*http.Request, error) {
	body := map[string]any{"from": m.From, "to": m.To, "subject": OneLine(m.Subject)}
	if len(m.Cc) > 0 {
		body["cc"] = m.Cc
	}
	if len(m.Bcc) > 0 {
		body["bcc"] = m.Bcc
	}
	if m.HTML != "" {
		body["html"] = m.HTML
	}
	if m.Text != "" {
		body["text"] = m.Text
	}
	if m.ReplyTo != "" {
		body["reply_to"] = m.ReplyTo
	}
	if len(m.Attachments) > 0 {
		var at []map[string]string
		for _, x := range m.Attachments {
			at = append(at, map[string]string{"filename": x.Name, "content": base64.StdEncoding.EncodeToString(x.Data), "content_type": typeOr(x.Type)})
		}
		body["attachments"] = at
	}
	req, err := jsonRequest(ctx, a.base("https://api.resend.com")+"/emails", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.APIKey)
	return req, nil
}

func typeOr(t string) string {
	if t == "" {
		return "application/octet-stream"
	}
	return t
}
