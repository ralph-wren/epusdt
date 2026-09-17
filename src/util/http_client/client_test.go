package http_client

import (
	"net/http"
	"testing"
)

func TestCallbackClientRejectsRedirects(t *testing.T) {
	client := newSecureCallbackHTTPClient().GetClient()
	if client.CheckRedirect == nil {
		t.Fatal("callback client has no redirect policy")
	}
	if err := client.CheckRedirect(&http.Request{}, []*http.Request{{}}); err == nil {
		t.Fatal("callback client followed redirect, want error")
	}
}
