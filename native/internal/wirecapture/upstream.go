package wirecapture

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func proxyEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		v = os.Getenv(strings.ToLower(key))
	}
	return v
}
func bypassProxy(host, port, raw string) bool {
	host = strings.ToLower(host)
	if strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback() {
		return true
	}
	for _, token := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' }) {
		pattern := strings.ToLower(strings.TrimSpace(token))
		if pattern == "" {
			continue
		}
		if pattern == "*" || pattern == "<local>" && !strings.Contains(host, ".") {
			return true
		}
		if _, subnet, err := net.ParseCIDR(pattern); err == nil && subnet.Contains(net.ParseIP(host)) {
			return true
		}
		if h, p, err := net.SplitHostPort(pattern); err == nil {
			if p != port {
				continue
			}
			pattern = h
		}
		pattern = strings.TrimPrefix(pattern, "*.")
		pattern = strings.TrimPrefix(pattern, ".")
		if pattern == host || strings.HasSuffix(host, "."+pattern) {
			return true
		}
	}
	return false
}
func configuredProxy(server, scheme string) (*url.URL, error) {
	if strings.Contains(server, "://") && !strings.Contains(server, ";") {
		return url.Parse(server)
	}
	selected := ""
	fallback := ""
	for _, part := range strings.Split(server, ";") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			if key != "" {
				selected = key
			}
			continue
		}
		if key == scheme {
			selected = value
		}
		if key == "socks" {
			fallback = value
		}
	}
	if selected == "" && fallback != "" {
		selected = "socks5://" + fallback
	}
	if selected == "" {
		return nil, nil
	}
	if !strings.Contains(selected, "://") {
		selected = "http://" + selected
	}
	return url.Parse(selected)
}
func originalProxy(r *http.Request) (*url.URL, error) {
	port := r.URL.Port()
	if port == "" {
		if r.URL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	if bypassProxy(r.URL.Hostname(), port, proxyEnv("NO_PROXY")) {
		return nil, nil
	}
	key := "HTTP_PROXY"
	if r.URL.Scheme == "https" {
		key = "HTTPS_PROXY"
	}
	if value := proxyEnv(key); value != "" {
		return configuredProxy(value, r.URL.Scheme)
	}
	if value := proxyEnv("ALL_PROXY"); value != "" {
		return configuredProxy(value, r.URL.Scheme)
	}
	server, bypass := systemProxyConfig()
	if bypassProxy(r.URL.Hostname(), port, bypass) {
		return nil, nil
	}
	return configuredProxy(server, r.URL.Scheme)
}
