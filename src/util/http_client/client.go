package http_client

import (
	"context"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/GMWalletApp/epusdt/util/security"
	"github.com/go-resty/resty/v2"
)

const MaxCallbackResponseBytes int64 = 64 * 1024

// ClientFactory is overridden in tests to stub outbound HTTP calls.
var ClientFactory = resty.New

// CallbackClientFactory is overridden by tests that use local HTTP servers.
var CallbackClientFactory = newSecureCallbackHTTPClient

// GetHttpClient 获取请求客户端
func GetHttpClient(proxys ...string) *resty.Client {
	client := ClientFactory()
	// 如果有代理
	if len(proxys) > 0 {
		proxy := proxys[0]
		client.SetProxy(proxy)
	}
	client.SetTimeout(time.Second * 10)
	return client
}

// GetCallbackHTTPClient returns the restricted client used for merchant
// callbacks. It does not honor proxy settings or redirects and pins each TCP
// connection to the public IP resolved by the URL guard.
func GetCallbackHTTPClient() *resty.Client {
	return CallbackClientFactory()
}

func newSecureCallbackHTTPClient() *resty.Client {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		resolved, err := security.ResolvePublicTCPAddress(ctx, address)
		if err != nil {
			return nil, err
		}
		return dialer.DialContext(ctx, network, resolved)
	}

	client := resty.NewWithClient(&http.Client{
		Transport: &limitedResponseTransport{base: base, maxBytes: MaxCallbackResponseBytes},
		Timeout:   10 * time.Second,
	})
	client.SetRedirectPolicy(resty.NoRedirectPolicy())
	return client
}

type limitedResponseTransport struct {
	base     http.RoundTripper
	maxBytes int64
}

func (t *limitedResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &limitedReadCloser{
		Reader: io.LimitReader(resp.Body, t.maxBytes+1),
		Closer: resp.Body,
	}
	return resp, nil
}

type limitedReadCloser struct {
	io.Reader
	io.Closer
}
