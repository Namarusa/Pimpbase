package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var defaultClient = &http.Client{Timeout: 5 * time.Minute}

// postJSON posts body and decodes the answer into out, retrying twice on
// rate limits, overload and network failures, as the Anthropic SDK does.
func (s *Service) postJSON(ctx context.Context, url string, header http.Header, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return s.doJSON(ctx, "POST", url, header, raw, out)
}

// getJSON reads url into out, with the same retries.
func (s *Service) getJSON(ctx context.Context, url string, header http.Header, out any) error {
	return s.doJSON(ctx, "GET", url, header, nil, out)
}

func (s *Service) doJSON(ctx context.Context, method, url string, header http.Header, raw []byte, out any) error {
	client := s.HTTPClient
	if client == nil {
		client = defaultClient
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			wait := s.RetryDelay << (attempt - 1)
			if pe, ok := last.(*providerError); ok && pe.retryAfter > 0 && pe.retryAfter < 30*time.Second {
				wait = pe.retryAfter
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		var body io.Reader
		if raw != nil {
			body = bytes.NewReader(raw)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, body)
		if err != nil {
			return err
		}
		for k, v := range header {
			req.Header[k] = v
		}
		if raw != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			last = err
			continue
		}
		data, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
		res.Body.Close()
		if err != nil {
			last = err
			continue
		}
		if res.StatusCode >= 300 {
			pe := &providerError{status: res.StatusCode, message: errorMessage(data)}
			if s, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil {
				pe.retryAfter = time.Duration(s) * time.Second
			}
			last = pe
			if res.StatusCode == 429 || res.StatusCode == 408 || res.StatusCode >= 500 {
				continue
			}
			return pe
		}
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("the answer is not JSON: %w", err)
		}
		return nil
	}
	return last
}

// errorMessage finds the message in the error bodies providers send:
// {"error": {"message"}}, {"error": "…"}, {"message"} or {"detail"}.
func errorMessage(data []byte) string {
	var b struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
		Detail  any             `json:"detail"`
	}
	if json.Unmarshal(data, &b) != nil {
		t := strings.TrimSpace(string(data))
		if len(t) > 300 {
			t = t[:300] + "…"
		}
		return t
	}
	if len(b.Error) > 0 {
		var e struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(b.Error, &e) == nil && e.Message != "" {
			return e.Message
		}
		var str string
		if json.Unmarshal(b.Error, &str) == nil {
			return str
		}
	}
	if b.Message != "" {
		return b.Message
	}
	if b.Detail != nil {
		if str, ok := b.Detail.(string); ok {
			return str
		}
		d, _ := json.Marshal(b.Detail)
		return string(d)
	}
	return ""
}
