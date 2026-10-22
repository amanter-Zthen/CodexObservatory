package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func fixtureRecord(typ string, p Obj, at string) []byte {
	return jsonBytes(Obj{"timestamp": at, "type": typ, "payload": p})
}
func fixtureUsage(input, total int64) Obj {
	return Obj{"input_tokens": input, "cached_input_tokens": input / 2, "cache_write_input_tokens": int64(0), "output_tokens": int64(100), "reasoning_output_tokens": int64(60), "total_tokens": total}
}
func fixtureToken(input, total int64) Obj {
	return Obj{"type": "token_count", "info": Obj{"total_token_usage": fixtureUsage(total-100, total), "last_token_usage": fixtureUsage(input, input+100), "model_context_window": int64(200000)}, "rate_limits": Obj{"limit_id": "codex-test", "plan_type": "demo", "primary": Obj{"used_percent": 25.0, "window_minutes": int64(300), "resets_at": time.Now().Add(time.Hour).Unix()}, "secondary": Obj{"used_percent": 40.0, "window_minutes": int64(10080), "resets_at": time.Now().Add(72 * time.Hour).Unix()}}}
}
func writeDemo(home string) error {
	d := filepath.Join(home, "sessions", "demo")
	if e := os.MkdirAll(d, 0700); e != nil {
		return e
	}
	for _, name := range []string{"main", "review"} {
		path := filepath.Join(d, "rollout-demo-"+name+".jsonl")
		if _, e := os.Stat(path); e == nil {
			continue
		}
		var b bytes.Buffer
		base := time.Now().Add(-65 * time.Minute)
		add := func(typ string, p Obj, n int) {
			b.Write(fixtureRecord(typ, p, base.Add(time.Duration(n)*time.Minute).UTC().Format(time.RFC3339)))
			b.WriteByte('\n')
		}
		add("session_meta", Obj{"id": "DEMO-" + name + "-not-a-real-thread", "session_id": "DEMO-" + name, "cwd": "C:\\Example-Project\\" + name, "source": "desktop", "originator": "codex_desktop (SYNTHETIC)", "cli_version": "DEMO-NOT-REAL"}, 0)
		model := "demo-premium-model"
		effort := "ultra"
		if name == "review" {
			model = "demo-review-model"
			effort = "high"
		}
		add("turn_context", Obj{"model": model, "effort": effort, "turn_id": "DEMO-turn-1", "model_context_window": 200000}, 1)
		total := int64(0)
		for i := 0; i < 10; i++ {
			turn := fmt.Sprintf("DEMO-turn-%d", i+1)
			add("event_msg", Obj{"type": "task_started", "turn_id": turn}, i*5+2)
			add("response_item", Obj{"type": "function_call", "name": "example_read_only_tool", "call_id": fmt.Sprintf("DEMO-call-%d", i)}, i*5+3)
			input := int64(24000 + i*8000)
			total += input + 100
			p := fixtureToken(input, total)
			q := obj(p["rate_limits"])
			obj(q["primary"])["used_percent"] = 14.0 + float64(i)*2.2
			obj(q["secondary"])["used_percent"] = 31.0 + float64(i)*.65
			add("event_msg", p, i*5+4)
			add("event_msg", Obj{"type": "task_complete", "turn_id": turn, "duration_ms": int64(44000)}, i*5+5)
		}
		add("event_msg", Obj{"type": "warning", "message": "Selected model is at capacity", "code": "server_is_overloaded"}, 54)
		if e := os.WriteFile(path, b.Bytes(), 0600); e != nil {
			return e
		}
	}
	return nil
}
func runSelfCheck() Obj {
	tests := []Obj{}
	check := func(name string, fn func() bool) {
		ok := false
		detail := ""
		func() {
			defer func() {
				if r := recover(); r != nil {
					detail = fmt.Sprint(r)
				}
			}()
			ok = fn()
		}()
		tests = append(tests, Obj{"name": name, "ok": ok, "error": detail})
	}
	at := time.Now().UTC().Format(time.RFC3339Nano)
	newF := func() *FileState { return newState("synthetic", "synthetic.jsonl", "test") }
	check("解析模型与原推理档，不降级", func() bool {
		f := newF()
		parseLine(f, fixtureRecord("turn_context", Obj{"model": "test-pro", "effort": "ultra"}, at), 0)
		return f.Summary.Model == "test-pro" && f.Summary.Effort == "ultra"
	})
	check("缺失模型保持未知", func() bool {
		f := newF()
		parseLine(f, fixtureRecord("event_msg", fixtureToken(1000, 2000), at), 0)
		return f.Summary.Model == ""
	})
	check("累计快照不重复相加", func() bool {
		f := newF()
		b := fixtureRecord("event_msg", fixtureToken(1000, 2000), at)
		parseLine(f, b, 0)
		events := parseLine(f, b, 100)
		return f.Summary.Total.Total == 2000 && len(events) == 0
	})
	check("context 使用最近输入，不使用累计总量", func() bool {
		f := newF()
		parseLine(f, fixtureRecord("event_msg", fixtureToken(1200, 90000), at), 0)
		return f.Summary.Latest.Input == 1200 && f.Summary.Total.Total == 90000 && f.Summary.ContextWindow == 200000
	})
	check("压缩使上下文估计失效", func() bool {
		f := newF()
		parseLine(f, fixtureRecord("event_msg", fixtureToken(1000, 2000), at), 0)
		parseLine(f, fixtureRecord("compacted", Obj{}, at), 100)
		return !f.Summary.ContextValid
	})
	check("未知额度不显示为 0%", func() bool { return extractWindow(Obj{"window_minutes": 300}) == nil })
	check("额度窗口保留实际分钟数", func() bool {
		q := extractWindow(Obj{"used_percent": 0, "window_minutes": 90})
		return q != nil && q.Used == 0 && q.Minutes != nil && *q.Minutes == 90
	})
	check("quota alias 不替换模型", func() bool {
		f := newF()
		parseLine(f, fixtureRecord("turn_context", Obj{"model": "model-a", "effort": "high"}, at), 0)
		p := fixtureToken(1000, 2000)
		obj(p["rate_limits"])["normal_model_slug"] = "model-b"
		parseLine(f, fixtureRecord("event_msg", p, at), 100)
		return f.Summary.Model == "model-a"
	})
	check("用户/工具正文不触发容量错误", func() bool {
		f := newF()
		parseLine(f, fixtureRecord("response_item", Obj{"type": "message", "role": "user", "content": "at capacity server_is_overloaded"}, at), 0)
		return f.Summary.LastError == nil
	})
	check("明确错误事件分类", func() bool {
		f := newF()
		parseLine(f, fixtureRecord("event_msg", Obj{"type": "error", "message": "Selected model is at capacity"}, at), 0)
		return f.Summary.LastError != nil && str(f.Summary.LastError.Detail["category"]) == "CAPACITY"
	})
	check("不保存对话正文", func() bool {
		f := newF()
		parseLine(f, fixtureRecord("response_item", Obj{"type": "message", "role": "assistant", "content": "PRIVATE_PROMPT_SENTINEL_9281"}, at), 0)
		return !bytes.Contains(jsonBytes(f), []byte("PRIVATE_PROMPT_SENTINEL_9281"))
	})
	check("等待未写完的 JSONL 行", func() bool {
		_, _, complete, _, e := readRecord(bufio.NewReader(strings.NewReader(`{"type":"turn_context"`)))
		return !complete && e != nil
	})
	check("CRLF 行可以读取", func() bool {
		b, _, complete, _, e := readRecord(bufio.NewReader(strings.NewReader("{}\r\n")))
		return complete && e == nil && string(bytes.TrimSpace(b)) == "{}"
	})
	check("损坏行不使解析器崩溃", func() bool { f := newF(); parseLine(f, []byte("{not-json}"), 0); return f.Summary.ParseErrors == 1 })
	check("仅早期响应不判为标签正常", func() bool {
		p := &Probe{Request: ProbeRequest{Model: "model-a"}, Responses: []ProbeResponse{{ID: "r", Models: []string{"model-a"}, Status: "in_progress"}}}
		return probeConclusion(p) == "UNOBSERVED"
	})
	check("不同模型标签独立记录", func() bool {
		p := &Probe{Request: ProbeRequest{Model: "model-a"}, Responses: []ProbeResponse{{ID: "r", Models: []string{"model-b"}, Status: "completed"}}}
		return probeConclusion(p) == "SERVER_LABEL_DIFFERENCE"
	})
	check("发送侧 trace 不作为服务器响应", func() bool {
		p := &Probe{Responses: []ProbeResponse{}, Errors: []string{}}
		c := probeCapture{p: p, byID: map[string]int{}}
		c.feed(`tungstenite::protocol: Sending frame {"type":"response.completed","response":{"id":"r","model":"fake"}}`, false)
		return p.Frames == 0 && len(p.Responses) == 0
	})
	check("不把 inherited history prefix 当作已完整读入", func() bool {
		f := newF()
		parseLine(f, fixtureRecord("session_meta", Obj{"id": "abc", "history_base": Obj{"thread_id": "base"}}, at), 0)
		return f.Summary.HasHistoryBase
	})
	check("本机临时目录写入与原子替换", func() bool {
		d, e := os.MkdirTemp("", "codex-observatory-selfcheck-")
		if e != nil {
			return false
		}
		defer os.RemoveAll(d)
		p := filepath.Join(d, "check.json")
		if saveJSON(p, Obj{"value": 1}) != nil || saveJSON(p, Obj{"value": 2}) != nil {
			return false
		}
		var m Obj
		if loadJSON(p, &m) != nil {
			return false
		}
		return num(m["value"]) == 2
	})
	passed := 0
	for _, t := range tests {
		if t["ok"] == true {
			passed++
		}
	}
	return Obj{"version": Version, "at": now(), "platform": runtime.GOOS + "/" + runtime.GOARCH, "passed": passed, "total": len(tests), "tests": tests, "model_requests": 0, "source_files_modified": 0, "scope": "Synthetic fixtures and temporary application-owned files only; not a live account or Windows GUI certification"}
}

var _ = json.Unmarshal
