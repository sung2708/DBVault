package gcs

import (
	sdk "cloud.google.com/go/storage"
	"context"
	"fmt"
	"google.golang.org/api/option"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUploadRequiresAbsentGeneration(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		io.Copy(io.Discard, r.Body)
		if r.URL.Query().Get("ifGenerationMatch") != "0" {
			t.Error("absent-generation condition missing", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(412)
		fmt.Fprint(w, `{"error":{"code":412,"message":"object already exists"}}`)
	}))
	defer server.Close()
	client, e := sdk.NewClient(context.Background(), option.WithEndpoint(server.URL+"/storage/v1/"), option.WithoutAuthentication(), option.WithHTTPClient(server.Client()), sdk.WithJSONReads())
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	p := &Provider{Client: client, Bucket: "bucket"}
	if e = p.Put(context.Background(), "archive.dump", strings.NewReader("payload"), 7); e == nil {
		t.Fatal("conditional rejection lost")
	}
	if requests == 0 {
		t.Fatal("upload never attempted")
	}
}
