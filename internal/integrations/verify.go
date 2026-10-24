package integrations

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Signed is a webhook call to check.
type Signed struct {
	Header http.Header
	Body   []byte
	// URL is the full address the sender called, for Twilio, whose
	// signature covers it.
	URL string
	// Form holds the posted form values, for Twilio.
	Form url.Values
	Now  time.Time
}

// ErrSignature is a webhook call whose signature is missing or wrong.
var ErrSignature = errors.New("signature")

type sigError struct{ msg string }

func (e *sigError) Error() string        { return e.msg }
func (e *sigError) Is(target error) bool { return target == ErrSignature }

func bad(format string, args ...any) error { return &sigError{fmt.Sprintf(format, args...)} }

// Check verifies a webhook call against a secret, the way v says. The
// error says what was wrong, without giving away the expected value.
func Check(v Verify, secret string, in Signed) error {
	if v.Scheme == "" || v.Scheme == "none" {
		return nil
	}
	if secret == "" {
		return bad("the signing secret is not set, so calls cannot be checked")
	}
	if in.Now.IsZero() {
		in.Now = time.Now()
	}
	tol := time.Duration(v.Tolerance) * time.Second
	if tol <= 0 {
		tol = 5 * time.Minute
	}
	fresh := func(ts string) error {
		n, err := strconv.ParseInt(strings.TrimSpace(ts), 10, 64)
		if err != nil {
			return bad("the signature's time stamp is not a number")
		}
		if d := in.Now.Sub(time.Unix(n, 0)); math.Abs(float64(d)) > float64(tol) {
			return bad("the signature is %s old, more than the %s allowed (a replayed call, or a clock that is off)", d.Round(time.Second), tol)
		}
		return nil
	}
	switch v.Scheme {
	case "stripe":
		h := in.Header.Get("Stripe-Signature")
		if h == "" {
			return bad("the Stripe-Signature header is missing")
		}
		var ts string
		var sigs []string
		for _, part := range strings.Split(h, ",") {
			k, val, _ := strings.Cut(strings.TrimSpace(part), "=")
			switch k {
			case "t":
				ts = val
			case "v1":
				sigs = append(sigs, val)
			}
		}
		if ts == "" || len(sigs) == 0 {
			return bad("the Stripe-Signature header has no time stamp or v1 signature")
		}
		want := mac(sha256.New, []byte(secret), []byte(ts+"."), in.Body)
		if !anyEqualHex(sigs, want) {
			return bad("the Stripe signature does not match; check the endpoint's signing secret (whsec_…)")
		}
		return fresh(ts)
	case "github":
		h := in.Header.Get("X-Hub-Signature-256")
		if h == "" {
			return bad("the X-Hub-Signature-256 header is missing")
		}
		got, _ := strings.CutPrefix(h, "sha256=")
		if !anyEqualHex([]string{got}, mac(sha256.New, []byte(secret), in.Body)) {
			return bad("the GitHub signature does not match; check the webhook's secret")
		}
		return nil
	case "slack":
		ts, sig := in.Header.Get("X-Slack-Request-Timestamp"), in.Header.Get("X-Slack-Signature")
		if ts == "" || sig == "" {
			return bad("the X-Slack-Signature or X-Slack-Request-Timestamp header is missing")
		}
		got, _ := strings.CutPrefix(sig, "v0=")
		if !anyEqualHex([]string{got}, mac(sha256.New, []byte(secret), []byte("v0:"+ts+":"), in.Body)) {
			return bad("the Slack signature does not match; check the app's signing secret")
		}
		return fresh(ts)
	case "twilio":
		sig := in.Header.Get("X-Twilio-Signature")
		if sig == "" {
			return bad("the X-Twilio-Signature header is missing")
		}
		data := in.URL
		keys := make([]string, 0, len(in.Form))
		for k := range in.Form {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			for _, val := range in.Form[k] {
				data += k + val
			}
		}
		want := mac(sha1.New, []byte(secret), []byte(data))
		got, err := base64.StdEncoding.DecodeString(sig)
		if err != nil || !hmac.Equal(got, want) {
			return bad("the Twilio signature does not match; check the auth token and that the public address Pimpbase knows (--public-url) is the one Twilio calls")
		}
		return nil
	case "standard":
		id, ts, sig := in.Header.Get("Webhook-Id"), in.Header.Get("Webhook-Timestamp"), in.Header.Get("Webhook-Signature")
		if id == "" {
			id, ts, sig = in.Header.Get("Svix-Id"), in.Header.Get("Svix-Timestamp"), in.Header.Get("Svix-Signature")
		}
		if id == "" || ts == "" || sig == "" {
			return bad("the webhook-id, webhook-timestamp or webhook-signature header is missing")
		}
		key := []byte(secret)
		if b, ok := strings.CutPrefix(secret, "whsec_"); ok {
			if k, err := base64.StdEncoding.DecodeString(b); err == nil {
				key = k
			}
		}
		want := mac(sha256.New, key, []byte(id+"."+ts+"."), in.Body)
		ok := false
		for _, part := range strings.Fields(sig) {
			ver, val, _ := strings.Cut(part, ",")
			if ver != "v1" {
				continue
			}
			if got, err := base64.StdEncoding.DecodeString(val); err == nil && hmac.Equal(got, want) {
				ok = true
			}
		}
		if !ok {
			return bad("the webhook signature does not match; check the signing secret")
		}
		return fresh(ts)
	case "hmac":
		name := v.Header
		if name == "" {
			name = "X-Signature"
		}
		h := strings.TrimSpace(in.Header.Get(name))
		if h == "" {
			return bad("the %s header is missing", name)
		}
		got, ok := strings.CutPrefix(h, v.Prefix)
		if !ok {
			return bad("the %s header does not start with %q", name, v.Prefix)
		}
		var algo func() hash.Hash
		switch v.Algorithm {
		case "", "sha256":
			algo = sha256.New
		case "sha1":
			algo = sha1.New
		case "sha512":
			algo = sha512.New
		default:
			return bad("the algorithm %q is unknown", v.Algorithm)
		}
		want := mac(algo, []byte(secret), in.Body)
		var raw []byte
		var err error
		if v.Encoding == "base64" {
			raw, err = base64.StdEncoding.DecodeString(got)
		} else {
			raw, err = hex.DecodeString(strings.ToLower(got))
		}
		if err != nil || !hmac.Equal(raw, want) {
			return bad("the signature in %s does not match; check the secret, algorithm and encoding", name)
		}
		return nil
	}
	return bad("the signature check %q is unknown", v.Scheme)
}

func mac(h func() hash.Hash, key []byte, parts ...[]byte) []byte {
	m := hmac.New(h, key)
	for _, p := range parts {
		m.Write(p)
	}
	return m.Sum(nil)
}

func anyEqualHex(list []string, want []byte) bool {
	ok := false
	for _, s := range list {
		if got, err := hex.DecodeString(strings.ToLower(strings.TrimSpace(s))); err == nil && hmac.Equal(got, want) {
			ok = true
		}
	}
	return ok
}

// Sign makes the signature headers a scheme expects, for tests and for
// the builder's "send a test event".
func Sign(v Verify, secret string, body []byte, now time.Time) http.Header {
	h := http.Header{}
	ts := strconv.FormatInt(now.Unix(), 10)
	switch v.Scheme {
	case "stripe":
		h.Set("Stripe-Signature", "t="+ts+",v1="+hex.EncodeToString(mac(sha256.New, []byte(secret), []byte(ts+"."), body)))
	case "github":
		h.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac(sha256.New, []byte(secret), body)))
	case "slack":
		h.Set("X-Slack-Request-Timestamp", ts)
		h.Set("X-Slack-Signature", "v0="+hex.EncodeToString(mac(sha256.New, []byte(secret), []byte("v0:"+ts+":"), body)))
	case "standard":
		key := []byte(secret)
		if b, ok := strings.CutPrefix(secret, "whsec_"); ok {
			if k, err := base64.StdEncoding.DecodeString(b); err == nil {
				key = k
			}
		}
		id := "msg_test"
		h.Set("Webhook-Id", id)
		h.Set("Webhook-Timestamp", ts)
		h.Set("Webhook-Signature", "v1,"+base64.StdEncoding.EncodeToString(mac(sha256.New, key, []byte(id+"."+ts+"."), body)))
	case "hmac":
		var algo func() hash.Hash = sha256.New
		if v.Algorithm == "sha1" {
			algo = sha1.New
		} else if v.Algorithm == "sha512" {
			algo = sha512.New
		}
		sum := mac(algo, []byte(secret), body)
		enc := hex.EncodeToString(sum)
		if v.Encoding == "base64" {
			enc = base64.StdEncoding.EncodeToString(sum)
		}
		name := v.Header
		if name == "" {
			name = "X-Signature"
		}
		h.Set(name, v.Prefix+enc)
	}
	return h
}

// TwilioSignature signs a Twilio form post to url.
func TwilioSignature(token, url string, form url.Values) string {
	data := url
	keys := make([]string, 0, len(form))
	for k := range form {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range form[k] {
			data += k + v
		}
	}
	return base64.StdEncoding.EncodeToString(mac(sha1.New, []byte(token), []byte(data)))
}
