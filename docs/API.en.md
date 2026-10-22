# API and state contract

[简体中文](API.zh-CN.md) · [Project home](../README.en.md) · [Integration](INTEGRATION.en.md) · [Build](BUILD.en.md) · [Six-layer visuals](LAYERS.en.md)

This document describes the local interfaces, states, and freshness rules used by the 0.4.9 display, plus the retained original-console interfaces. The visual version is 0.4.9; the underlying Go state engine retains the `0.4.8-nacre` version identifier. The overlay-built backend supplies the new control endpoints.

## 1. Address, authentication, and requests

The backend listens on an OS-assigned `127.0.0.1` port and writes private `instance.json` in Data. Its `url` has the form `http://127.0.0.1:<port>/#<token>`. The address and token may change on every backend start, so reread the instance file.

API requests use `X-Observatory-Token`. The backend checks Host; an Origin, when present, must match the backend address; `Sec-Fetch-Site: cross-site` is rejected. Responses use `Cache-Control: no-store`. The native display connects only to an exact IPv4 loopback instance URL and excludes port 10854. The page never receives the token.

The following PowerShell example only reads state and prints readiness and row count. Point it at your own instance; it does not print the token or full snapshot.

```powershell
$instance = Get-Content -LiteralPath 'C:\CodexObservatoryData\instance.json' -Raw | ConvertFrom-Json
$uri = [Uri]$instance.url
if ($uri.Scheme -ne 'http' -or $uri.Host -ne '127.0.0.1' -or $uri.Port -eq 10854 -or $uri.AbsolutePath -ne '/' -or !$uri.Fragment) {
  throw 'Expected a local Observatory backend instance'
}
$origin = $uri.GetLeftPart([UriPartial]::Authority)
$headers = @{ 'X-Observatory-Token' = $uri.Fragment.Substring(1) }
$snapshot = Invoke-RestMethod -Method Get -Uri "$origin/api/floating-state" -Headers $headers
[pscustomobject]@{ ready = $snapshot.ready; rows = @($snapshot.rows).Count }
```

POST uses `Content-Type: application/json` and the corresponding JSON body, up to 64 KiB. Struct-based requests reject unknown fields, and all requests reject a second trailing JSON value. Authentication failures return 403, unsupported methods 405, invalid JSON or parameters 400, and an unsuitable Content-Type 415. Some console operations additionally return conflict or local I/O errors.

## 2. Display endpoints

| Endpoint | Request | Response and effect |
|---|---|---|
| `/api/floating-state` | Recommended `GET` | Current Snapshot for display consumption. The old handler's common middleware permits GET/POST without an additional GET-only restriction. |
| `/api/display-controls` | `GET` | `{version, route_sync}`. Reads current synchronization state and strictly requires GET. |
| `/api/routing/pulse` | `POST {}` | `{accepted: boolean}`. Wakes the existing incremental reader. |
| `/api/routing/resync` | `POST {}` | `{accepted: boolean, route_sync: {...}}`. Requests manual reconnection and replay of available evidence. |
| `/api/shutdown` | `POST {}` | Returns `{ok: true}`, then gracefully stops the backend; does not remove the watcher or proxy configuration. |

HTTP 200 and `accepted:true` mean a request was accepted, not that new response evidence has arrived. Normal display operation uses pulse automatically; resync and shutdown follow explicit user menu selections. `--live-check` performs only snapshot GET and page acknowledgement.

## 3. Snapshot format

This is a synthetic example. When using it as a live fixture, update `at` and other timestamps to the current test clock.

```json
{
  "mode": "observed",
  "revision": 42,
  "at": "2026-10-23T12:00:00Z",
  "ready": true,
  "pending_completed": 0,
  "rows": [
    {
      "thread_id": "11111111-2222-3333-4444-555555555555",
      "turn_id": "example-turn",
      "response_id": "example-response",
      "model": "example-model",
      "requested_model": "example-model",
      "reported_model": "example-model",
      "route": "match",
      "color": "green",
      "activity": "running",
      "activity_scope": "turn",
      "health_valid": true,
      "turn_anomaly": false,
      "started_at": "2026-10-23T11:59:50Z",
      "outputting": true,
      "output_activity": 48.0,
      "estimated_visible_tokens_per_second": 19.6,
      "speed": "—/s",
      "speed_valid": false,
      "speed_fresh": false
    }
  ],
  "quota": {
    "known": true,
    "fresh": true,
    "remaining_percent": 87,
    "bucket": "example-weekly",
    "at": "2026-10-23T11:59:00Z"
  },
  "diagnostics": {
    "duplicates": 0,
    "ignored": 0,
    "capacity_evictions": 0
  }
}
```

### 3.1 Top-level and quota fields

| Field | Meaning |
|---|---|
| `mode` | `observed` for normal backend data, `synthetic` for previews; the disconnected host uses `unconnected`. |
| `revision` | Data-engine revision; equal revisions can still have different time-dependent state. |
| `at` | UTC snapshot time. The receiver independently records actual receive time. |
| `ready` | Whether global capture health is ready and fresh; not an attestation of upstream service availability. |
| `rows` | Current rows, including bounded completion transitions; not full conversation history. |
| `pending_completed` | Original-backend completion count. The new floating display does not use it for a historical inbox or blue dot. |
| `diagnostics` | Duplicate, ignored-event, and capacity-eviction counts. |
| `quota.known` | Whether usable quota evidence exists; unknown is not 0%. |
| `quota.fresh` | Whether quota evidence is within its freshness window. |
| `quota.remaining_percent` | Remaining percentage when known, from 0 to 100. |
| `quota.bucket`, `quota.at` | Optional quota-bucket identifier and evidence time. |

### 3.2 Row fields

| Field | Meaning |
|---|---|
| `thread_id`, `turn_id`, `response_id` | Separate thread, turn, and response identities; response ID may be omitted. |
| `model` | Current display label, preferring the service-reported label when available. |
| `requested_model`, `reported_model` | Requested and service-reported labels; the latter may be omitted when unavailable. |
| `route`, `color` | Label comparison and display color after health/anomaly precedence. |
| `activity`, `activity_scope` | Activity state and whether it belongs to a whole turn or one response. |
| `health_valid` | The row is associated with a valid client PID in the health state. |
| `turn_anomaly` | Retained label-mismatch evidence for the current turn. |
| `started_at`, `ended_at` | Start and end times; an unfinished row's end time may be a zero value or omitted. |
| `outputting` | The current response is unfinished and recent visible output is still fresh. |
| `output_activity` | Visible characters per second as activity, optional. |
| `estimated_visible_tokens_per_second` | Approximate visible-text units per second, optional. |
| `tokens_per_second` | Measured rate for a valid completed response, optional. |
| `speed_valid`, `speed_fresh`, `speed` | Validity, freshness, and formatted text for a completion measurement; not precision flags for a streaming estimate. |

## 4. Route, activity, and color

### 4.1 Route states

| `route` | Meaning |
|---|---|
| `pending` | Requested or reported label is missing; evidence is incomplete. |
| `match` | Requested and reported labels match case-insensitively. |
| `version_variant` | A recognized version variant, including a parseable date suffix. |
| `conflicting_evidence` | Available protocol evidence conflicts. |
| `mismatch` | Different requested/reported labels were observed and recorded as a turn anomaly. |

These states compare protocol labels, not independently establish which model weights ran. A version variant is not authentication of identical weights either.

### 4.2 Activity states

| `activity` | Meaning |
|---|---|
| `running` | The turn or response is still running; text output is not guaranteed at that instant. |
| `tool_wait` | A tool phase or waiting for a tool. |
| `completed` | A completion event for the relevant lifecycle was received. |
| `failed` | A failure event for the relevant lifecycle was received. |
| `cancelled` | A cancellation event for the relevant lifecycle was received. |

`activity_scope=turn` denotes a full task turn, while `activity_scope=response` denotes an auxiliary response without its own task turn, such as automatic naming. `ResponseFinished` ends only that response; a tool or multi-response turn still needs its actual turn-finished event. A later auxiliary request does not replace a known active task turn on the same thread.

### 4.3 Color precedence

| Color | Condition |
|---|---|
| green | `health_valid=true`, no retained turn anomaly, and `route=match`. |
| red | `health_valid=true` and `turn_anomaly=true`; a later match in the same turn does not erase the anomaly. |
| yellow | Insufficient health or evidence, or pending/variant/conflict without a higher-priority anomaly; expired disconnections also use yellow. |

Color and activity are independent dimensions. Red is not a generic state for every error, and green does not guarantee success; for example, `activity=failed` can coexist with matching model labels.

## 5. Timing, rates, and capacity

| Item | Current contract |
|---|---|
| Health freshness | 10 seconds by default; a row also requires a valid client PID association. |
| Output active | The response is unfinished, with its latest output event less than 1500ms old at snapshot time. |
| Completion-rate freshness | A finished valid response's rate evidence is between 0 and 1100ms old, excluding the upper bound. |
| Weekly quota | Accepts only a 10080-minute window; fresh for 15 minutes by default. |
| Completion transition | Retained for 8 seconds normally, or 30 seconds with a label anomaly. |
| Activity/state eviction | Activity defaults to 2 hours; state to 24 hours, also subject to client retirement and capacity rules. |
| Backend capacity | At most 256 threads, 32 responses per thread, and 4096 deduplication entries. |
| Actual visible rows | At most 16; the work area may reduce this further, with scrolling retained. |

The streaming estimate uses actual visible-text increments: roughly 0.25 units per ASCII character, 1 per CJK-like character, and 0.5 for other characters, published at intervals of at least about 200ms. Without that estimate, the display can use `output_activity × 0.4`. This is not service tokenization or a billing reading.

Completion measurement uses valid `output_tokens / elapsed_seconds`, with response association and interval coverage determining `speed_valid`; `speed_fresh` separately controls tail freshness. The display uses bounded-tangent 0.65-second interpolation and updates one decimal place on each presented frame. Interpolation does not overwrite raw API values or add random digits. Stable observations can produce a stable display, and absent output is not fabricated as continuing output.

The page receiver keeps one latest-snapshot slot and validates generation, sequence, and source-time ordering. If receive age or Snapshot.at age exceeds 10 seconds, it marks state not ready, turns rows yellow, sets `outputting=false` and `speed_fresh=false`, and marks quota stale. Repeated reads or delayed delivery cannot refresh old source evidence.

The native worker waits about 250ms between requests with at most one HTTP request in flight; slow requests extend the interval. It sends the next page delivery only after consumption acknowledgement, coalescing to the latest snapshot under load. It does not accumulate historical frames or promise fixed 4Hz arrivals or per-token alignment. Clients must not ignore new time-dependent state merely because `revision` is unchanged.

## 6. Five-second pulse and manual resync

A normal observed connection attempts `POST /api/routing/pulse` every five seconds by default; it can be disabled in the menu. A pulse only wakes the existing liveFeedLoop and continues its current epoch/sequence cursor. The original roughly 200ms feed-reading loop remains in place.

A pulse has one pending slot. It returns `accepted:false` when the reader is busy, work is already pending, manual sync is busy, demo/preview is active, the owner has stopped, or the loop is not established. These are normal skip/coalescing conditions, not necessarily errors. Failures and system suspension do not cause catch-up requests.

A pulse does not start a collector, create another reader thread, replay history, issue a model request, or reconfigure networking. `accepted:true` confirms only that the wake-up was accepted.

Manual `POST /api/routing/resync` uses the original reconnect and checkpoint-replay behavior, preserving source time, identity, deduplication, and anomaly evidence. It cannot recover evidence the collector never received. POST returns the immediate state; read later results through `GET /api/display-controls`.

```json
{
  "version": "0.4.8-nacre",
  "route_sync": {
    "state": "idle",
    "message": "",
    "at": ""
  }
}
```

| `route_sync.state` | Meaning |
|---|---|
| `idle` | No synchronization has run. |
| `syncing` | Resynchronization is in progress. |
| `health_pending` | Data synchronized; capture health remains unconfirmed. |
| `collector_update_required` | An older collector may miss this response type and needs updating. |
| `awaiting_evidence` | Resynchronized; waiting for new response evidence. |
| `synced_with_anomaly` | Resynchronized; existing anomaly evidence is retained. |
| `synced` | Current model evidence is restored, or no session is active; message distinguishes the two. |

Menu synchronization results are shown for at most five minutes from their source `at`. The readback window is bounded to about 30 seconds; prolonged uncertainty releases the UI gate and permits user retry, without an automatic loop of manual resync requests.

## 7. Original-console interfaces

The original backend retains these interfaces under the same local authentication. The new floating layer provides a console entry point without loading conversation history or the completion inbox. Operation fields follow the existing console; this table indexes the endpoints.

| Endpoint | Method and effect |
|---|---|
| `/api/state` | `GET`; console state, selection, scan status, and local evidence overview. |
| `/api/history?key=<session-key>` | Recommended `GET`; events and route records for a valid session, each capped at 1200 with truncation flags. |
| `/api/completions` | `GET`; pending completion list. |
| `/api/completions/open` | `POST`; opens a selected completion and acknowledges it only after successful opening. |
| `/api/settings` | `GET/POST`; settings and candidate locations; scan interval 2–60 seconds and at most 8 explicit roots. |
| `/api/pin` | `POST`; pins a session and optionally sets its local display name. |
| `/api/mark` | `POST`; saves a manual observation. |
| `/api/export` | `POST`; exports a local-evidence ZIP. |
| `/api/probe` | `POST`; an explicitly requested independent new request that may consume model quota. |
| `/api/cancel-probe` | `POST`; cancels a running independent check. |
| `/api/rescan` | `POST`; requests a local-index rescan. |
| `/api/open-data` | `POST`; opens the data directory. |
| `/api/self-test` | `POST`; runs offline checks and saves the report. |

An independent probe does not reconstruct a historical Desktop turn, and protocol model fields in an export are not third-party model authentication. The new display host does not call probe automatically.

## 8. Storage and extension boundaries

The new display's disk state contains only window preferences and WebView2 cache; memory holds current state and bounded animation resources. The original backend still reads local Codex JSONL and stores indexes, routing evidence, completion state, manual marks, and optional exports. Keep real instance files, data directories, CA private keys, and configuration backups private.

The lower-level metadata feed is a separate loopback service discovered through private `routing-live-feed.json`. It uses `Authorization: Bearer <token>` with `/events?epoch=<epoch>&after=<sequence>`, not the display API's X-Observatory-Token protocol. Ordinary display integration can use `/api/floating-state` directly.

See [integration](INTEGRATION.en.md) for data sources and proxy/certificate setup, and [six-layer visuals](LAYERS.en.md) for scene and effect parameters.
