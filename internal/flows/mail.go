package flows

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
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

// Mail is an email a flow sends.
type Mail struct {
	From        string
	To, Cc      []string
	ReplyTo     string
	Subject     string
	Text, HTML  string
	Attachments []Attachment
}

// Attachment is a file sent with a mail.
type Attachment struct {
	Name, Type string
	Data       []byte
}

// Mailer delivers mail. Send returns a note on how the mail went, shown in
// the run log.
type Mailer interface {
	Send(ctx context.Context, m *Mail) (string, error)
}

// NewMailer reads the server's mail setting:
//
//	smtp://user:pass@host:587     STARTTLS when the server offers it
//	smtps://user:pass@host:465    TLS from the start
//	log                           keep mails in the run log, send nothing
//
// from is the default sender. An empty setting gives a mailer that
// explains how to set mail up.
func NewMailer(setting, from string) (Mailer, error) {
	setting = strings.TrimSpace(setting)
	switch setting {
	case "":
		return noMailer{}, nil
	case "log":
		return &LogMailer{From: from}, nil
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

type noMailer struct{}

func (noMailer) Send(context.Context, *Mail) (string, error) {
	return "", fmt.Errorf("email is not set up: start the server with --smtp smtp://user:password@host:587 and --mail-from, or --smtp log to only log mails")
}

// LogMailer keeps mails instead of sending them, for trying flows out.
type LogMailer struct {
	From string
	mu   sync.Mutex
	Sent []*Mail
}

func (l *LogMailer) Send(_ context.Context, m *Mail) (string, error) {
	if m.From == "" {
		m.From = l.From
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.Sent) >= 100 {
		l.Sent = l.Sent[1:]
	}
	l.Sent = append(l.Sent, m)
	return "not sent: the server keeps mails in the run log (--smtp log)", nil
}

// SMTPMailer sends mail through an SMTP server.
type SMTPMailer struct {
	Host, Port         string
	TLS                bool
	Username, Password string
	From               string
}

func (s *SMTPMailer) Send(ctx context.Context, m *Mail) (string, error) {
	if m.From == "" {
		m.From = s.From
	}
	from, err := mail.ParseAddress(m.From)
	if err != nil {
		return "", fmt.Errorf("the sender %q is not an email address", m.From)
	}
	var rcpt []string
	for _, a := range append(append([]string{}, m.To...), m.Cc...) {
		p, err := mail.ParseAddress(a)
		if err != nil {
			return "", fmt.Errorf("%q is not an email address", a)
		}
		rcpt = append(rcpt, p.Address)
	}
	msg, err := m.Bytes(s.Host)
	if err != nil {
		return "", err
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
		return "", fmt.Errorf("the mail server %s could not be reached: %w", addr, err)
	}
	conn.SetDeadline(time.Now().Add(60 * time.Second))
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		conn.Close()
		return "", fmt.Errorf("the mail server: %w", err)
	}
	defer c.Close()
	if !s.TLS {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: s.Host}); err != nil {
				return "", fmt.Errorf("the mail server's TLS: %w", err)
			}
		}
	}
	if s.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			return "", fmt.Errorf("signing in to the mail server failed: %w", err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return "", fmt.Errorf("the mail server refused the sender: %w", err)
	}
	for _, r := range rcpt {
		if err := c.Rcpt(r); err != nil {
			return "", fmt.Errorf("the mail server refused %s: %w", r, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return "", err
	}
	if _, err := w.Write(msg); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("the mail server refused the mail: %w", err)
	}
	c.Quit()
	return "sent through " + s.Host, nil
}

// Bytes writes the mail as a MIME message.
func (m *Mail) Bytes(host string) ([]byte, error) {
	var b bytes.Buffer
	h := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
	}
	addrs := func(list []string) string {
		out := make([]string, 0, len(list))
		for _, a := range list {
			if p, err := mail.ParseAddress(a); err == nil {
				out = append(out, p.String())
			}
		}
		return strings.Join(out, ", ")
	}
	from := m.From
	if p, err := mail.ParseAddress(m.From); err == nil {
		from = p.String()
	}
	h("From", from)
	h("To", addrs(m.To))
	h("Cc", addrs(m.Cc))
	h("Reply-To", addrs([]string{m.ReplyTo}))
	h("Subject", mime.QEncoding.Encode("utf-8", oneLine(m.Subject)))
	h("Date", time.Now().Format(time.RFC1123Z))
	h("Message-ID", "<"+strings.TrimPrefix(ids.New("m"), "m_")+"@"+host+">")
	h("MIME-Version", "1.0")

	bodyType, body := "text/plain; charset=utf-8", m.Text
	if m.HTML != "" {
		bodyType, body = "text/html; charset=utf-8", m.HTML
	}
	if len(m.Attachments) == 0 {
		h("Content-Type", bodyType)
		h("Content-Transfer-Encoding", "base64")
		b.WriteString("\r\n")
		writeBase64(&b, []byte(body))
		return b.Bytes(), nil
	}
	mw := multipart.NewWriter(&b)
	h("Content-Type", `multipart/mixed; boundary="`+mw.Boundary()+`"`)
	b.WriteString("\r\n")
	part, err := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {bodyType}, "Content-Transfer-Encoding": {"base64"}})
	if err != nil {
		return nil, err
	}
	writeBase64(part, []byte(body))
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

func writeBase64(w interface{ Write([]byte) (int, error) }, data []byte) {
	enc := base64.StdEncoding.EncodeToString(data)
	for len(enc) > 76 {
		w.Write([]byte(enc[:76] + "\r\n"))
		enc = enc[76:]
	}
	w.Write([]byte(enc + "\r\n"))
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// addresses splits "a@x.com, b@y.com" or a list into addresses.
func addresses(v any) []string {
	var out []string
	for _, item := range asList(v) {
		s := ""
		switch x := item.(type) {
		case string:
			s = x
		case map[string]any:
			s, _ = x["email"].(string)
		}
		for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}
