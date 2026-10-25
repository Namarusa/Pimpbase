package queries

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckAddress(t *testing.T) {
	for in, want := range map[string]string{
		"":                                     "",
		"http://192.168.1.10:8080/api":         "http://192.168.1.10:8080/api",
		"192.168.1.10:8080":                    "http://192.168.1.10:8080",
		"localhost:3000/v1":                    "http://localhost:3000/v1",
		"http://{{ env.HOST }}:{{ env.PORT }}": "http://{{ env.HOST }}:{{ env.PORT }}",
		"{{ env.SCHEME }}://{{ env.HOST }}/v1": "{{ env.SCHEME }}://{{ env.HOST }}/v1",
		"https://api.example.com/{{ env.V }}":  "https://api.example.com/{{ env.V }}",
	} {
		got, err := CheckAddress(in)
		if err != nil || got != want {
			t.Errorf("CheckAddress(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"ftp://x", "ftp://{{ env.HOST }}", "/just/a/path", "http://"} {
		if _, err := CheckAddress(in); err == nil {
			t.Errorf("CheckAddress(%q) passed", in)
		}
	}
}

func TestParseAddress(t *testing.T) {
	for in, want := range map[string]string{
		"http://10.0.0.5:8080/x": "http://10.0.0.5:8080/x",
		"10.0.0.5:8080/x?a=1":    "http://10.0.0.5:8080/x?a=1",
		"https://[::1]:8443/":    "https://[::1]:8443/",
	} {
		u, err := ParseAddress(in)
		if err != nil || u.String() != want {
			t.Errorf("ParseAddress(%q) = %v, %v", in, u, err)
		}
	}
	for _, in := range []string{"/items", "ftp://x/y", "http:///x"} {
		if _, err := ParseAddress(in); err == nil {
			t.Errorf("ParseAddress(%q) passed", in)
		}
	}
}

func TestNetPolicy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer srv.Close()
	get := func(p NetPolicy) error {
		res, err := (&http.Client{Transport: p.Transport()}).Get(srv.URL)
		if err == nil {
			res.Body.Close()
		}
		return err
	}
	if err := get(NetPolicy{}); err != nil {
		t.Fatalf("default policy blocked localhost: %v", err)
	}
	err := get(NetPolicy{BlockPrivate: true})
	if err == nil || !strings.Contains(err.Error(), "--block-private-networks") {
		t.Fatalf("blocking policy let localhost through: %v", err)
	}
	p, err := ParseNetPolicy(true, "127.0.0.0/8, 10.1.2.3")
	if err != nil || len(p.Allow) != 2 {
		t.Fatalf("ParseNetPolicy = %v, %v", p, err)
	}
	if err := get(p); err != nil {
		t.Fatalf("allowed network blocked: %v", err)
	}
	if _, err := ParseNetPolicy(true, "intranet"); err == nil {
		t.Error("ParseNetPolicy took a name")
	}
}
