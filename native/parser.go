package main

import (
	"encoding/json"
	"sort"
	"strings"
)

// Only allowlisted telemetry is persisted. Conversation/tool content is not retained.
func parseLine(f *FileState, b []byte, offset int64) []Event {
	r, err := decodeObj(b)
	if err != nil {
		f.Summary.ParseErrors++
		return nil
	}
	if f.Summary.Quotas == nil {
		f.Summary.Quotas = map[string]Quota{}
	}
	if f.SeenToolIDs == nil {
		f.SeenToolIDs = map[string]bool{}
	}
	s := &f.Summary
	f.TierSchema = 1
	typ := str(r["type"])
	p := obj(r["payload"])
	at := str(r["timestamp"])
	if at != "" {
		s.At = at
		if s.StartedAt == "" {
			s.StartedAt = at
		}
	}
	events := []Event{}
	emit := func(kind, field string, detail Obj) *Event {
		e := eventFor(f, r, b, offset, kind, field)
		e.Detail = detail
		events = append(events, e)
		return &events[len(events)-1]
	}
	switch typ {
	case "session_meta":
		s.Thread = first(str(p["id"]), str(p["thread_id"]), str(p["session_id"]), s.Thread)
		s.RootThread = first(str(p["session_id"]), s.RootThread)
		s.Parent = first(str(p["parent_thread_id"]), s.Parent)
		s.Forked = first(str(p["forked_from_id"]), s.Forked)
		s.CWD = first(str(p["cwd"]), s.CWD)
		s.ClientVersion = first(str(p["cli_version"]), s.ClientVersion)
		s.Originator = first(str(p["originator"]), s.Originator)
		if v := str(p["source"]); v != "" {
			s.Source = v
		} else if v := obj(p["source"]); len(v) > 0 {
			if _, ok := v["subagent"]; ok {
				s.Source = "subagent"
			} else {
				s.Source = "structured/unknown"
			}
		}
		s.HasHistoryBase = len(obj(p["history_base"])) > 0
		emit("session_meta", "session_meta.payload", Obj{"client_version": s.ClientVersion, "source": s.Source, "has_history_base": s.HasHistoryBase})
	case "turn_context":
		s.CurrentTurn = first(str(p["turn_id"]), s.CurrentTurn)
		settings := obj(obj(p["collaboration_mode"])["settings"])
		model := first(str(p["model"]), str(settings["model"]))
		effort := first(str(p["effort"]), str(p["reasoning_effort"]), str(p["model_reasoning_effort"]), str(settings["reasoning_effort"]))
		if model != "" && model != s.Model {
			s.ContextValid = false
			s.ContextWindow = 0
		}
		// Model/effort are per-turn snapshots. The separately applied thread
		// tier remains available when this snapshot omits that optional field.
		tier, tierSource, tierAt := contextServiceTier(s, p, at)
		changed := model != s.Model || effort != s.Effort || tier != s.Tier
		old := s.Model
		s.Model = model
		s.Effort = effort
		s.Tier, s.TierSource, s.TierAt = tier, tierSource, tierAt
		s.SettingsAt = at
		s.ModelField = "turn_context.payload.model"
		if str(p["model"]) == "" && model != "" {
			s.ModelField = "turn_context.payload.collaboration_mode.settings.model"
		}
		if n := num(p["model_context_window"]); n > 0 {
			s.ContextWindow = n
		}
		s.CWD = first(str(p["cwd"]), s.CWD)
		if changed {
			emit("settings", s.ModelField, Obj{"previous_model": old, "model": model, "effort": effort, "service_tier": s.Tier})
		}
	case "compacted":
		s.ContextValid = false
		s.Compactions++
		emit("compaction", "compacted", Obj{"note": "context estimate invalidated; wait for a new usage snapshot"})
	case "response_item":
		k := str(p["type"])
		if k == "function_call" || k == "custom_tool_call" || k == "web_search_call" || k == "local_shell_call" {
			id := first(str(p["call_id"]), str(p["id"]))
			if id == "" || !f.SeenToolIDs[id] {
				s.ToolCalls++
				if id != "" {
					f.SeenToolIDs[id] = true
				}
				if len(f.SeenToolIDs) > 8192 {
					f.SeenToolIDs = map[string]bool{id: true}
				}
				emit("tool_call", "response_item.payload.type", Obj{"tool": first(str(p["name"]), k), "call_id": id})
			}
		}
	case "event_msg":
		k := str(p["type"])
		switch k {
		case "token_count":
			info := obj(p["info"])
			if total := usage(info["total_token_usage"]); total != nil {
				s.Total = total
			}
			if latest := usage(info["last_token_usage"]); latest != nil {
				s.Latest = latest
				s.UsageAt = at
			}
			if n := num(info["model_context_window"]); n > 0 {
				s.ContextWindow = n
			}
			if len(info) > 0 {
				sig := hashBytes(jsonBytes(Obj{"info": info, "turn": s.CurrentTurn, "model": s.Model, "effort": s.Effort}))
				if sig != f.UsageSignature {
					if usage(info["last_token_usage"]) != nil {
						s.ContextValid = true
					}
					f.UsageSignature = sig
					e := emit("usage_snapshot", "event_msg.payload.info", nil)
					e.Usage = s.Latest
					e.Total = s.Total
					e.Detail = Obj{"context_window": s.ContextWindow}
				}
			}
			qs := extractQuotas(p["rate_limits"], at, "event_msg.payload.rate_limits")
			if len(qs) > 0 {
				// Timestamp does not make repeated quota values a new historical datapoint.
				copyQ := make([]Quota, len(qs))
				copy(copyQ, qs)
				for i := range copyQ {
					copyQ[i].At = ""
				}
				sig := hashBytes(jsonBytes(copyQ))
				for _, q := range qs {
					s.Quotas[q.ID] = q
				}
				if sig != f.QuotaSignature {
					f.QuotaSignature = sig
					e := emit("quota_snapshot", "event_msg.payload.rate_limits", nil)
					e.Quotas = qs
				}
			}
		case "task_started", "turn_started":
			id := first(str(p["turn_id"]), str(r["id"]))
			if id != "" {
				s.CurrentTurn = id
			}
			if id == "" || id != f.LastStartedTurn {
				s.Turns++
				f.LastStartedTurn = id
				emit("turn_started", "event_msg.payload.type", nil)
			}
			if n := num(p["model_context_window"]); n > 0 {
				s.ContextWindow = n
			}
		case "task_complete", "turn_complete", "turn_completed", "turn_aborted":
			status := first(str(p["status"]), k)
			if len(obj(p["error"])) > 0 {
				status = "failed"
			}
			e := emit("turn_completed", "event_msg.payload.type", Obj{"status": status, "duration_ms": num(p["duration_ms"])})
			if v := str(p["turn_id"]); v != "" {
				e.Turn = v
			}
		case "context_compacted":
			s.ContextValid = false
			s.Compactions++
			emit("compaction", "event_msg.payload.type", nil)
		case "thread_rolled_back":
			s.ContextValid = false
			emit("rollback", "event_msg.payload.type", Obj{"num_turns": num(p["num_turns"])})
		case "model_reroute", "model_rerouted":
			e := emit("model_reroute", "event_msg.payload", Obj{"from_model": ident(p["from_model"]), "to_model": ident(p["to_model"]), "reason": ident(p["reason"])})
			cpy := *e
			s.LastReroute = &cpy
		case "safety_buffering":
			emit("safety_buffering", "event_msg.payload.type", Obj{"model": ident(p["model"]), "note": "safety buffering is not a capacity/routing verdict"})
		case "thread_settings_applied":
			t := obj(p["thread_settings"])
			owner := str(p["thread_id"])
			// Copied settings may belong to a different thread. Do not overwrite this one.
			if owner != "" && s.Thread != "" && owner != s.Thread {
				break
			}
			old := s.Model
			s.Model = str(t["model"])
			s.Effort = str(t["reasoning_effort"])
			applyThreadServiceTier(s, t, owner, at)
			s.SettingsAt = at
			s.ModelField = "event_msg.payload.thread_settings.model"
			if old != s.Model {
				s.ContextValid = false
			}
			emit("settings", s.ModelField, Obj{"model": s.Model, "effort": s.Effort, "service_tier": s.Tier, "owner_thread_id": owner})
		case "raw_response_completed":
			e := emit("response_completed", "event_msg.payload.raw_response_completed", Obj{"note": "local completion record; no model identity inferred"})
			e.Response = str(p["response_id"])
			e.Usage = usage(p["token_usage"])
		case "error", "stream_error", "warning", "auth_recovery":
			code := errorCode(p)
			category := classifyError(code + " " + str(p["message"]))
			e := emit("error", "event_msg.payload.type", Obj{"event_type": k, "category": category, "code": code, "message_omitted": true})
			cpy := *e
			s.LastError = &cpy
		case "agent_message", "agent_reasoning", "user_message", "mcp_startup_update", "mcp_startup_complete", "exec_command_begin", "exec_command_end", "item_started", "item_completed":
			// Intentionally ignored: may include prompts, reasoning, commands and tool outputs.
		default:
			s.UnknownEvents++
		}
	default:
		// Optional app-server JSON notifications found in imported JSONL. Not an IPC subscription.
		method := str(r["method"])
		params := obj(r["params"])
		if method == "model/rerouted" {
			e := emit("model_reroute", "method=model/rerouted.params", Obj{"from_model": ident(params["fromModel"]), "to_model": ident(params["toModel"]), "reason": ident(params["reason"])})
			e.Turn = str(params["turnId"])
			cpy := *e
			s.LastReroute = &cpy
		} else {
			s.UnknownEvents++
		}
	}
	for _, e := range events {
		appendRecent(f, e)
	}
	return events
}
func errorCode(p Obj) string {
	if e := obj(p["error"]); len(e) > 0 {
		return first(ident(e["code"]), ident(e["type"]))
	}
	if v := ident(p["codex_error_info"]); v != "" {
		return v
	}
	for k := range obj(p["codex_error_info"]) {
		return limitText(k, 120)
	}
	return ident(p["code"])
}
func classifyError(s string) string {
	s = strings.ToLower(s)
	switch {
	case strings.Contains(s, "not supported") || strings.Contains(s, "unsupported") || strings.Contains(s, "model_not_found") || strings.Contains(s, "not entitled"):
		return "UNSUPPORTED"
	case strings.Contains(s, "at capacity") || strings.Contains(s, "overload") || strings.Contains(s, "capacity"):
		return "CAPACITY"
	case strings.Contains(s, "rate_limit") || strings.Contains(s, "usage limit") || strings.Contains(s, "quota") || strings.Contains(s, "429"):
		return "RATE_LIMIT"
	case strings.Contains(s, "unauthorized") || strings.Contains(s, "401") || strings.Contains(s, "authentication") || strings.Contains(s, "not logged"):
		return "AUTH"
	case strings.Contains(s, "timeout") || strings.Contains(s, "timed out"):
		return "TIMEOUT"
	case strings.Contains(s, "connect") || strings.Contains(s, "network") || strings.Contains(s, "dns") || strings.Contains(s, "tls"):
		return "NETWORK"
	case strings.Contains(s, "retry") || strings.Contains(s, "reconnect"):
		return "RETRY"
	default:
		return "OTHER"
	}
}
func extractWindow(x any) *Window {
	m := obj(x)
	if _, ok := m["used_percent"]; !ok {
		return nil
	}
	w := &Window{Used: decimal(m["used_percent"])}
	if v, ok := m["window_minutes"]; ok && v != nil {
		n := num(v)
		w.Minutes = &n
	}
	if v, ok := m["resets_at"]; ok && v != nil {
		n := num(v)
		w.Reset = &n
	}
	return w
}
func extractQuotas(x any, at, source string) []Quota {
	out := []Quota{}
	m := obj(x)
	if len(m) == 0 {
		return out
	}
	add := func(id string, v Obj) {
		p := extractWindow(v["primary"])
		s := extractWindow(v["secondary"])
		if p == nil && s == nil {
			return
		}
		id = first(ident(v["limit_id"]), id, "unlabeled")
		out = append(out, Quota{ID: id, Name: ident(v["limit_name"]), Plan: ident(v["plan_type"]), Primary: p, Secondary: s, At: at, Source: source})
	}
	if _, ok := m["primary"]; ok {
		add("unlabeled", m)
	} else if _, ok := m["secondary"]; ok {
		add("unlabeled", m)
	}
	for _, key := range []string{"rate_limits_by_limit_id", "rateLimitsByLimitId", "limits"} {
		for id, v := range obj(m[key]) {
			add(id, obj(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func cloneState(f *FileState) *FileState {
	var c FileState
	_ = json.Unmarshal(jsonBytes(f), &c)
	if c.SeenToolIDs == nil {
		c.SeenToolIDs = map[string]bool{}
	}
	return &c
}
