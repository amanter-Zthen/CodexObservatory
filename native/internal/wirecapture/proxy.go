// Package wirecapture observes request/response metadata on a process-scoped
// loopback proxy. Original bodies and authentication pass through unchanged.
package wirecapture

import (
	"bufio"
	"bytes"
	"codex-observatory/internal/routetrace"
	"compress/flate"
	"compress/gzip"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/klauspost/compress/zstd"
	"io"
	"math/big"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxMessage = 64 * 1024 * 1024

// Advertised in local health metadata so the monitor can distinguish an older
// collector that skips headerless HTTP fallback streams.
const CaptureRevision = "native-home-v2-sse"

type Proxy struct {
	listener    net.Listener
	server      *http.Server
	transport   *http.Transport
	mu          sync.Mutex
	certs       map[string]*tls.Certificate
	key         *ecdsa.PrivateKey
	ca          *x509.Certificate
	auth        string
	URL         string
	CA          []byte
	Hosts       map[string]bool
	Collector   *routetrace.Collector
	Emit        func([]routetrace.Record)
	connections map[net.Conn]bool
	password    string
}

// Options retains one local capture endpoint across host restarts. Private key
// and password belong to the current user's application data, never reports.
type Options struct {
	Address  string `json:"address"`
	CA       []byte `json:"ca"`
	Key      []byte `json:"key"`
	Password string `json:"password"`
}

func Start(collector *routetrace.Collector, emit func([]routetrace.Record), fixture bool) (*Proxy, error) {
	return StartWithOptions(collector, emit, fixture, Options{})
}

func StartWithOptions(collector *routetrace.Collector, emit func([]routetrace.Record), fixture bool, options Options) (*Proxy, error) {
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return nil, e
	}
	if len(options.Key) > 0 {
		block, _ := pem.Decode(options.Key)
		if block == nil {
			return nil, fmt.Errorf("capture identity key is invalid")
		}
		key, e = x509.ParseECPrivateKey(block.Bytes)
		if e != nil {
			return nil, fmt.Errorf("capture identity key is invalid")
		}
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	ca := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "CodexObservatory process-local capture"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(365 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, e := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if e != nil {
		return nil, e
	}
	ca, e = x509.ParseCertificate(der)
	if e != nil {
		return nil, e
	}
	if len(options.CA) > 0 {
		block, _ := pem.Decode(options.CA)
		if block == nil || len(options.Key) == 0 {
			return nil, fmt.Errorf("capture identity certificate is invalid")
		}
		ca, e = x509.ParseCertificate(block.Bytes)
		if e != nil || !ca.IsCA || time.Now().After(ca.NotAfter) {
			return nil, fmt.Errorf("capture identity certificate is invalid or expired")
		}
		public, ok := ca.PublicKey.(*ecdsa.PublicKey)
		if !ok || !public.Equal(&key.PublicKey) {
			return nil, fmt.Errorf("capture identity certificate and key differ")
		}
		der = ca.Raw
	} else if len(options.Key) > 0 {
		return nil, fmt.Errorf("capture identity requires its certificate")
	}
	roots, e := x509.SystemCertPool()
	if e != nil {
		roots = x509.NewCertPool()
	}
	originalPEM := []byte{}
	originalCA := os.Getenv("CODEX_CA_CERTIFICATE")
	if originalCA == "" {
		originalCA = os.Getenv("SSL_CERT_FILE")
	}
	if originalCA != "" {
		if b, er := os.ReadFile(originalCA); er == nil {
			roots.AppendCertsFromPEM(b)
			for len(b) > 0 {
				block, rest := pem.Decode(b)
				if block == nil {
					break
				}
				if block.Type == "CERTIFICATE" {
					originalPEM = append(originalPEM, pem.EncodeToMemory(block)...)
				}
				b = rest
			}
		}
	}
	random := make([]byte, 24)
	if _, e = rand.Read(random); e != nil {
		return nil, e
	}
	password := options.Password
	if password == "" {
		password = hex.EncodeToString(random)
	}
	if len(password) < 32 || strings.ContainsAny(password, ":@\r\n") {
		return nil, fmt.Errorf("capture authentication is invalid")
	}
	address := options.Address
	if address == "" {
		address = "127.0.0.1:0"
	}
	host, _, e := net.SplitHostPort(address)
	if e != nil || host != "127.0.0.1" {
		return nil, fmt.Errorf("capture listener must use IPv4 loopback")
	}
	listener, e := net.Listen("tcp4", address)
	if e != nil {
		return nil, e
	}
	p := &Proxy{listener: listener, key: key, ca: ca, certs: map[string]*tls.Certificate{}, connections: map[net.Conn]bool{}, auth: "Basic " + base64.StdEncoding.EncodeToString([]byte("observer:"+password)), CA: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), Hosts: map[string]bool{"chatgpt.com": true, "api.openai.com": true}, Collector: collector, Emit: emit}
	if fixture {
		p.Hosts["127.0.0.1"] = true
		p.Hosts["localhost"] = true
	}
	p.CA = append(p.CA, originalPEM...)
	p.password = password
	p.URL = "http://observer:" + password + "@" + listener.Addr().String()
	p.transport = &http.Transport{Proxy: originalProxy, TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, DialContext: (&net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: 20 * time.Second, ResponseHeaderTimeout: 120 * time.Second, DisableCompression: true, ForceAttemptHTTP2: false}
	p.server = &http.Server{Handler: p, ReadHeaderTimeout: 15 * time.Second, MaxHeaderBytes: 1024 * 1024}
	go p.server.Serve(listener)
	return p, nil
}

func (p *Proxy) Identity() Options {
	der, _ := x509.MarshalECPrivateKey(p.key)
	return Options{Address: p.listener.Addr().String(), CA: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: p.ca.Raw}), Key: pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), Password: p.password}
}
func (p *Proxy) Close() {
	_ = p.server.Close()
	p.mu.Lock()
	for c := range p.connections {
		_ = c.Close()
	}
	p.mu.Unlock()
	p.transport.CloseIdleConnections()
}
func (p *Proxy) cert(host string) (*tls.Certificate, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if certificate := p.certs[host]; certificate != nil && time.Until(certificate.Leaf.NotAfter) > 24*time.Hour {
		return certificate, nil
	}
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return nil, e
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	leaf := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: host}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(7 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if ip := net.ParseIP(host); ip != nil {
		leaf.IPAddresses = []net.IP{ip}
	} else {
		leaf.DNSNames = []string{host}
	}
	der, e := x509.CreateCertificate(rand.Reader, leaf, p.ca, &key.PublicKey, p.key)
	if e != nil {
		return nil, e
	}
	leaf, e = x509.ParseCertificate(der)
	if e != nil {
		return nil, e
	}
	certificate := &tls.Certificate{Certificate: [][]byte{der, p.ca.Raw}, PrivateKey: key, Leaf: leaf}
	p.certs[host] = certificate
	return certificate, nil
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) { return c.reader.Read(b) }
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Proxy-Authorization")), []byte(p.auth)) != 1 {
		w.Header().Set("Proxy-Authenticate", `Basic realm="local-observer"`)
		w.WriteHeader(407)
		return
	}
	r.Header.Del("Proxy-Authorization")
	// An authenticated, entirely local readiness check. It never contacts an
	// upstream service or creates a model request.
	if r.Method == http.MethodGet && r.URL.Host == "codex-observatory.invalid" && r.URL.Path == "/ready" {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"capture":"CodexObservatory","ready":true}`)
		return
	}
	if r.Method != http.MethodConnect {
		p.forward(w, r)
		return
	}
	host, _, e := net.SplitHostPort(r.Host)
	if e != nil {
		http.Error(w, "Invalid destination", 400)
		return
	}
	conn, reader, e := w.(http.Hijacker).Hijack()
	if e != nil {
		return
	}
	p.mu.Lock()
	p.connections[conn] = true
	p.mu.Unlock()
	defer func() { conn.Close(); p.mu.Lock(); delete(p.connections, conn); p.mu.Unlock() }()
	if !p.Hosts[strings.ToLower(host)] {
		upstream, e := p.tunnel(r.Host)
		if e != nil {
			io.WriteString(conn, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
			return
		}
		defer upstream.Close()
		io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		done := make(chan struct{})
		go func() { io.Copy(upstream, reader); close(done) }()
		io.Copy(conn, upstream)
		return
	}
	certificate, e := p.cert(host)
	if e != nil {
		return
	}
	io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
	local := tls.Server(&bufferedConn{Conn: conn, reader: reader.Reader}, &tls.Config{Certificates: []tls.Certificate{*certificate}, MinVersion: tls.VersionTLS12})
	if local.Handshake() != nil {
		return
	}
	br := bufio.NewReader(local)
	for {
		request, e := http.ReadRequest(br)
		if e != nil {
			return
		}
		request.URL.Scheme = "https"
		request.URL.Host = r.Host
		request.RequestURI = ""
		request.Header.Del("Proxy-Authorization")
		response, e := p.roundTrip(request)
		if e != nil {
			io.WriteString(local, "HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\nContent-Length: 0\r\n\r\n")
			return
		}
		if response.StatusCode == 101 {
			p.upgrade(local, br, response, request)
			return
		}
		if response.Write(local) != nil {
			response.Body.Close()
			return
		}
		response.Body.Close()
		if request.Close || response.Close {
			return
		}
	}
}
func (p *Proxy) tunnel(target string) (net.Conn, error) {
	request := &http.Request{URL: &url.URL{Scheme: "https", Host: target}}
	upstream, e := p.transport.Proxy(request)
	if e != nil {
		return nil, e
	}
	if upstream == nil {
		return net.DialTimeout("tcp", target, 20*time.Second)
	}
	if upstream.Scheme == "socks5" || upstream.Scheme == "socks5h" {
		return socksTunnel(upstream, target)
	}
	host := upstream.Host
	if !strings.Contains(host, ":") {
		port := "80"
		if upstream.Scheme == "https" {
			port = "443"
		}
		host = net.JoinHostPort(host, port)
	}
	conn, e := net.DialTimeout("tcp", host, 20*time.Second)
	if e != nil {
		return nil, e
	}
	if upstream.Scheme == "https" {
		wrapped := tls.Client(conn, &tls.Config{ServerName: upstream.Hostname(), RootCAs: p.transport.TLSClientConfig.RootCAs, MinVersion: tls.VersionTLS12})
		if e = wrapped.Handshake(); e != nil {
			conn.Close()
			return nil, e
		}
		conn = wrapped
	}
	header := ""
	if upstream.User != nil {
		password, _ := upstream.User.Password()
		header = "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(upstream.User.Username()+":"+password)) + "\r\n"
	}
	if _, e = fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n%s\r\n", target, target, header); e != nil {
		conn.Close()
		return nil, e
	}
	br := bufio.NewReader(conn)
	resp, e := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if e != nil || resp.StatusCode != 200 {
		conn.Close()
		return nil, fmt.Errorf("upstream tunnel unavailable")
	}
	return &bufferedConn{Conn: conn, reader: br}, nil
}
func socksTunnel(proxy *url.URL, target string) (net.Conn, error) {
	address := proxy.Host
	if proxy.Port() == "" {
		address = net.JoinHostPort(proxy.Hostname(), "1080")
	}
	c, err := net.DialTimeout("tcp", address, 20*time.Second)
	if err != nil {
		return nil, err
	}
	failed := true
	defer func() {
		if failed {
			c.Close()
		}
	}()
	c.SetDeadline(time.Now().Add(20 * time.Second))
	methods := []byte{5, 1, 0}
	if proxy.User != nil {
		methods = []byte{5, 2, 0, 2}
	}
	if _, err = c.Write(methods); err != nil {
		return nil, err
	}
	choice := make([]byte, 2)
	if _, err = io.ReadFull(c, choice); err != nil {
		return nil, err
	}
	if choice[0] != 5 || choice[1] != 0 && choice[1] != 2 {
		return nil, fmt.Errorf("SOCKS authentication unavailable")
	}
	if choice[1] == 2 {
		if proxy.User == nil {
			return nil, fmt.Errorf("SOCKS credentials unavailable")
		}
		name := proxy.User.Username()
		password, _ := proxy.User.Password()
		if len(name) > 255 || len(password) > 255 {
			return nil, fmt.Errorf("SOCKS credentials too long")
		}
		auth := append([]byte{1, byte(len(name))}, []byte(name)...)
		auth = append(auth, byte(len(password)))
		auth = append(auth, []byte(password)...)
		if _, err = c.Write(auth); err != nil {
			return nil, err
		}
		if _, err = io.ReadFull(c, choice); err != nil || choice[1] != 0 {
			return nil, fmt.Errorf("SOCKS authentication failed")
		}
	}
	host, portText, err := net.SplitHostPort(target)
	if err != nil || len(host) > 255 {
		return nil, fmt.Errorf("Invalid tunnel destination")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("Invalid tunnel port")
	}
	request := append([]byte{5, 1, 0, 3, byte(len(host))}, []byte(host)...)
	request = append(request, byte(port>>8), byte(port))
	if _, err = c.Write(request); err != nil {
		return nil, err
	}
	reply := make([]byte, 4)
	if _, err = io.ReadFull(c, reply); err != nil || reply[0] != 5 || reply[1] != 0 {
		return nil, fmt.Errorf("SOCKS tunnel unavailable")
	}
	length := 0
	switch reply[3] {
	case 1:
		length = 4
	case 4:
		length = 16
	case 3:
		var size [1]byte
		if _, err = io.ReadFull(c, size[:]); err != nil {
			return nil, err
		}
		length = int(size[0])
	default:
		return nil, fmt.Errorf("Invalid SOCKS response")
	}
	if _, err = io.CopyN(io.Discard, c, int64(length+2)); err != nil {
		return nil, err
	}
	c.SetDeadline(time.Time{})
	failed = false
	return c, nil
}
func (p *Proxy) forward(w http.ResponseWriter, r *http.Request) {
	r.RequestURI = ""
	response, e := p.roundTrip(r)
	if e != nil {
		http.Error(w, "Upstream unavailable", 502)
		return
	}
	defer response.Body.Close()
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	buffer := make([]byte, 32*1024)
	for {
		n, e := response.Body.Read(buffer)
		if n > 0 {
			if _, we := w.Write(buffer[:n]); we != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if e != nil {
			return
		}
	}
}

type bodyWithOriginal struct {
	io.Reader
	original io.Closer
}

func (b bodyWithOriginal) Close() error { return b.original.Close() }
func requestHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for _, key := range []string{"thread-id", "session-id", "x-codex-turn-metadata"} {
		out[key] = h.Get(key)
	}
	return out
}
func decoded(reader io.Reader, encoding string) (io.Reader, func(), error) {
	switch strings.ToLower(encoding) {
	case "gzip":
		r, e := gzip.NewReader(reader)
		if e != nil {
			return nil, func() {}, e
		}
		return r, func() { r.Close() }, nil
	case "zstd":
		r, e := zstd.NewReader(reader, zstd.WithDecoderMaxMemory(128*1024*1024))
		if e != nil {
			return nil, func() {}, e
		}
		return r, func() { r.Close() }, nil
	default:
		return reader, func() {}, nil
	}
}
func (p *Proxy) roundTrip(r *http.Request) (*http.Response, error) {
	var body map[string]any
	observe := p.Hosts[strings.ToLower(r.URL.Hostname())] && strings.HasSuffix(r.URL.Path, "/responses") && r.Method == http.MethodPost
	if observe && r.Body != nil {
		original := r.Body
		prefix, e := io.ReadAll(io.LimitReader(original, maxMessage+1))
		if e != nil {
			return nil, e
		}
		r.Body = bodyWithOriginal{Reader: io.MultiReader(bytes.NewReader(prefix), original), original: original}
		if len(prefix) <= maxMessage {
			reader, close, e := decoded(bytes.NewReader(prefix), r.Header.Get("Content-Encoding"))
			if e == nil {
				data, _ := io.ReadAll(io.LimitReader(reader, maxMessage+1))
				if len(data) <= maxMessage {
					_ = json.Unmarshal(data, &body)
				}
				close()
			}
		}
	}
	if observe && body != nil {
		p.Emit(p.Collector.BeginWire(routetrace.RequestFromWire(body, requestHeaders(r.Header)), "sse"))
	}
	response, e := p.transport.RoundTrip(r)
	if e != nil {
		return nil, e
	}
	if observe && body != nil && observesSSE(response, body) {
		ctx := routetrace.RequestFromWire(body, requestHeaders(r.Header))
		headers := map[string]any{}
		for _, key := range []string{"openai-model", "x-openai-model"} {
			if value := response.Header.Get(key); value != "" {
				headers[key] = value
			}
		}
		reader, writer := io.Pipe()
		original := response.Body
		go func() {
			defer reader.Close()
			source, close, e := decoded(reader, response.Header.Get("Content-Encoding"))
			if e != nil {
				return
			}
			defer close()
			responseID := ""
			scanner := bufio.NewScanner(source)
			scanner.Buffer(make([]byte, 64*1024), maxMessage)
			for scanner.Scan() {
				line := scanner.Bytes()
				if bytes.HasPrefix(line, []byte("data:")) {
					payload := bytes.TrimSpace(line[5:])
					if bytes.Equal(payload, []byte("[DONE]")) {
						continue
					}
					var frame map[string]any
					if json.Unmarshal(payload, &frame) == nil {
						stage, _ := frame["type"].(string)
						p.Collector.NoteWireMessage(stage, true)
						if stage == "response.created" {
							responseID = websocketResponseID(frame)
						}
						if stage == "response.output_text.delta" && websocketResponseID(frame) == "" && responseID != "" {
							frame["response_id"] = responseID
						}
						if stage == "codex.response.metadata" || stage == "response.metadata" {
							if metadata, ok := frame["headers"].(map[string]any); ok {
								replaceModelHeaders(headers, metadata)
							}
							if body, ok := frame["response"].(map[string]any); ok {
								if metadata, ok := body["headers"].(map[string]any); ok {
									replaceModelHeaders(headers, metadata)
								}
							}
						}
						p.Emit(p.Collector.ObserveWire(ctx, frame, "sse", headers))
					} else {
						p.Collector.NoteWireMessage("", false)
					}
				}
			}
		}()
		response.Body = &observedBody{original: original, writer: writer}
	}
	return response, nil
}

func observesSSE(response *http.Response, request map[string]any) bool {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false
	}
	contentType := strings.TrimSpace(response.Header.Get("Content-Type"))
	if contentType == "" {
		// Responses HTTP fallback can omit Content-Type. The caller has already
		// checked the host, POST /responses and parsed request body. Only an
		// explicitly streaming request enables the headerless event observer;
		// the normal event parser still requires actual server model evidence.
		return request["stream"] == true
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && strings.EqualFold(mediaType, "text/event-stream")
}

type observedBody struct {
	original io.ReadCloser
	writer   *io.PipeWriter
}

func (b *observedBody) Read(p []byte) (int, error) {
	n, e := b.original.Read(p)
	if n > 0 {
		_, _ = b.writer.Write(p[:n])
	}
	if e != nil {
		b.writer.Close()
	}
	return n, e
}
func (b *observedBody) Close() error { b.writer.Close(); return b.original.Close() }

type websocketContext struct {
	mu              sync.Mutex
	pending         []*websocketRequest
	responses       map[string]*websocketRequest
	headers         map[string]string
	responseHeaders map[string]any
	handshakeUsed   bool
}

type websocketRequest struct {
	request    routetrace.RequestContext
	headers    map[string]any
	active     bool
	responseID string
	bodyModel  string
}

func modelHeaderLabel(label string) bool {
	if len(label) == 0 || len(label) > 160 {
		return false
	}
	for _, r := range label {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:/+-", r)) {
			return false
		}
	}
	return true
}

func copyModelHeaders(headers map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range headers {
		if strings.EqualFold(key, "openai-model") || strings.EqualFold(key, "x-openai-model") {
			// Only model labels enter connection state; other response metadata
			// remains transient input to the relay.
			switch value := value.(type) {
			case string:
				if modelHeaderLabel(value) {
					out[strings.ToLower(key)] = value
				}
			case []any:
				for _, item := range value {
					if label, ok := item.(string); ok && modelHeaderLabel(label) {
						out[strings.ToLower(key)] = label
						break
					}
				}
			}
		}
	}
	return out
}

func replaceModelHeaders(target, metadata map[string]any) {
	labels := copyModelHeaders(metadata)
	if len(labels) == 0 {
		return
	}
	// Both header spellings describe the same label. A new observation
	// replaces the earlier spelling instead of leaving a stale alias behind.
	for key := range target {
		if strings.EqualFold(key, "openai-model") || strings.EqualFold(key, "x-openai-model") {
			delete(target, key)
		}
	}
	for key, value := range labels {
		target[key] = value
	}
}

func websocketResponseID(frame map[string]any) string {
	response, _ := frame["response"].(map[string]any)
	id, _ := response["id"].(string)
	if id == "" {
		id, _ = frame["response_id"].(string)
	}
	if len(id) == 0 || len(id) > 256 {
		return ""
	}
	for _, r := range id {
		if r < 33 || r > 126 {
			return ""
		}
	}
	return id
}

func (ctx *websocketContext) uniqueRequest() *websocketRequest {
	var result *websocketRequest
	seen := map[*websocketRequest]bool{}
	consider := func(state *websocketRequest) bool {
		if state == nil || seen[state] {
			return true
		}
		seen[state] = true
		if result != nil {
			return false
		}
		result = state
		return true
	}
	for _, state := range ctx.pending {
		if !consider(state) {
			return nil
		}
	}
	for _, state := range ctx.responses {
		if state != nil && state.active && !consider(state) {
			return nil
		}
	}
	return result
}

func (p *Proxy) observeWebsocketFrame(frame map[string]any, fromClient bool, ctx *websocketContext) {
	stage, _ := frame["type"].(string)
	if fromClient {
		if stage != "response.create" {
			return
		}
		request := routetrace.RequestFromWire(frame, ctx.headers)
		state := &websocketRequest{request: request, headers: map[string]any{}}
		ctx.mu.Lock()
		// A handshake label is evidence for at most the first create. Every
		// subsequent request collects its own model metadata.
		if !ctx.handshakeUsed {
			state.headers = copyModelHeaders(ctx.responseHeaders)
			ctx.handshakeUsed = true
		}
		ctx.pending = append(ctx.pending, state)
		// Without a response ID, overlapping creates do not establish which
		// request a subsequent response belongs to.
		if len(ctx.pending) > 1 {
			for _, pending := range ctx.pending {
				pending.request = routetrace.RequestContext{}
			}
		}
		if len(ctx.pending) > 256 {
			ctx.pending = []*websocketRequest{{headers: map[string]any{}}}
		}
		ctx.mu.Unlock()
		p.Emit(p.Collector.BeginWire(request, "websocket"))
		return
	}
	id := websocketResponseID(frame)
	if stage == "response.output_text.delta" {
		ctx.mu.Lock()
		state := ctx.responses[id]
		if state == nil && id == "" {
			state = ctx.uniqueRequest()
		}
		request := routetrace.RequestContext{}
		headers := map[string]any{}
		responseID, bodyModel := "", ""
		if state != nil && state.active {
			request, headers, responseID, bodyModel = state.request, copyModelHeaders(state.headers), state.responseID, state.bodyModel
		}
		ctx.mu.Unlock()
		if responseID != "" {
			progress := map[string]any{"type": stage, "response": map[string]any{"id": responseID, "model": bodyModel}, "delta": frame["delta"]}
			p.Emit(p.Collector.ObserveWire(request, progress, "websocket", headers))
		}
		return
	}
	if stage == "codex.response.metadata" || stage == "response.metadata" {
		ctx.mu.Lock()
		state := ctx.responses[id]
		if state == nil {
			state = ctx.uniqueRequest()
			if state != nil && id != "" && state.responseID != "" && state.responseID != id {
				state = nil
			}
		}
		request := routetrace.RequestContext{}
		headers := map[string]any{}
		associatedID := ""
		bodyModel := ""
		if state != nil {
			modelHeaders := map[string]any{}
			if headers, ok := frame["headers"].(map[string]any); ok {
				modelHeaders = copyModelHeaders(headers)
			}
			if response, ok := frame["response"].(map[string]any); ok {
				if headers, ok := response["headers"].(map[string]any); ok {
					replaceModelHeaders(modelHeaders, headers)
				}
			}
			replaceModelHeaders(state.headers, modelHeaders)
			if state.responseID == "" && id != "" {
				state.responseID = id
			}
			if state.active {
				request = state.request
				headers = copyModelHeaders(state.headers)
				associatedID = state.responseID
				bodyModel = state.bodyModel
			}
		}
		ctx.mu.Unlock()
		// A label that arrives after response.created can update the current
		// route immediately. The ID comes from the established association.
		if associatedID != "" {
			metadata := map[string]any{"type": stage, "response": map[string]any{"id": associatedID, "model": bodyModel}, "headers": headers}
			p.Emit(p.Collector.ObserveWire(request, metadata, "websocket", headers))
		}
		return
	}
	terminal := stage == "response.completed" || stage == "response.failed" || stage == "response.incomplete" || stage == "response.cancelled"
	if id == "" || stage != "response.created" && !terminal {
		return
	}
	ctx.mu.Lock()
	state := ctx.responses[id]
	if state == nil && len(ctx.pending) > 0 {
		state = ctx.pending[0]
		ctx.pending = ctx.pending[1:]
		if state.responseID != "" && state.responseID != id {
			state.headers = map[string]any{}
		}
		state.responseID = id
		state.active = true
		ctx.responses[id] = state
	}
	request := routetrace.RequestContext{}
	headers := map[string]any{}
	if state != nil {
		if response, ok := frame["response"].(map[string]any); ok {
			if responseHeaders, ok := response["headers"].(map[string]any); ok {
				replaceModelHeaders(state.headers, responseHeaders)
			}
			if model, ok := response["model"].(string); ok && modelHeaderLabel(model) {
				state.bodyModel = model
			}
		}
		request = state.request
		headers = copyModelHeaders(state.headers)
		if terminal {
			state.active = false
		}
	}
	if len(ctx.responses) > 2048 {
		// Keep active associations while discarding old completed responses.
		for responseID, candidate := range ctx.responses {
			if !candidate.active && responseID != id {
				delete(ctx.responses, responseID)
			}
		}
		if len(ctx.responses) > 4096 {
			// An abnormal number of simultaneous responses cannot retain a
			// trustworthy unique request. Bound memory and keep it uncertain.
			for _, pending := range ctx.pending {
				pending.request = routetrace.RequestContext{}
			}
			if state != nil {
				state.request = routetrace.RequestContext{}
			}
			ctx.responses = map[string]*websocketRequest{}
			if state != nil {
				ctx.responses[id] = state
			}
			request = routetrace.RequestContext{}
		}
	}
	ctx.mu.Unlock()
	p.Emit(p.Collector.ObserveWire(request, frame, "websocket", headers))
}

func (p *Proxy) upgrade(client net.Conn, clientReader *bufio.Reader, response *http.Response, request *http.Request) {
	upstream, ok := response.Body.(io.ReadWriteCloser)
	if !ok {
		response.Body.Close()
		return
	}
	defer upstream.Close()
	// HTTP headers only: Response.Write would treat the upgraded body as a stream.
	fmt.Fprintf(client, "HTTP/1.1 101 Switching Protocols\r\n")
	for k, values := range response.Header {
		for _, v := range values {
			fmt.Fprintf(client, "%s: %s\r\n", k, v)
		}
	}
	io.WriteString(client, "\r\n")
	ctx := &websocketContext{responses: map[string]*websocketRequest{}, headers: requestHeaders(request.Header), responseHeaders: map[string]any{}}
	for _, key := range []string{"openai-model", "x-openai-model"} {
		if v := response.Header.Get(key); v != "" {
			ctx.responseHeaders[key] = v
		}
	}
	done := make(chan struct{})
	go func() { p.wsRelay(upstream, clientReader, true, ctx); close(done) }()
	p.wsRelay(client, bufio.NewReader(upstream), false, ctx)
}
func (p *Proxy) wsRelay(out io.Writer, in *bufio.Reader, fromClient bool, ctx *websocketContext) {
	var message, dict []byte
	compressed := false
	oversize := false
	for {
		header := make([]byte, 2)
		if _, e := io.ReadFull(in, header); e != nil {
			return
		}
		opcode := header[0] & 15
		fin := header[0]&128 != 0
		maskFlag := header[1]&128 != 0
		size := uint64(header[1] & 127)
		if size == 126 {
			extra := make([]byte, 2)
			if _, e := io.ReadFull(in, extra); e != nil {
				return
			}
			header = append(header, extra...)
			size = uint64(binary.BigEndian.Uint16(extra))
		} else if size == 127 {
			extra := make([]byte, 8)
			if _, e := io.ReadFull(in, extra); e != nil {
				return
			}
			header = append(header, extra...)
			size = binary.BigEndian.Uint64(extra)
		}
		mask := []byte(nil)
		if maskFlag {
			mask = make([]byte, 4)
			if _, e := io.ReadFull(in, mask); e != nil {
				return
			}
			header = append(header, mask...)
		}
		if _, e := out.Write(header); e != nil {
			return
		}
		if opcode == 1 {
			message = nil
			compressed = header[0]&64 != 0
			oversize = false
		}
		buffer := make([]byte, 32*1024)
		remaining := size
		offset := uint64(0)
		observed := false
		observe := func() {
			observed = true
			data := message
			if compressed {
				inflater := flate.NewReaderDict(bytes.NewReader(append(append([]byte{}, message...), 0, 0, 255, 255)), dict)
				decoded, _ := io.ReadAll(io.LimitReader(inflater, maxMessage+1))
				inflater.Close()
				if len(decoded) > maxMessage {
					if !fromClient {
						p.Collector.NoteWireMessage("", false)
					}
					return
				}
				data = decoded
				dict = append(dict, data...)
				if len(dict) > 32768 {
					dict = append([]byte{}, dict[len(dict)-32768:]...)
				}
			}
			var frame map[string]any
			if json.Unmarshal(data, &frame) == nil {
				if !fromClient {
					stage, _ := frame["type"].(string)
					p.Collector.NoteWireMessage(stage, true)
				}
				p.observeWebsocketFrame(frame, fromClient, ctx)
			} else if !fromClient {
				p.Collector.NoteWireMessage("", false)
			}
		}
		for remaining > 0 {
			n := len(buffer)
			if uint64(n) > remaining {
				n = int(remaining)
			}
			if _, e := io.ReadFull(in, buffer[:n]); e != nil {
				return
			}
			if opcode == 1 || opcode == 0 {
				if len(message)+n > maxMessage {
					oversize = true
					message = nil
				}
				if !oversize {
					part := append([]byte{}, buffer[:n]...)
					if maskFlag {
						for i := range part {
							part[i] ^= mask[(offset+uint64(i))%4]
						}
					}
					message = append(message, part...)
				}
			}
			remaining -= uint64(n)
			offset += uint64(n)
			if remaining == 0 && fin && (opcode == 1 || opcode == 0) && !oversize {
				// Register a complete create before its final original payload
				// bytes reach the server, so even an immediate reply is associated.
				observe()
			}
			if _, e := out.Write(buffer[:n]); e != nil {
				return
			}
		}
		if opcode == 8 {
			return
		}
		if oversize && fin && (opcode == 1 || opcode == 0) && !fromClient {
			p.Collector.NoteWireMessage("", false)
		}
		if !fin || opcode != 1 && opcode != 0 || oversize {
			continue
		}
		if !observed {
			observe()
		}
	}
}

// Digest is useful for fixture assertions without retaining bodies.
func Digest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
