package ingress

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestCaddyClientRegisterAndRemoveRoute(t *testing.T) {
	var methods []string
	var paths []string

	c := NewCaddyClient("http://caddy.local:2019")
	c.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		methods = append(methods, r.Method)
		paths = append(paths, r.URL.Path)
		status := http.StatusOK
		if r.Method == http.MethodPatch {
			// Simulate route not yet existing on first call
			status = http.StatusNotFound
		}
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})

	if err := c.RegisterRoute(context.Background(), "demo", 40005); err != nil {
		t.Fatalf("RegisterRoute failed: %v", err)
	}
	if err := c.RemoveRoute(context.Background(), "demo"); err != nil {
		t.Fatalf("RemoveRoute failed: %v", err)
	}

	if len(methods) != 3 ||
		methods[0] != http.MethodPatch || paths[0] != "/id/gantry-app-demo" ||
		methods[1] != http.MethodPut || paths[1] != "/config/apps/http/servers/srv0/routes/0" ||
		methods[2] != http.MethodDelete || paths[2] != "/id/gantry-app-demo" {
		t.Fatalf("unexpected Caddy API calls: methods=%v paths=%v", methods, paths)
	}
}
