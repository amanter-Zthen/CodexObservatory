package routefeed

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DiscoveryFile = "routing-live-feed.json"

type Description struct {
	URL     string `json:"url"`
	Token   string `json:"token"`
	Version int    `json:"version"`
}
type Server struct {
	Description Description
	server      *http.Server
}

func Handler(hub *Hub, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 || r.Header.Get("Origin") != "" {
			http.Error(w, "unauthorized", http.StatusForbidden)
			return
		}
		if r.Method != "GET" || r.URL.Path != "/events" {
			http.Error(w, "unsupported", http.StatusMethodNotAllowed)
			return
		}
		after, err := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
		if err != nil {
			http.Error(w, "invalid cursor", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(hub.Read(r.URL.Query().Get("epoch"), after))
	})
}
func Listen(hub *Hub) (*Server, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(b)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{Description: Description{URL: "http://" + listener.Addr().String() + "/events", Token: token, Version: 1},
		server: &http.Server{Handler: Handler(hub, token), ReadHeaderTimeout: time.Second, WriteTimeout: 2 * time.Second, IdleTimeout: 30 * time.Second}}
	go s.server.Serve(listener)
	return s, nil
}
func (s *Server) Close(ctx context.Context) error { return s.server.Shutdown(ctx) }
func ValidDescription(d Description) bool {
	u, err := url.Parse(d.URL)
	return err == nil && d.Version == 1 && u.Scheme == "http" && u.Hostname() == "127.0.0.1" && u.Port() != "" && u.Path == "/events" && u.User == nil && len(d.Token) == 64
}
func Fetch(ctx context.Context, client *http.Client, d Description, epoch string, after uint64) (Batch, error) {
	if !ValidDescription(d) {
		return Batch{}, fmt.Errorf("invalid local feed")
	}
	parsed, _ := url.Parse(d.URL)
	q := parsed.Query()
	q.Set("epoch", epoch)
	q.Set("after", strconv.FormatUint(after, 10))
	parsed.RawQuery = q.Encode()
	r, err := http.NewRequestWithContext(ctx, "GET", parsed.String(), nil)
	if err != nil {
		return Batch{}, err
	}
	r.Header.Set("Authorization", "Bearer "+d.Token)
	resp, err := client.Do(r)
	if err != nil {
		return Batch{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Batch{}, fmt.Errorf("local feed status %d", resp.StatusCode)
	}
	var batch Batch
	dec := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 4<<20))
	if err = dec.Decode(&batch); err != nil {
		return Batch{}, err
	}
	if len(batch.Epoch) != 32 || strings.TrimSpace(batch.Epoch) == "" || len(batch.Items) > 2048 {
		return Batch{}, fmt.Errorf("invalid feed batch")
	}
	return batch, nil
}
