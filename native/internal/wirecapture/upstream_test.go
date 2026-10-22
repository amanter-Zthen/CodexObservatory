package wirecapture

import (
	"net/http"
	"net/url"
	"testing"
)

func TestOriginalProxyKeepsUpstreamAndBypassRules(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:4567")
	t.Setenv("NO_PROXY", "*.example.test,10.0.0.0/8")
	for _, tc := range []struct {
		target string
		want   bool
	}{{"https://chatgpt.com/backend-api/codex/responses", true}, {"https://x.example.test", false}, {"https://127.0.0.1", false}, {"https://10.0.1.2", false}} {
		u, _ := url.Parse(tc.target)
		p, e := originalProxy(&http.Request{URL: u})
		if e != nil || (p != nil) != tc.want {
			t.Fatalf("%s proxy=%v error=%v", tc.target, p, e)
		}
	}
	p, e := configuredProxy("http=127.0.0.1:80;https=127.0.0.1:8080", "https")
	if e != nil || p.Host != "127.0.0.1:8080" {
		t.Fatal("protocol-specific system proxy changed")
	}
	p, e = configuredProxy("socks=127.0.0.1:1080", "https")
	if e != nil || p.Scheme != "socks5" {
		t.Fatal("SOCKS system proxy changed")
	}
}
