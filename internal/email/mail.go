// Package email sends mail for flows and for the workspace (invitations,
// password resets, sign-in links). It holds the delivery settings (an SMTP
// server or a provider's API), the apps' email templates with their
// versions, and renders templates into HTML and plain text.
package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"log/slog"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/pimpbase/pimpbase/internal/ids"
)

// Mail is an email to send.
type Mail struct {
	From        string
	To, Cc, Bcc []string
	ReplyTo     string
	Subject     string
	Text, HTML  string
	Attachments []Attachment
	// MessageID is set when the mail is written; providers that make
	// their own ids report them in the Receipt.
	MessageID string
}

// Attachment is a file sent with a mail.
type Attachment struct {
	Name, Type string
	Data       []byte
}

// Receipt says how a mail went.
type Receipt struct {
	// Via names what delivered it: "SMTP mail.example.com", "SendGrid", "log".
	Via string `json:"via"`
	// ID is the provider's message id, when it gives one.
	ID string `json:"id,omitempty"`
	// Note is shown in run logs.
	Note string `json:"note"`
}

// Mailer delivers mail.
type Mailer interface {
	Send(ctx context.Context, m *Mail) (*Receipt, error)
}

// Error messages are safe to show.
type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

func errorf(format string, args ...any) error { return &Error{fmt.Sprintf(format, args...)} }

// ErrNotSetUp is returned when no delivery is set up.
var ErrNotSetUp = &Error{"email is not set up: a workspace admin chooses how mail is sent in Users → Email, or the server is started with --smtp"}

// NewMailer reads the server's mail setting:
//
//	smtp://user:pass@host:587     STARTTLS when the server offers it
//	smtps://user:pass@host:465    TLS from the start
//	log                           keep mails in the run log, send nothing
//
// from is the default sender. An empty setting gives nil: the workspace's
// saved settings decide.
func NewMailer(setting, from string) (Mailer, error) {
	setting = strings.TrimSpace(setting)
	switch setting {
	case "":
		return nil, nil
	case "log":
		return &LogMailer{From: from, Log: slog.Default()}, nil
	}
	u, err := url.Parse(setting)
	if err != nil || (u.Scheme != "smtp" && u.Scheme != "smtps") || u.Hostname() == "" {
		return nil, fmt.Errorf("the mail setting is smtp://user:password@host:port, smtps://… or log")
	}
	m := &SMTPMailer{Host: u.Hostname(), Port: u.Port(), TLS: u.Scheme == "smtps", From: from}
	if m.Port == "" {
		m.Port = "587"
		if m.TLS {
			m.Port = "465"
		}
	}
	if u.User != nil {
		m.Username = u.User.Username()
		m.Password, _ = u.User.Password()
	}
	if m.From == "" {
		m.From = u.Query().Get("from")
	}
	if m.From == "" {
		return nil, fmt.Errorf("set the sender address with --mail-from (PIMPBASE_MAIL_FROM)")
	}
	return m, nil
}

// LogMailer keeps mails instead of sending them, for trying flows out.
// With Log, it also writes each mail's recipients, subject and text to
// the server log, where links in invitations and resets can be found.
type LogMailer struct {
	From string
	Log  *slog.Logger
	mu   sync.Mutex
	Sent []*Mail
}

func (l *LogMailer) Send(_ context.Context, m *Mail) (*Receipt, error) {
	if m.From == "" {
		m.From = l.From
	}
	if l.Log != nil {
		l.Log.Info("email not sent (log only)", "to", strings.Join(m.To, ", "), "subject", m.Subject, "text", m.Text)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.Sent) >= 100 {
		l.Sent = l.Sent[1:]
	}
	l.Sent = append(l.Sent, m)
	return &Receipt{Via: "log", Note: "not sent: mails are only kept in the log"}, nil
}

// Last is the newest mail kept, or nil.
func (l *LogMailer) Last() *Mail {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.Sent) == 0 {
		return nil
	}
	return l.Sent[len(l.Sent)-1]
}

// SMTPMailer sends mail through an SMTP server.
type SMTPMailer struct {
	Host, Port string
	// TLS connects with TLS from the start (port 465); otherwise STARTTLS
	// is used when the server offers it, unless Plain is set.
	TLS                bool
	Plain              bool
	Username, Password string
	From               string
}

func (s *SMTPMailer) Send(ctx context.Context, m *Mail) (*Receipt, error) {
	if m.From == "" {
		m.From = s.From
	}
	from, err := mail.ParseAddress(m.From)
	if err != nil {
		return nil, errorf("the sender %q is not an email address", m.From)
	}
	rcpt, err := m.recipients()
	if err != nil {
		return nil, err
	}
	msg, err := m.Bytes(s.Host)
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(s.Host, s.Port)
	d := &net.Dialer{Timeout: 20 * time.Second}
	var conn net.Conn
	if s.TLS {
		conn, err = (&tls.Dialer{NetDialer: d, Config: &tls.Config{ServerName: s.Host}}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, errorf("the mail server %s could not be reached: %s", addr, err)
	}
	conn.SetDeadline(time.Now().Add(60 * time.Second))
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		conn.Close()
		return nil, errorf("the mail server: %s", err)
	}
	defer c.Close()
	if !s.TLS && !s.Plain {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: s.Host}); err != nil {
				return nil, errorf("the mail server's TLS: %s", err)
			}
		}
	}
	if s.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			return nil, errorf("signing in to the mail server failed: %s", err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return nil, errorf("the mail server refused the sender: %s", err)
	}
	for _, r := range rcpt {
		if err := c.Rcpt(r); err != nil {
			return nil, errorf("the mail server refused %s: %s", r, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(msg); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, errorf("the mail server refused the mail: %s", err)
	}
	c.Quit()
	return &Receipt{Via: "SMTP " + s.Host, ID: m.MessageID, Note: "sent through " + s.Host}, nil
}

// recipients checks every address and lists them all, Bcc included.
func (m *Mail) recipients() ([]string, error) {
	var out []string
	for _, a := range append(append(append([]string{}, m.To...), m.Cc...), m.Bcc...) {
		p, err := mail.ParseAddress(a)
		if err != nil {
			return nil, errorf("%q is not an email address", a)
		}
		out = append(out, p.Address)
	}
	if len(out) == 0 {
		return nil, errorf("there is nobody to send the email to")
	}
	return out, nil
}

// Check validates the addresses of a mail.
func (m *Mail) Check() error {
	if len(m.To) == 0 {
		return errorf("there is nobody to send the email to")
	}
	if _, err := m.recipients(); err != nil {
		return err
	}
	if m.ReplyTo != "" {
		if _, err := mail.ParseAddress(m.ReplyTo); err != nil {
			return errorf("the reply-to %q is not an email address", m.ReplyTo)
		}
	}
	return nil
}

// Bytes writes the mail as a MIME message. Bcc is left out. A mail with
// both texts is multipart/alternative, so mail apps that do not show HTML
// show the text.
func (m *Mail) Bytes(host string) ([]byte, error) {
	var b bytes.Buffer
	h := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
	}
	from := m.From
	if p, err := mail.ParseAddress(m.From); err == nil {
		from = p.String()
	}
	if m.MessageID == "" {
		m.MessageID = "<" + strings.TrimPrefix(ids.New("m"), "m_") + "@" + host + ">"
	}
	h("From", from)
	h("To", addressList(m.To))
	h("Cc", addressList(m.Cc))
	h("Reply-To", addressList([]string{m.ReplyTo}))
	h("Subject", mime.QEncoding.Encode("utf-8", OneLine(m.Subject)))
	h("Date", time.Now().Format(time.RFC1123Z))
	h("Message-ID", m.MessageID)
	h("MIME-Version", "1.0")

	if len(m.Attachments) == 0 {
		if err := m.writeBody(&b, h); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	}
	mw := multipart.NewWriter(&b)
	h("Content-Type", `multipart/mixed; boundary="`+mw.Boundary()+`"`)
	b.WriteString("\r\n")
	var body bytes.Buffer
	var bh textproto.MIMEHeader = textproto.MIMEHeader{}
	if err := m.writeBody(&body, func(k, v string) { bh.Set(k, v) }); err != nil {
		return nil, err
	}
	part, err := mw.CreatePart(bh)
	if err != nil {
		return nil, err
	}
	part.Write(bytes.TrimPrefix(body.Bytes(), []byte("\r\n")))
	for _, a := range m.Attachments {
		typ := a.Type
		if typ == "" {
			typ = "application/octet-stream"
		}
		part, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {typ},
			"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": a.Name})},
			"Content-Transfer-Encoding": {"base64"},
		})
		if err != nil {
			return nil, err
		}
		writeBase64(part, a.Data)
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// writeBody writes the body's headers with h, then a blank line and the
// body: one text, or both as alternatives.
func (m *Mail) writeBody(b *bytes.Buffer, h func(k, v string)) error {
	if m.HTML == "" || m.Text == "" {
		bodyType, body := "text/plain; charset=utf-8", m.Text
		if m.HTML != "" {
			bodyType, body = "text/html; charset=utf-8", m.HTML
		}
		h("Content-Type", bodyType)
		h("Content-Transfer-Encoding", "base64")
		b.WriteString("\r\n")
		writeBase64(b, []byte(body))
		return nil
	}
	mw := multipart.NewWriter(b)
	h("Content-Type", `multipart/alternative; boundary="`+mw.Boundary()+`"`)
	b.WriteString("\r\n")
	for _, p := range []struct{ typ, body string }{{"text/plain; charset=utf-8", m.Text}, {"text/html; charset=utf-8", m.HTML}} {
		part, err := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {p.typ}, "Content-Transfer-Encoding": {"base64"}})
		if err != nil {
			return err
		}
		writeBase64(part, []byte(p.body))
	}
	return mw.Close()
}

func addressList(list []string) string {
	out := make([]string, 0, len(list))
	for _, a := range list {
		if p, err := mail.ParseAddress(a); err == nil {
			out = append(out, p.String())
		}
	}
	return strings.Join(out, ", ")
}

func writeBase64(w interface{ Write([]byte) (int, error) }, data []byte) {
	enc := base64.StdEncoding.EncodeToString(data)
	for len(enc) > 76 {
		w.Write([]byte(enc[:76] + "\r\n"))
		enc = enc[76:]
	}
	w.Write([]byte(enc + "\r\n"))
}

// OneLine folds a text onto one line, as subjects need.
func OneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Addresses splits "a@x.com, b@y.com", a list of addresses, or rows and
// users with an email key into addresses.
func Addresses(v any) []string {
	var out []string
	var items []any
	switch x := v.(type) {
	case nil:
	case []any:
		items = x
	default:
		items = []any{v}
	}
	for _, item := range items {
		s := ""
		switch x := item.(type) {
		case string:
			s = x
		case map[string]any:
			for _, k := range []string{"email", "Email", "EMAIL"} {
				if e, ok := x[k].(string); ok {
					s = e
					break
				}
			}
		}
		for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// Count is how many mails are kept.
func (l *LogMailer) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.Sent)
}

// Reset forgets the mails kept.
func (l *LogMailer) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Sent = nil
}
