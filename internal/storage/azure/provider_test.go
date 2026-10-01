package azure

import (
	"context"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConditionalUploadAndSizeFailure(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		io.Copy(io.Discard, r.Body)
		if r.Header.Get("If-None-Match") != "*" {
			t.Error("conditional commit missing")
		}
		w.Header().Set("x-ms-error-code", "ConditionNotMet")
		w.WriteHeader(412)
	}))
	defer server.Close()
	client, e := azblob.NewClientWithNoCredential(server.URL, nil)
	if e != nil {
		t.Fatal(e)
	}
	p := &Provider{Client: client, Container: "container"}
	if e = p.Put(context.Background(), "archive.dump", strings.NewReader("payload"), 7); e == nil {
		t.Fatal("conditional rejection lost")
	}
	if attempts != 1 {
		t.Fatal("unexpected commit attempts", attempts)
	}
	if e = p.Put(context.Background(), "bad-size.dump", strings.NewReader("payload"), 8); e == nil {
		t.Fatal("size mismatch accepted")
	}
	if attempts != 1 {
		t.Fatal("size failure reached commit")
	}
}
