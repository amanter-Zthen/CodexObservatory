package main

import (
	"bytes"
	"codex-observatory/internal/routetrace"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRelayPreservesProtocolBytesAndFinalFragment(t *testing.T) {
	raw := []byte("  {\"id\":1}\r\n" + strings.Repeat("x", 200000) + "\n final-fragment ")
	var out bytes.Buffer
	var lines [][]byte
	if err := relay(bytes.NewReader(raw), &out, func(b []byte) { lines = append(lines, append([]byte{}, b...)) }); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), raw) || len(lines) != 3 || string(lines[2]) != "final-fragment" {
		t.Fatal("protocol bytes or line boundaries changed")
	}
}
func TestMetadataSinkWritesOnlySelectedFields(t *testing.T) {
	dir := t.TempDir()
	s := &sink{dir: dir}
	s.write([]routetrace.Record{{ID: "record", Thread: "thread", Turn: "turn", Requested: "model", Reported: "model", Verdict: "match"}})
	b, e := os.ReadFile(filepath.Join(dir, "record.json"))
	if e != nil {
		t.Fatal(e)
	}
	var r routetrace.Record
	if json.Unmarshal(b, &r) != nil || r.Thread != "thread" || s.failures() != 0 {
		t.Fatal("metadata sink failed")
	}
}
