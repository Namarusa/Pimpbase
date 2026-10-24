package email

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/pimpbase/pimpbase/internal/store"
)

// Settings are how the workspace sends mail, saved by an admin.
type Settings struct {
	// Method is smtp, sendgrid, mailgun, postmark, resend or log; empty
	// means mail is not set up.
	Method    string `json:"method"`
	FromEmail string `json:"fromEmail"`
	FromName  string `json:"fromName"`
	ReplyTo   string `json:"replyTo"`

	SMTPHost string `json:"smtpHost"`
	SMTPPort string `json:"smtpPort"`
	// SMTPSecurity is starttls (the default), tls or none.
	SMTPSecurity string `json:"smtpSecurity"`
	SMTPUser     string `json:"smtpUser"`
	SMTPPassword string `json:"smtpPassword,omitempty"`

	APIKey string `json:"apiKey,omitempty"`
	// Domain and Region are Mailgun's.
	Domain string `json:"domain"`
	Region string `json:"region"`

	// EmailLinks lets people sign in with a link sent to their address.
	EmailLinks bool `json:"emailLinks"`
}

// Status is the settings as the builder sees them: secrets left out.
type Status struct {
	Settings
	PasswordSet bool `json:"passwordSet"`
	APIKeySet   bool `json:"apiKeySet"`
	// Configured is true when mail can be sent.
	Configured bool `json:"configured"`
	// Server describes delivery set on the server (--smtp), which wins
	// over the saved settings.
	Server string `json:"server,omitempty"`
}

// Service holds the delivery settings and the email templates. It is a
// Mailer: Send uses the server's mailer when one is set, the saved
// settings otherwise.
type Service struct {
	db *store.DB
	// Server is the mailer given to the server (--smtp); it wins over the
	// saved settings.
	Server Mailer
	// HTTPClient calls the providers' APIs; BaseURL replaces their
	// addresses, for tests.
	HTTPClient *http.Client
	BaseURL    string
	now        func() time.Time
}

func NewService(s *store.Store) *Service {
	return &Service{db: s.Meta, now: time.Now}
}

const settingsKey = "email"

func (s *Service) Settings(ctx context.Context) (*Settings, error) {
	var v string
	err := s.db.QueryRow(ctx, `SELECT value FROM settings WHERE key = ?`, settingsKey).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return &Settings{}, nil
	}
	if err != nil {
		return nil, err
	}
	var st Settings
	if err := json.Unmarshal([]byte(v), &st); err != nil {
		return &Settings{}, nil
	}
	return &st, nil
}

func (s *Service) Status(ctx context.Context) (*Status, error) {
	st, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	out := &Status{Settings: *st, PasswordSet: st.SMTPPassword != "", APIKeySet: st.APIKey != ""}
	out.SMTPPassword, out.APIKey = "", ""
	if s.Server != nil {
		out.Server = describe(s.Server)
		out.Configured = true
	} else {
		_, err := s.mailer(st)
		out.Configured = err == nil
	}
	return out, nil
}

func describe(m Mailer) string {
	switch x := m.(type) {
	case *SMTPMailer:
		return "SMTP server " + x.Host + ":" + x.Port + ", from " + x.From
	case *LogMailer:
		return "log only (--smtp log): mails are kept in run logs, not sent"
	}
	return "set on the server"
}

// Update saves new settings. Blank secrets keep the saved ones.
func (s *Service) Update(ctx context.Context, in Settings) error {
	cur, err := s.Settings(ctx)
	if err != nil {
		return err
	}
	in.Method = strings.ToLower(strings.TrimSpace(in.Method))
	if _, ok := ProviderNames[in.Method]; !ok && in.Method != "" {
		return errorf("choose how mail is sent")
	}
	in.FromEmail = strings.TrimSpace(in.FromEmail)
	in.FromName = strings.TrimSpace(in.FromName)
	in.ReplyTo = strings.TrimSpace(in.ReplyTo)
	if in.FromEmail != "" {
		if a, err := mail.ParseAddress(in.FromEmail); err != nil || a.Name != "" {
			return errorf("the sender %q is not an email address", in.FromEmail)
		}
	}
	if in.ReplyTo != "" {
		if _, err := mail.ParseAddress(in.ReplyTo); err != nil {
			return errorf("the reply-to %q is not an email address", in.ReplyTo)
		}
	}
	if in.SMTPPassword == "" {
		in.SMTPPassword = cur.SMTPPassword
	}
	// A key belongs to its provider: switching provider drops it.
	if in.APIKey == "" && in.Method == cur.Method {
		in.APIKey = cur.APIKey
	}
	in.SMTPHost = strings.TrimSpace(in.SMTPHost)
	in.SMTPPort = strings.TrimSpace(in.SMTPPort)
	if in.SMTPPort != "" {
		if p, err := strconv.Atoi(in.SMTPPort); err != nil || p < 1 || p > 65535 {
			return errorf("the port is a number from 1 to 65535")
		}
	}
	switch in.SMTPSecurity {
	case "", "starttls", "tls", "none":
	default:
		return errorf("security is STARTTLS, TLS or none")
	}
	in.Domain = strings.TrimSpace(in.Domain)
	js, err := json.Marshal(in)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		settingsKey, string(js), s.now().UTC().Format(time.RFC3339))
	return err
}

// Sender is the From address mails without one get, for logs and
// previews; empty when mail is not set up.
func (s *Service) Sender(ctx context.Context) string {
	switch m := s.Server.(type) {
	case *SMTPMailer:
		return m.From
	case *LogMailer:
		return m.From
	case *APIMailer:
		return m.From
	}
	st, err := s.Settings(ctx)
	if err != nil {
		return ""
	}
	return st.sender()
}

func (st *Settings) sender() string {
	if st.FromEmail != "" && st.FromName != "" {
		return (&mail.Address{Name: st.FromName, Address: st.FromEmail}).String()
	}
	return st.FromEmail
}

// mailer builds the mailer the settings describe.
func (s *Service) mailer(st *Settings) (Mailer, error) {
	from := st.sender()
	switch st.Method {
	case "":
		return nil, ErrNotSetUp
	case "log":
		return &LogMailer{From: from, Log: slog.Default()}, nil
	}
	if from == "" {
		return nil, errorf("email needs a sender address: set it in Users → Email")
	}
	switch st.Method {
	case "smtp":
		if st.SMTPHost == "" {
			return nil, errorf("email needs the SMTP server's address: set it in Users → Email")
		}
		m := &SMTPMailer{Host: st.SMTPHost, Port: st.SMTPPort, TLS: st.SMTPSecurity == "tls", Plain: st.SMTPSecurity == "none",
			Username: st.SMTPUser, Password: st.SMTPPassword, From: from}
		if m.Port == "" {
			m.Port = "587"
			if m.TLS {
				m.Port = "465"
			}
		}
		return m, nil
	default:
		if st.APIKey == "" {
			return nil, errorf("%s needs an API key: add it in Users → Email", ProviderNames[st.Method])
		}
		return &APIMailer{Provider: st.Method, APIKey: st.APIKey, Domain: st.Domain, Region: st.Region, From: from, Client: s.HTTPClient, BaseURL: s.BaseURL}, nil
	}
}

// Send delivers a mail with the server's mailer or the saved settings,
// filling in the default sender and reply-to.
func (s *Service) Send(ctx context.Context, m *Mail) (*Receipt, error) {
	if s.Server != nil {
		return s.Server.Send(ctx, m)
	}
	st, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	mm, err := s.mailer(st)
	if err != nil {
		return nil, err
	}
	if m.ReplyTo == "" {
		m.ReplyTo = st.ReplyTo
	}
	return mm.Send(ctx, m)
}

// Configured is true when mail can be sent.
func (s *Service) Configured(ctx context.Context) bool {
	if s.Server != nil {
		return true
	}
	st, err := s.Settings(ctx)
	if err != nil {
		return false
	}
	_, err = s.mailer(st)
	return err == nil
}

// EmailLinks is true when people may sign in with an emailed link.
func (s *Service) EmailLinks(ctx context.Context) bool {
	st, err := s.Settings(ctx)
	return err == nil && st.EmailLinks && s.Configured(ctx)
}
