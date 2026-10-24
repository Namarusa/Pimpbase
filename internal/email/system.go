package email

import (
	"context"
	"errors"
)

// System emails are the workspace's own: invitations, password resets and
// sign-in links. Each has a built-in template that an admin may change in
// Users → Email; a changed one is saved with the app id "" and its key as
// name.

const (
	Invite        = "invite"
	PasswordReset = "password_reset"
	SignInLink    = "sign_in_link"
)

// SystemKeys lists the system emails in the order the builder shows them.
var SystemKeys = []string{Invite, PasswordReset, SignInLink}

var systemNames = map[string]string{Invite: "Invitation", PasswordReset: "Password reset", SignInLink: "Sign-in link"}

func systemParams(expires string) []Param {
	return []Param{
		{Name: "name", Type: "text", Description: "the person's name", Sample: "Kim Lee"},
		{Name: "email", Type: "text", Description: "the person's email address", Sample: "kim@example.com"},
		{Name: "link", Type: "text", Description: "the link to open", Sample: "https://apps.example.com/reset?token=sample"},
		{Name: "expires", Type: "text", Description: "how long the link works", Sample: expires},
		{Name: "site", Type: "text", Description: "the address of this Pimpbase", Sample: "https://apps.example.com"},
	}
}

// DefaultSystem is the built-in template of a system email.
func DefaultSystem(key string) *Template {
	foot := "You get this email because of an account at {{ site }}."
	t := &Template{AppID: "", Name: key, Key: key, Mode: "blocks", Style: Style{Brand: "#2563eb", Footer: foot}}
	switch key {
	case Invite:
		t.Description = "Sent when an admin adds someone and invites them by email."
		t.Subject = "You are invited to Pimpbase"
		t.Preheader = "{{ inviter }} added you. Choose a password to get started."
		t.Params = append(systemParams("7 days"), Param{Name: "inviter", Type: "text", Description: "who sent the invitation", Sample: "Nassim"})
		t.Blocks = []Block{
			{ID: "b1", Type: "heading", Level: 1, Text: "Welcome, {{ name | default:\"there\" }}"},
			{ID: "b2", Type: "text", Text: "**{{ inviter }}** invited you to the apps at {{ site }}.\n\nChoose a password to finish setting up your account. You sign in with **{{ email }}**."},
			{ID: "b3", Type: "button", Text: "Choose a password", URL: "{{ link }}"},
			{ID: "b4", Type: "text", Text: "The link works for {{ expires }}. If the button does not work, paste this address into your browser:\n{{ link }}"},
		}
	case PasswordReset:
		t.Description = "Sent when someone asks to reset a forgotten password."
		t.Subject = "Reset your Pimpbase password"
		t.Preheader = "Choose a new password. The link works for {{ expires }}."
		t.Params = systemParams("1 hour")
		t.Blocks = []Block{
			{ID: "b1", Type: "heading", Level: 1, Text: "Reset your password"},
			{ID: "b2", Type: "text", Text: "Hi {{ name | default:\"there\" }}, someone asked to reset the password of **{{ email }}**. Choose a new one with the button below."},
			{ID: "b3", Type: "button", Text: "Choose a new password", URL: "{{ link }}"},
			{ID: "b4", Type: "text", Text: "The link works once, for {{ expires }}. If you did not ask for this, ignore this email: your password stays as it is."},
		}
	case SignInLink:
		t.Description = "Sent when someone signs in with a link instead of a password."
		t.Subject = "Your Pimpbase sign-in link"
		t.Preheader = "Sign in with one click. The link works for {{ expires }}."
		t.Params = systemParams("15 minutes")
		t.Blocks = []Block{
			{ID: "b1", Type: "heading", Level: 1, Text: "Sign in to Pimpbase"},
			{ID: "b2", Type: "text", Text: "Hi {{ name | default:\"there\" }}, press the button to sign in as **{{ email }}**."},
			{ID: "b3", Type: "button", Text: "Sign in", URL: "{{ link }}"},
			{ID: "b4", Type: "text", Text: "The link works once, for {{ expires }}. If you did not ask for it, ignore this email."},
		}
	default:
		return nil
	}
	return t
}

// SystemName is a system email's display name.
func SystemName(key string) string { return systemNames[key] }

// SystemTemplate is a system email's template: the saved one, or the
// built-in one. custom tells which.
func (s *Service) SystemTemplate(ctx context.Context, key string) (t *Template, custom bool, err error) {
	def := DefaultSystem(key)
	if def == nil {
		return nil, false, ErrNotFound
	}
	saved, err := s.Template(ctx, "", key)
	if errors.Is(err, ErrNotFound) {
		return def, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return saved, true, nil
}

// SaveSystem saves a changed system email.
func (s *Service) SaveSystem(ctx context.Context, key string, in *Template, by string) (*Template, error) {
	if DefaultSystem(key) == nil {
		return nil, ErrNotFound
	}
	in.Name, in.Key = key, key
	if _, err := s.Template(ctx, "", key); errors.Is(err, ErrNotFound) {
		return s.CreateTemplate(ctx, "", in, by)
	}
	return s.UpdateTemplate(ctx, "", key, in, by)
}

// ResetSystem goes back to the built-in template.
func (s *Service) ResetSystem(ctx context.Context, key string) error {
	err := s.DeleteTemplate(ctx, "", key)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// SendSystem renders a system email and sends it to one address.
func (s *Service) SendSystem(ctx context.Context, key, to string, data map[string]any) (*Receipt, error) {
	t, _, err := s.SystemTemplate(ctx, key)
	if err != nil {
		return nil, err
	}
	r, err := t.Render(data, nil)
	if err != nil {
		return nil, err
	}
	return s.Send(ctx, &Mail{To: []string{to}, Subject: r.Subject, HTML: r.HTML, Text: r.Text})
}
