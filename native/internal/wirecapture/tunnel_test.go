package wirecapture

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestOpaqueHTTPProxyTunnelUsesOriginalCredentialsAndBytes(t *testing.T) {
	result := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" || r.Host != "unobserved.example:443" || r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpwYXNz" {
			result <- fmt.Errorf("original upstream tunnel metadata changed")
			w.WriteHeader(400)
			return
		}
		c, rw, e := w.(http.Hijacker).Hijack()
		if e != nil {
			result <- e
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(time.Second))
		io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n")
		b := make([]byte, 6)
		_, e = io.ReadFull(rw, b)
		if e == nil && string(b) != "opaque" {
			e = fmt.Errorf("opaque bytes changed")
		}
		if e == nil {
			_, e = c.Write(b)
		}
		result <- e
	}))
	defer server.Close()
	p, _, _ := testCapture(t, server)
	u, _ := url.Parse(server.URL)
	u.User = url.UserPassword("user", "pass")
	p.transport.Proxy = http.ProxyURL(u)
	c, e := p.tunnel("unobserved.example:443")
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	c.Write([]byte("opaque"))
	b := make([]byte, 6)
	if _, e = io.ReadFull(c, b); e != nil || string(b) != "opaque" {
		t.Fatalf("tunnel response: %v", e)
	}
	if e = <-result; e != nil {
		t.Fatal(e)
	}
}
func TestOpaqueSOCKSProxyTunnelPreservesAuthenticationAndDestination(t *testing.T) {
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	result := make(chan error, 1)
	go func() {
		c, e := listener.Accept()
		if e != nil {
			result <- e
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(time.Second))
		h := make([]byte, 2)
		io.ReadFull(c, h)
		methods := make([]byte, int(h[1]))
		io.ReadFull(c, methods)
		c.Write([]byte{5, 2})
		io.ReadFull(c, h)
		name := make([]byte, int(h[1]))
		io.ReadFull(c, name)
		var n [1]byte
		io.ReadFull(c, n[:])
		password := make([]byte, int(n[0]))
		io.ReadFull(c, password)
		if string(name) != "user" || string(password) != "pass" {
			result <- fmt.Errorf("SOCKS authentication changed")
			return
		}
		c.Write([]byte{1, 0})
		request := make([]byte, 5)
		io.ReadFull(c, request)
		host := make([]byte, int(request[4]))
		io.ReadFull(c, host)
		port := make([]byte, 2)
		io.ReadFull(c, port)
		if string(host) != "unobserved.example" || binary.BigEndian.Uint16(port) != 443 {
			result <- fmt.Errorf("SOCKS destination changed")
			return
		}
		c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 1})
		body := make([]byte, 6)
		_, e = io.ReadFull(c, body)
		if e == nil {
			_, e = c.Write(body)
		}
		result <- e
	}()
	u, _ := url.Parse("socks5://user:pass@" + listener.Addr().String())
	c, e := socksTunnel(u, "unobserved.example:443")
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	c.Write([]byte("opaque"))
	b := make([]byte, 6)
	if _, e = io.ReadFull(c, b); e != nil || string(b) != "opaque" {
		t.Fatalf("SOCKS bytes: %v", e)
	}
	if e = <-result; e != nil {
		t.Fatal(e)
	}
}
