package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestMultipartConditionalCommitAndAbort(t *testing.T) {
	var failed atomic.Bool
	var aborts, commits, parts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		q := r.URL.Query()
		w.Header().Set("Content-Type", "application/xml")
		switch {
		case r.Method == "POST" && q.Has("uploads"):
			fmt.Fprint(w, `<InitiateMultipartUploadResult><UploadId>owned-upload</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == "PUT" && q.Has("partNumber"):
			parts.Add(1)
			if failed.Load() {
				w.WriteHeader(400)
				fmt.Fprint(w, `<Error><Code>InvalidRequest</Code><Message>injected upload failure</Message></Error>`)
			} else {
				w.Header().Set("ETag", `"part"`)
			}
		case r.Method == "POST" && q.Has("uploadId"):
			if r.Header.Get("If-None-Match") != "*" {
				t.Error("conditional multipart commit missing")
			}
			commits.Add(1)
			fmt.Fprint(w, `<CompleteMultipartUploadResult><ETag>final</ETag></CompleteMultipartUploadResult>`)
		case r.Method == "DELETE":
			aborts.Add(1)
			w.WriteHeader(204)
		default:
			if r.Header.Get("If-None-Match") != "*" {
				t.Error("conditional put missing")
			}
			w.WriteHeader(412)
			fmt.Fprint(w, `<Error><Code>PreconditionFailed</Code></Error>`)
		}
	}))
	defer server.Close()
	client := sdk.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""), HTTPClient: server.Client()}, func(o *sdk.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
		o.RetryMaxAttempts = 1
	})
	p := &Provider{Client: client, Bucket: "bucket", PartSize: 5 << 20}
	data := bytes.Repeat([]byte("x"), 11<<20)
	if e := p.Put(context.Background(), "backup.archive", bytes.NewReader(data), -1); e != nil {
		t.Fatal(e)
	}
	if parts.Load() != 3 || commits.Load() != 1 {
		t.Fatal("multipart stream incomplete", parts.Load(), commits.Load())
	}
	failed.Store(true)
	if e := p.Put(context.Background(), "failed.archive", bytes.NewReader(data), -1); e == nil {
		t.Fatal("upload failure lost")
	}
	if aborts.Load() != 1 {
		t.Fatal("failed multipart not aborted")
	}
	if e := p.Put(context.Background(), "existing.meta.json", bytes.NewReader([]byte("metadata")), 8); e == nil {
		t.Fatal("precondition failure lost")
	}
}

func TestCancellationAbortsWithFreshContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var aborts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		q := r.URL.Query()
		w.Header().Set("Content-Type", "application/xml")
		switch {
		case r.Method == "POST" && q.Has("uploads"):
			fmt.Fprint(w, `<InitiateMultipartUploadResult><UploadId>cancelled-upload</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == "PUT":
			cancel()
			w.Header().Set("ETag", `"part"`)
		case r.Method == "DELETE":
			aborts.Add(1)
			w.WriteHeader(204)
		default:
			t.Error("unexpected request after cancellation", r.Method)
			w.WriteHeader(400)
		}
	}))
	defer server.Close()
	client := sdk.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""), HTTPClient: server.Client()}, func(o *sdk.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
		o.RetryMaxAttempts = 1
	})
	p := &Provider{Client: client, Bucket: "bucket", PartSize: 5 << 20}
	e := p.Put(ctx, "cancelled.archive", bytes.NewReader(bytes.Repeat([]byte("x"), 11<<20)), -1)
	if !errors.Is(e, context.Canceled) {
		t.Fatal("cancellation lost", e)
	}
	if aborts.Load() != 1 {
		t.Fatal("cancelled upload was not aborted", aborts.Load())
	}
}
