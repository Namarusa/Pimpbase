package files

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Blobs keeps file contents outside the app's database: in a folder (which
// several servers may share, such as an NFS mount) or in an S3-compatible
// bucket. Without one, contents live in the database.
type Blobs interface {
	// Name is what file rows record to find the content again: "dir" or
	// "s3".
	Name() string
	Put(ctx context.Context, key, contentType string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
}

// OpenBlobs reads a storage setting: "" or "db" keeps files in the
// database, "dir:/path" in a folder, and
// "s3://bucket/prefix?region=…&endpoint=…&path_style=true" in a bucket,
// with keys from AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY (and
// AWS_SESSION_TOKEN when set).
func OpenBlobs(spec string) (Blobs, error) {
	switch {
	case spec == "" || spec == "db":
		return nil, nil
	case strings.HasPrefix(spec, "dir:"):
		root := strings.TrimPrefix(spec, "dir:")
		if root == "" {
			return nil, errors.New("name the folder: dir:/path/to/files")
		}
		if err := os.MkdirAll(root, 0o755); err != nil {
			return nil, err
		}
		return &DirBlobs{Root: root}, nil
	case strings.HasPrefix(spec, "s3://"):
		return parseS3(spec)
	}
	return nil, fmt.Errorf("unknown file storage %q: use db, dir:/path or s3://bucket", spec)
}

// DirBlobs keeps contents as files under Root, one folder per app.
type DirBlobs struct{ Root string }

func (d *DirBlobs) Name() string { return "dir" }

func (d *DirBlobs) path(key string) (string, error) {
	clean := filepath.Clean("/" + key)
	if clean != "/"+key || strings.Contains(key, "..") {
		return "", fmt.Errorf("bad file key %q", key)
	}
	return filepath.Join(d.Root, filepath.FromSlash(key)), nil
}

func (d *DirBlobs) Put(_ context.Context, key, _ string, data []byte) error {
	p, err := d.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".upload-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	return os.Rename(tmp.Name(), p)
}

func (d *DirBlobs) Get(_ context.Context, key string) ([]byte, error) {
	p, err := d.path(key)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return data, err
}

func (d *DirBlobs) Delete(_ context.Context, key string) error {
	p, err := d.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// S3Blobs keeps contents in an S3-compatible bucket (AWS S3, MinIO,
// Cloudflare R2, Backblaze B2 and others), signed with AWS Signature V4.
type S3Blobs struct {
	Endpoint  *url.URL // e.g. https://s3.eu-west-1.amazonaws.com
	Bucket    string
	Prefix    string
	Region    string
	PathStyle bool
	AccessKey string
	SecretKey string
	Token     string
	Client    *http.Client
	now       func() time.Time
}

func parseS3(spec string) (*S3Blobs, error) {
	u, err := url.Parse(spec)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	b := &S3Blobs{
		Bucket: u.Host, Prefix: strings.Trim(u.Path, "/"), Region: q.Get("region"),
		PathStyle: q.Get("path_style") == "true",
		AccessKey: os.Getenv("AWS_ACCESS_KEY_ID"), SecretKey: os.Getenv("AWS_SECRET_ACCESS_KEY"), Token: os.Getenv("AWS_SESSION_TOKEN"),
		Client: &http.Client{Timeout: 2 * time.Minute}, now: time.Now,
	}
	if b.Bucket == "" {
		return nil, errors.New("name the bucket: s3://bucket/prefix")
	}
	if b.Region == "" {
		b.Region = "us-east-1"
	}
	ep := q.Get("endpoint")
	if ep == "" {
		ep = "https://s3." + b.Region + ".amazonaws.com"
	}
	if b.Endpoint, err = url.Parse(ep); err != nil || b.Endpoint.Host == "" {
		return nil, fmt.Errorf("the S3 endpoint %q is not an address", ep)
	}
	if b.AccessKey == "" || b.SecretKey == "" {
		return nil, errors.New("S3 file storage needs AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY")
	}
	return b, nil
}

func (b *S3Blobs) Name() string { return "s3" }

func (b *S3Blobs) objectURL(key string) *url.URL {
	if b.Prefix != "" {
		key = b.Prefix + "/" + key
	}
	u := *b.Endpoint
	path := "/" + uriEncode(key, false)
	if b.PathStyle {
		path = "/" + b.Bucket + path
	} else {
		u.Host = b.Bucket + "." + u.Host
	}
	u.Path, u.RawPath = "", path
	u.Opaque = ""
	return &u
}

func (b *S3Blobs) do(ctx context.Context, method, key, contentType string, body []byte) (*http.Response, error) {
	u := b.objectURL(key)
	req, err := http.NewRequestWithContext(ctx, method, u.Scheme+"://"+u.Host+u.RawPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	b.sign(req, body)
	return b.Client.Do(req)
}

func (b *S3Blobs) Put(ctx context.Context, key, contentType string, data []byte) error {
	res, err := b.do(ctx, http.MethodPut, key, contentType, data)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return s3Error("storing", res)
	}
	return nil
}

func (b *S3Blobs) Get(ctx context.Context, key string) ([]byte, error) {
	res, err := b.do(ctx, http.MethodGet, key, "", nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if res.StatusCode/100 != 2 {
		return nil, s3Error("reading", res)
	}
	return io.ReadAll(io.LimitReader(res.Body, MaxSize+1))
}

func (b *S3Blobs) Delete(ctx context.Context, key string) error {
	res, err := b.do(ctx, http.MethodDelete, key, "", nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 && res.StatusCode != http.StatusNotFound {
		return s3Error("deleting", res)
	}
	return nil
}

func s3Error(what string, res *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	return fmt.Errorf("S3 answered %s while %s a file: %s", res.Status, what, strings.TrimSpace(string(body)))
}

// sign adds AWS Signature Version 4 headers for service "s3".
func (b *S3Blobs) sign(req *http.Request, body []byte) {
	now := time.Now
	if b.now != nil {
		now = b.now
	}
	t := now().UTC()
	amzDate := t.Format("20060102T150405Z")
	day := t.Format("20060102")
	sum := sha256.Sum256(body)
	payload := hex.EncodeToString(sum[:])
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payload)
	if b.Token != "" {
		req.Header.Set("X-Amz-Security-Token", b.Token)
	}
	req.Header.Set("Authorization", signature(req, payload, b.AccessKey, b.SecretKey, b.Region, day, amzDate))
}

func signature(req *http.Request, payload, accessKey, secretKey, region, day, amzDate string) string {
	headers := map[string]string{"host": req.URL.Host}
	for k, v := range req.Header {
		lk := strings.ToLower(k)
		if lk == "content-type" || lk == "range" || strings.HasPrefix(lk, "x-amz-") {
			headers[lk] = strings.TrimSpace(strings.Join(v, ","))
		}
	}
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, k := range names {
		canonHeaders.WriteString(k + ":" + headers[k] + "\n")
	}
	signed := strings.Join(names, ";")
	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	canonical := strings.Join([]string{req.Method, path, canonicalQuery(req.URL.Query()), canonHeaders.String(), signed, payload}, "\n")
	scope := day + "/" + region + "/s3/aws4_request"
	h := sha256.Sum256([]byte(canonical))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(h[:])
	key := hmacSHA([]byte("AWS4"+secretKey), day)
	key = hmacSHA(key, region)
	key = hmacSHA(key, "s3")
	key = hmacSHA(key, "aws4_request")
	sig := hex.EncodeToString(hmacSHA(key, toSign))
	return "AWS4-HMAC-SHA256 Credential=" + accessKey + "/" + scope + ", SignedHeaders=" + signed + ", Signature=" + sig
}

func canonicalQuery(q url.Values) string {
	var parts []string
	for k, vs := range q {
		for _, v := range vs {
			parts = append(parts, uriEncode(k, true)+"="+uriEncode(v, true))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "&")
}

func hmacSHA(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// uriEncode escapes as SigV4 asks: everything but unreserved characters,
// and "/" too when slash is true.
func uriEncode(s string, slash bool) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == '/' && !slash:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
