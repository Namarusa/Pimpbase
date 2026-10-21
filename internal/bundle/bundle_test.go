package bundle

import "testing"

func TestSecrets(t *testing.T) {
	cfg := `{"auth":{"type":"bearer","token":"t0k"},"db":{"host":"db","password":"pw","uri":"postgres://u:pw@db/x"},"baseUrl":"https://x"}`
	blank := blankAuth(cfg)
	if blank != `{"auth":{"token":"","type":"bearer"},"baseUrl":"https://x","db":{"host":"db","password":"","uri":""}}` {
		t.Fatalf("blankAuth = %s", blank)
	}
	if got := blankAuth(`{"db":{"password":"{{ env.DB_PASS }}"}}`); got != `{"db":{"password":"{{ env.DB_PASS }}"}}` {
		t.Errorf("binding blanked: %s", got)
	}
	if got := keepAuth(blank, cfg); got != `{"auth":{"token":"t0k","type":"bearer"},"baseUrl":"https://x","db":{"host":"db","password":"pw","uri":"postgres://u:pw@db/x"}}` {
		t.Errorf("keepAuth = %s", got)
	}
	// A value given in the file wins over the app's.
	if got := keepAuth(`{"db":{"password":"new"}}`, `{"db":{"password":"old"}}`); got != `{"db":{"password":"new"}}` {
		t.Errorf("keepAuth with a new password = %s", got)
	}
}
