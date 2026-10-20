package files

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bubibase/bubibase/internal/apps"
	"github.com/bubibase/bubibase/internal/store"
	"github.com/bubibase/bubibase/internal/store/storetest"
)

// The example from AWS's Signature Version 4 documentation for S3
// ("GET Object", single chunk).
func TestSigV4Example(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Range", "bytes=0-9")
	req.Header.Set("X-Amz-Content-Sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	req.Header.Set("X-Amz-Date", "20130524T000000Z")
	got := signature(req, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1", "20130524", "20130524T000000Z")
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got != want {
		t.Errorf("signature\n got %s\nwant %s", got, want)
	}
}

// fakeS3 keeps objects in memory and insists on signed requests.
func fakeS3(t *testing.T) (*httptest.Server, map[string][]byte) {
	var mu sync.Mutex
	objects := map[string][]byte{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=AK/") || r.Header.Get("X-Amz-Content-Sha256") == "" {
			http.Error(w, "unsigned", 403)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case "PUT":
			b, _ := io.ReadAll(r.Body)
			objects[r.URL.Path] = b
		case "GET":
			b, ok := objects[r.URL.Path]
			if !ok {
				http.Error(w, "<Error><Code>NoSuchKey</Code></Error>", 404)
				return
			}
			w.Write(b)
		case "DELETE":
			delete(objects, r.URL.Path)
			w.WriteHeader(204)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, objects
}

func TestBlobStores(t *testing.T) {
	srv, objects := fakeS3(t)
	t.Setenv("AWS_ACCESS_KEY_ID", "AK")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "SK")
	s3, err := OpenBlobs("s3://bucket/some/prefix?path_style=true&region=eu-west-1&endpoint=" + url.QueryEscape(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	dir, err := OpenBlobs("dir:" + t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if db, err := OpenBlobs("db"); db != nil || err != nil {
		t.Errorf("db = %v, %v", db, err)
	}
	if _, err := OpenBlobs("ftp://x"); err == nil {
		t.Error("ftp accepted")
	}
	for _, b := range []Blobs{s3, dir} {
		ctx := context.Background()
		if err := b.Put(ctx, "app_1/fil_a b", "text/plain", []byte("hello")); err != nil {
			t.Fatalf("%s put: %v", b.Name(), err)
		}
		got, err := b.Get(ctx, "app_1/fil_a b")
		if err != nil || string(got) != "hello" {
			t.Errorf("%s get = %q, %v", b.Name(), got, err)
		}
		if err := b.Delete(ctx, "app_1/fil_a b"); err != nil {
			t.Errorf("%s delete: %v", b.Name(), err)
		}
		if _, err := b.Get(ctx, "app_1/fil_a b"); err != ErrNotFound {
			t.Errorf("%s get after delete: %v", b.Name(), err)
		}
	}
	if _, err := dir.Get(context.Background(), "../etc/passwd"); err == nil || err == ErrNotFound {
		t.Errorf("dir escaped: %v", err)
	}
	_ = objects
}

// TestServiceBlobs keeps contents outside the database and still reads
// files saved before the switch.
func TestServiceBlobs(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		a, err := apps.NewService(s).Create(ctx, "Docs", "")
		if err != nil {
			t.Fatal(err)
		}
		svc := NewService(s)
		svc.now = func() time.Time { return time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC) }
		old, err := svc.Save(ctx, a.ID, "tbl_x", "usr_1", "old.txt", "text/plain", strings.NewReader("in the database"))
		if err != nil {
			t.Fatal(err)
		}
		root := t.TempDir()
		svc.Blobs = &DirBlobs{Root: root}
		f, err := svc.Save(ctx, a.ID, "tbl_x", "usr_1", "new.pdf", "", bytes.NewReader([]byte("%PDF-1.4 tiny")))
		if err != nil {
			t.Fatal(err)
		}
		for id, want := range map[string]string{old.ID: "in the database", f.ID: "%PDF-1.4 tiny"} {
			_, data, err := svc.Get(ctx, a.ID, id)
			if err != nil || string(data) != want {
				t.Errorf("%s = %q, %v", id, data, err)
			}
		}
		if n, size, err := svc.Usage(ctx, a.ID); err != nil || n != 2 || size != int64(len("in the database")+len("%PDF-1.4 tiny")) {
			t.Errorf("usage = %d %d %v", n, size, err)
		}
		if err := svc.DeleteApp(ctx, a.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := (&DirBlobs{Root: root}).Get(ctx, a.ID+"/"+f.ID); err != ErrNotFound {
			t.Errorf("content left after app delete: %v", err)
		}
		svc.Blobs = nil
		if _, _, err := svc.Get(ctx, a.ID, f.ID); err == nil || !strings.Contains(err.Error(), "dir storage") {
			t.Errorf("read without storage: %v", err)
		}
	})
}
