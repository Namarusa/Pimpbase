package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pimpbase/pimpbase/internal/auth"
	"github.com/pimpbase/pimpbase/internal/email"
	"github.com/pimpbase/pimpbase/internal/flows"
)

// Email: the workspace's delivery settings and system emails, for admins;
// apps' email templates, for builders; and the links people get by email
// (invitations, password resets, sign-in links).

func (s *Server) emailSettings(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireAdmin(r); err != nil {
		return err
	}
	st, err := s.Email.Status(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, 200, st)
}

func (s *Server) updateEmailSettings(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireAdmin(r); err != nil {
		return err
	}
	var in email.Settings
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if err := s.Email.Update(r.Context(), in); err != nil {
		return err
	}
	return s.emailSettings(w, r)
}

// testEmail sends a short mail with the saved settings, to the admin or
// the address given.
func (s *Server) testEmail(w http.ResponseWriter, r *http.Request) error {
	u, err := s.requireAdmin(r)
	if err != nil {
		return err
	}
	var in struct {
		To string `json:"to"`
	}
	readJSON(r, &in)
	to := strings.TrimSpace(in.To)
	if to == "" {
		to = u.Email
	}
	site := s.siteURL(r)
	rc, err := s.Email.Send(r.Context(), &email.Mail{To: []string{to}, Subject: "Pimpbase test email",
		Text: "This is a test from " + site + ". Mail from Pimpbase reaches you.\n",
		HTML: `<p style="font-family:sans-serif">This is a test from <a href="` + site + `">` + site + `</a>. Mail from Pimpbase reaches you.</p>`})
	if err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"to": to, "receipt": rc})
}

// System emails.

type systemEmail struct {
	Key      string          `json:"key"`
	Name     string          `json:"name"`
	Custom   bool            `json:"custom"`
	Template *email.Template `json:"template"`
}

func (s *Server) systemEmails(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireAdmin(r); err != nil {
		return err
	}
	out := []systemEmail{}
	for _, k := range email.SystemKeys {
		t, custom, err := s.Email.SystemTemplate(r.Context(), k)
		if err != nil {
			return err
		}
		out = append(out, systemEmail{k, email.SystemName(k), custom, t})
	}
	return writeJSON(w, 200, out)
}

func (s *Server) saveSystemEmail(w http.ResponseWriter, r *http.Request) error {
	u, err := s.requireAdmin(r)
	if err != nil {
		return err
	}
	var in email.Template
	if err := readJSON(r, &in); err != nil {
		return err
	}
	key := r.PathValue("key")
	// The fields of a system email are fixed: the server fills them.
	if def := email.DefaultSystem(key); def != nil {
		in.Params = def.Params
	}
	t, err := s.Email.SaveSystem(r.Context(), key, &in, u.Email)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, systemEmail{key, email.SystemName(key), true, t})
}

func (s *Server) resetSystemEmail(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireAdmin(r); err != nil {
		return err
	}
	key := r.PathValue("key")
	if email.DefaultSystem(key) == nil {
		return email.ErrNotFound
	}
	if err := s.Email.ResetSystem(r.Context(), key); err != nil {
		return err
	}
	return writeJSON(w, 200, systemEmail{key, email.SystemName(key), false, email.DefaultSystem(key)})
}

// previewRequest is a template as it stands in the editor, saved or not,
// with values for its fields; missing ones take their samples.
type previewRequest struct {
	Template email.Template `json:"template"`
	Data     map[string]any `json:"data"`
	// Send mails the preview to the signed-in builder.
	Send bool `json:"send"`
	// Mark tags each block for the editor's preview.
	Mark bool `json:"mark"`
}

func (s *Server) renderPreview(r *http.Request, in *previewRequest, app map[string]any) (*email.Rendered, error) {
	t := in.Template
	if t.Name == "" {
		t.Name = "Preview"
	}
	if err := t.Check(); err != nil {
		return nil, err
	}
	data := t.SampleData()
	for k, v := range in.Data {
		data[k] = v
	}
	if in.Mark && !in.Send {
		return t.RenderMarked(data, app)
	}
	return t.Render(data, app)
}

type previewResult struct {
	*email.Rendered
	Sent *sentTo `json:"sent,omitempty"`
}

type sentTo struct {
	To      string         `json:"to"`
	Receipt *email.Receipt `json:"receipt"`
}

// sendTest mails a preview to whoever asked, marked as a test.
func (s *Server) sendTest(r *http.Request, out *email.Rendered) (*sentTo, error) {
	u := userFrom(r)
	if u == nil {
		return nil, errLogin
	}
	rc, err := s.Email.Send(r.Context(), &email.Mail{To: []string{u.Email}, Subject: "[Test] " + out.Subject, HTML: out.HTML, Text: out.Text})
	if err != nil {
		return nil, err
	}
	return &sentTo{u.Email, rc}, nil
}

func (s *Server) previewSystemEmail(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireAdmin(r); err != nil {
		return err
	}
	var in previewRequest
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if def := email.DefaultSystem(r.PathValue("key")); def != nil {
		in.Template.Params = def.Params
		in.Data = map[string]any{"site": s.siteURL(r)}
	}
	out, err := s.renderPreview(r, &in, nil)
	if err != nil {
		return err
	}
	res := previewResult{Rendered: out}
	if in.Send {
		if res.Sent, err = s.sendTest(r, out); err != nil {
			return err
		}
	}
	return writeJSON(w, 200, res)
}

// App templates.

// templateInfo is a template in the list, with the flows that send it.
type templateInfo struct {
	*email.Template
	UsedBy []templateUse `json:"usedBy"`
}

type templateUse struct {
	FlowID   string `json:"flowId"`
	FlowName string `json:"flowName"`
	Step     string `json:"step"`
}

// templateUses finds the Send email steps of an app's flows by template.
func (s *Server) templateUses(r *http.Request, appID string) (map[string][]templateUse, error) {
	list, err := s.Flows.Flows(r.Context(), appID)
	if err != nil {
		return nil, err
	}
	out := map[string][]templateUse{}
	var walk func(f *flows.Flow, steps []*flows.Step)
	walk = func(f *flows.Flow, steps []*flows.Step) {
		for _, st := range steps {
			if st.Type == "send_email" && st.Inputs["mode"] == "template" {
				if id, _ := st.Inputs["template"].(string); id != "" {
					out[id] = append(out[id], templateUse{f.ID, f.Name, st.Name})
				}
			}
			walk(f, st.Then)
			walk(f, st.Else)
			walk(f, st.Do)
		}
	}
	for _, f := range list {
		walk(f, f.Steps)
	}
	return out, nil
}

func (s *Server) listEmailTemplates(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	list, err := s.Email.Templates(r.Context(), a.ID)
	if err != nil {
		return err
	}
	uses, err := s.templateUses(r, a.ID)
	if err != nil {
		return err
	}
	out := make([]templateInfo, 0, len(list))
	for _, t := range list {
		u := uses[t.ID]
		if u == nil {
			u = []templateUse{}
		}
		out = append(out, templateInfo{t, u})
	}
	return writeJSON(w, 200, out)
}

func (s *Server) getEmailTemplate(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	t, err := s.Email.Template(r.Context(), a.ID, r.PathValue("tpl"))
	if err != nil {
		return err
	}
	uses, err := s.templateUses(r, a.ID)
	if err != nil {
		return err
	}
	u := uses[t.ID]
	if u == nil {
		u = []templateUse{}
	}
	return writeJSON(w, 200, templateInfo{t, u})
}

func (s *Server) createEmailTemplate(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	var in email.Template
	if err := readJSON(r, &in); err != nil {
		return err
	}
	in.Key = ""
	t, err := s.Email.CreateTemplate(r.Context(), a.ID, &in, userFrom(r).Email)
	if err != nil {
		return err
	}
	return writeJSON(w, 201, templateInfo{t, []templateUse{}})
}

func (s *Server) updateEmailTemplate(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	var in email.Template
	if err := readJSON(r, &in); err != nil {
		return err
	}
	t, err := s.Email.UpdateTemplate(r.Context(), a.ID, r.PathValue("tpl"), &in, userFrom(r).Email)
	if err != nil {
		return err
	}
	return s.getEmailTemplateByID(w, r, a.ID, t.ID)
}

func (s *Server) getEmailTemplateByID(w http.ResponseWriter, r *http.Request, appID, id string) error {
	t, err := s.Email.Template(r.Context(), appID, id)
	if err != nil {
		return err
	}
	uses, err := s.templateUses(r, appID)
	if err != nil {
		return err
	}
	u := uses[t.ID]
	if u == nil {
		u = []templateUse{}
	}
	return writeJSON(w, 200, templateInfo{t, u})
}

func (s *Server) deleteEmailTemplate(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	t, err := s.Email.Template(r.Context(), a.ID, r.PathValue("tpl"))
	if err != nil {
		return err
	}
	uses, err := s.templateUses(r, a.ID)
	if err != nil {
		return err
	}
	if u := uses[t.ID]; len(u) > 0 && r.URL.Query().Get("force") != "true" {
		names := []string{}
		for _, x := range u {
			names = append(names, x.FlowName+" ("+x.Step+")")
		}
		return errStatus(409, "flows send this template: "+strings.Join(names, ", ")+"; change those steps first")
	}
	if err := s.Email.DeleteTemplate(r.Context(), a.ID, t.ID); err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) emailTemplateVersions(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	list, err := s.Email.Versions(r.Context(), a.ID, r.PathValue("tpl"))
	if err != nil {
		return err
	}
	return writeJSON(w, 200, list)
}

func versionOf(r *http.Request) (int, error) {
	v, err := strconv.Atoi(r.PathValue("version"))
	if err != nil || v < 1 {
		return 0, errStatus(400, "versions are numbered from 1")
	}
	return v, nil
}

func (s *Server) emailTemplateVersion(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	v, err := versionOf(r)
	if err != nil {
		return err
	}
	t, err := s.Email.Version(r.Context(), a.ID, r.PathValue("tpl"), v)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, t)
}

func (s *Server) restoreEmailTemplate(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	v, err := versionOf(r)
	if err != nil {
		return err
	}
	t, err := s.Email.Restore(r.Context(), a.ID, r.PathValue("tpl"), v, userFrom(r).Email)
	if err != nil {
		return err
	}
	return s.getEmailTemplateByID(w, r, a.ID, t.ID)
}

func (s *Server) previewEmailTemplate(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	var in previewRequest
	if err := readJSON(r, &in); err != nil {
		return err
	}
	out, err := s.renderPreview(r, &in, map[string]any{"id": a.ID, "name": a.Name, "slug": a.Slug})
	if err != nil {
		return err
	}
	res := previewResult{Rendered: out}
	if in.Send {
		if res.Sent, err = s.sendTest(r, out); err != nil {
			return err
		}
	}
	return writeJSON(w, 200, res)
}

// previewFlowEmail fills a Send email step with the sample values the
// builder shows for it, and can mail the result to the builder.
func (s *Server) previewFlowEmail(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	if _, err := s.Flows.Flow(r.Context(), a.ID, r.PathValue("flow")); err != nil {
		return err
	}
	var in struct {
		Inputs map[string]any `json:"inputs"`
		Scope  map[string]any `json:"scope"`
		Send   bool           `json:"send"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if in.Inputs == nil {
		in.Inputs = map[string]any{}
	}
	m, log, err := s.Engine.PreviewEmail(r.Context(), a.ID, plainJSON(in.Inputs), plainJSON(in.Scope), in.Send)
	res := map[string]any{"email": log}
	if err != nil {
		var fe *email.Error
		if log == nil && !errors.As(err, &fe) {
			return errStatus(400, err.Error())
		}
		res["error"] = err.Error()
		return writeJSON(w, 200, res)
	}
	if in.Send {
		u := userFrom(r)
		m.To, m.Cc, m.Bcc = []string{u.Email}, nil, nil
		m.Subject = "[Test] " + m.Subject
		rc, err := s.Email.Send(r.Context(), m)
		if err != nil {
			return err
		}
		res["sent"] = sentTo{u.Email, rc}
	}
	return writeJSON(w, 200, res)
}

// plainJSON turns json.Number values (readJSON keeps numbers as such)
// into float64, as flows expect.
func plainJSON(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out, _ := jsonPlain(m).(map[string]any)
	return out
}

func jsonPlain(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, y := range x {
			out[k] = jsonPlain(y)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, y := range x {
			out[i] = jsonPlain(y)
		}
		return out
	case interface{ Float64() (float64, error) }:
		f, _ := x.Float64()
		return f
	}
	return v
}

// Links sent by email.

// siteURL is the address people reach Pimpbase at.
func (s *Server) siteURL(r *http.Request) string {
	return strings.TrimSuffix(s.redirectURL(r), "/api/sso/callback")
}

// localPath keeps a return path on this site.
func localPath(p string) string {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.ContainsAny(p, "\\\r\n") {
		return ""
	}
	return p
}

// authOptions tells the sign-in page which email links it may offer.
func (s *Server) authOptions(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	return writeJSON(w, 200, map[string]any{"passwordReset": s.Email.Configured(ctx), "emailLinks": s.Email.EmailLinks(ctx)})
}

// sendLink makes a link of a kind for a user and mails it with the
// matching system email.
func (s *Server) sendLink(ctx context.Context, r *http.Request, u *auth.User, kind, next string, extra map[string]any) (*email.Receipt, error) {
	token, err := s.Auth.NewToken(ctx, u.ID, kind)
	if err != nil {
		return nil, err
	}
	site := s.siteURL(r)
	q := url.Values{"token": {token}}
	if next = localPath(next); next != "" {
		q.Set("next", next)
	}
	link, key := site+"/reset?"+q.Encode(), email.PasswordReset
	switch kind {
	case auth.TokenInvite:
		q.Set("invite", "1")
		link, key = site+"/reset?"+q.Encode(), email.Invite
	case auth.TokenSignIn:
		link, key = site+"/api/auth/link?"+q.Encode(), email.SignInLink
	}
	data := map[string]any{"name": u.Name, "email": u.Email, "link": link, "site": site, "expires": humanTTL(auth.TokenTTL[kind])}
	for k, v := range extra {
		data[k] = v
	}
	return s.Email.SendSystem(ctx, key, u.Email, data)
}

func humanTTL(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + " days"
	case d >= 2*time.Hour:
		return strconv.Itoa(int(d.Hours())) + " hours"
	case d >= time.Hour:
		return "1 hour"
	}
	return strconv.Itoa(int(d.Minutes())) + " minutes"
}

// emailLinkRequest asks for a password reset or a sign-in link. The answer
// is the same whether the address is known or not, and the mail goes out
// in the background so timing does not tell either.
func (s *Server) emailLinkRequest(w http.ResponseWriter, r *http.Request, kind string) error {
	var in struct {
		Email string `json:"email"`
		Next  string `json:"next"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	ip := s.clientIP(r)
	if s.limiter.blocked(ip) {
		return errStatus(429, "too many requests; wait a few minutes")
	}
	s.limiter.fail(ip)
	ctx := r.Context()
	if kind == auth.TokenSignIn && !s.Email.EmailLinks(ctx) {
		return errStatus(403, "signing in with an email link is switched off")
	}
	if !s.Email.Configured(ctx) {
		return email.ErrNotSetUp
	}
	addr := strings.ToLower(strings.TrimSpace(in.Email))
	if addr == "" {
		return errStatus(400, "enter your email address")
	}
	go func() {
		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		u, err := s.Auth.ByEmail(bg, addr)
		if err != nil || u.Disabled || s.Auth.RecentToken(bg, u.ID, kind, time.Minute) {
			return
		}
		if _, err := s.sendLink(bg, r, u, kind, in.Next, nil); err != nil {
			s.Log.Warn("sending an email link failed", "kind", kind, "err", err)
		}
	}()
	return writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) forgotPassword(w http.ResponseWriter, r *http.Request) error {
	return s.emailLinkRequest(w, r, auth.TokenReset)
}

func (s *Server) requestSignInLink(w http.ResponseWriter, r *http.Request) error {
	return s.emailLinkRequest(w, r, auth.TokenSignIn)
}

// linkInfo tells the reset page who a link is for.
func (s *Server) linkInfo(w http.ResponseWriter, r *http.Request) error {
	u, kind, err := s.Auth.PeekToken(r.Context(), r.URL.Query().Get("token"), auth.TokenInvite, auth.TokenReset)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"email": u.Email, "name": u.Name, "kind": kind})
}

func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	u, token, err := s.Auth.SetPasswordWithToken(r.Context(), in.Token, in.Password)
	if err != nil {
		return err
	}
	s.setCookie(w, token, int(auth.SessionTTL/time.Second))
	return writeJSON(w, 200, u)
}

// signInWithLink spends a sign-in link and goes on to the page asked for.
func (s *Server) signInWithLink(w http.ResponseWriter, r *http.Request) error {
	next := localPath(r.URL.Query().Get("next"))
	if next == "" {
		next = "/"
	}
	_, token, err := s.Auth.SignInWithToken(r.Context(), r.URL.Query().Get("token"))
	if err != nil {
		msg := url.QueryEscape(err.Error())
		http.Redirect(w, r, "/?linkError="+msg, http.StatusSeeOther)
		return nil
	}
	s.setCookie(w, token, int(auth.SessionTTL/time.Second))
	http.Redirect(w, r, next, http.StatusSeeOther)
	return nil
}

// inviteUser (re)sends an invitation, or a password reset to someone who
// has a password.
func (s *Server) inviteUser(w http.ResponseWriter, r *http.Request) error {
	me, err := s.requireAdmin(r)
	if err != nil {
		return err
	}
	u, err := s.Auth.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	if u.Disabled {
		return errStatus(400, "this account is switched off; switch it on first")
	}
	kind, extra := auth.TokenReset, map[string]any(nil)
	if !u.HasPassword {
		kind, extra = auth.TokenInvite, map[string]any{"inviter": me.Name}
	}
	rc, err := s.sendLink(r.Context(), r, u, kind, "", extra)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"kind": kind, "to": u.Email, "receipt": rc})
}
