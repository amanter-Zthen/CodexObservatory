// Package routefeed splits immutable routing metadata into live memory and an
// independently paced report writer. It never receives conversation content.
package routefeed

import (
	"codex-observatory/internal/routetrace"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
)

type Item struct {
	Sequence uint64            `json:"sequence"`
	Record   routetrace.Record `json:"record"`
}
type Batch struct {
	Epoch    string `json:"epoch"`
	Sequence uint64 `json:"sequence"`
	Reset    bool   `json:"reset"`
	Items    []Item `json:"items"`
}
type checkpoint struct {
	turn                              string
	request, created, latest, anomaly Item
}
type Hub struct {
	mu          sync.RWMutex
	epoch       string
	ring        []Item
	sequence    uint64
	checkpoints map[string]*checkpoint
}

func NewHub(capacity int) (*Hub, error) {
	if capacity < 8 {
		capacity = 8
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	return &Hub{epoch: hex.EncodeToString(random), ring: make([]Item, capacity), checkpoints: make(map[string]*checkpoint)}, nil
}
func (h *Hub) Publish(rows []routetrace.Record) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range rows {
		if r.ID == "" || r.Scope != "original_desktop_request" || r.Thread == "" || r.Turn == "" {
			continue
		}
		h.sequence++
		item := Item{Sequence: h.sequence, Record: r}
		h.ring[(h.sequence-1)%uint64(len(h.ring))] = item
		c := h.checkpoints[r.Thread]
		if c == nil {
			if len(h.checkpoints) >= 256 {
				oldID := ""
				var oldest uint64
				for id, previous := range h.checkpoints {
					if oldID == "" || previous.latest.Sequence < oldest {
						oldID, oldest = id, previous.latest.Sequence
					}
				}
				delete(h.checkpoints, oldID)
			}
			c = &checkpoint{turn: r.Turn}
			h.checkpoints[r.Thread] = c
		}
		if c.turn != r.Turn {
			if r.Stage != "request.sent" || r.At < c.latest.Record.At {
				continue
			}
			*c = checkpoint{turn: r.Turn}
		}
		if r.Stage == "request.sent" {
			if r.At >= c.request.Record.At {
				c.request = item
			}
			continue
		}
		if r.Verdict == "mismatch" && r.Requested != "" && r.Reported != "" {
			c.anomaly = item
		}
		if r.At < c.latest.Record.At {
			continue
		}
		if r.Response != c.latest.Record.Response {
			c.created = Item{}
		}
		if r.Stage == "response.created" {
			c.created = item
		}
		c.latest = item
	}
}

// Reconnection returns a bounded state checkpoint, including a turn's mismatch.
func (h *Hub) Read(epoch string, after uint64) Batch {
	h.mu.RLock()
	defer h.mu.RUnlock()
	b := Batch{Epoch: h.epoch, Sequence: h.sequence}
	oldest := uint64(1)
	if h.sequence > uint64(len(h.ring)) {
		oldest = h.sequence - uint64(len(h.ring)) + 1
	}
	if epoch != h.epoch || after > h.sequence || after+1 < oldest {
		b.Reset = true
		ids := map[string]bool{}
		for _, c := range h.checkpoints {
			for _, item := range []Item{c.request, c.created, c.latest, c.anomaly} {
				if item.Sequence > 0 && !ids[item.Record.ID] {
					ids[item.Record.ID] = true
					b.Items = append(b.Items, item)
				}
			}
		}
		sort.Slice(b.Items, func(i, j int) bool { return b.Items[i].Sequence < b.Items[j].Sequence })
		return b
	}
	for seq := after + 1; seq <= h.sequence; seq++ {
		b.Items = append(b.Items, h.ring[(seq-1)%uint64(len(h.ring))])
	}
	return b
}
