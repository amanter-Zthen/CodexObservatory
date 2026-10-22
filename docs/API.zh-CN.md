# API 与状态契约

[English](API.en.md) · [项目首页](../README.md) · [接入](INTEGRATION.zh-CN.md) · [构建](BUILD.zh-CN.md) · [六层视觉](LAYERS.zh-CN.md)

本文说明 0.4.9 显示使用的本地接口、状态与时效，以及保留的原控制台接口。视觉版本为 0.4.9；底层 Go 状态引擎保留 `0.4.8-nacre` 版本标识。新控制接口由覆盖层后端提供。

## 1. 地址、鉴权和请求

后端监听系统分配的 `127.0.0.1` 端口，并在 Data 目录写出私有 `instance.json`。其中 `url` 形如 `http://127.0.0.1:<port>/#<token>`。地址和令牌每次后端启动都可能改变，应重新读取实例文件。

API 请求使用 `X-Observatory-Token`。后端核对 Host；存在 Origin 时必须与后端地址一致；`Sec-Fetch-Site: cross-site` 被拒绝。响应使用 `Cache-Control: no-store`。原生显示只连接精确的 IPv4 loopback 实例 URL，并排除端口 10854，网页不接触令牌。

以下 PowerShell 示例只读取状态，并只显示就绪标志及行数。路径指向自己的实例，不输出令牌或完整快照。

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

POST 使用 `Content-Type: application/json` 和对应 JSON body，上限 64 KiB；结构化请求拒绝未知字段，所有请求拒绝尾随第二个 JSON。鉴权失败返回 403，不支持的方法返回 405，JSON 或参数无效返回 400，Content-Type 不适用返回 415。部分控制台操作另有冲突或本地 I/O 错误响应。

## 2. 显示接口

| 接口 | 请求 | 返回与作用 |
|---|---|---|
| `/api/floating-state` | 推荐 `GET` | 当前 Snapshot，供显示消费。旧 handler 的公共中间件允许 GET/POST，不额外强制 GET-only。 |
| `/api/display-controls` | `GET` | `{version, route_sync}`。只读当前同步状态，严格限制 GET。 |
| `/api/routing/pulse` | `POST {}` | `{accepted: boolean}`。唤醒现有增量读取循环。 |
| `/api/routing/resync` | `POST {}` | `{accepted: boolean, route_sync: {...}}`。请求手动重连和已有证据重放。 |
| `/api/shutdown` | `POST {}` | `{ok: true}` 后正常停止后端；不解除 watcher 或代理配置。 |

HTTP 200 和 `accepted:true` 表示请求被接受，不保证已经取得新响应证据。正常显示自动使用 pulse；resync 与 shutdown 由用户明确选择菜单触发。`--live-check` 只执行快照 GET 和网页接收确认。

## 3. Snapshot 格式

以下为合成示例。用作实时夹具时，将 `at` 等时间更新到当前测试时钟。

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

### 3.1 顶层与额度字段

| 字段 | 含义 |
|---|---|
| `mode` | 正常后端为 `observed`，合成预览为 `synthetic`；宿主未连接状态使用 `unconnected`。 |
| `revision` | 数据引擎修订号；相同修订仍可能有不同的时间相关状态。 |
| `at` | Snapshot 的 UTC 时间。接收端还独立记录真实接收时间。 |
| `ready` | 全局采集健康是否就绪且新鲜；不是上游服务可用性认证。 |
| `rows` | 当前行，包括有限的结束过渡；不是完整会话历史。 |
| `pending_completed` | 原后端完成计数。新悬浮窗不据此显示历史收件箱或蓝点。 |
| `diagnostics` | 重复、忽略和容量淘汰计数。 |
| `quota.known` | 是否有可用的额度证据；未知不等于 0%。 |
| `quota.fresh` | 额度是否在有效新鲜度窗口内。 |
| `quota.remaining_percent` | 已知时的剩余百分比，范围 0–100。 |
| `quota.bucket`, `quota.at` | 可选额度桶标识与证据时间。 |

### 3.2 行字段

| 字段 | 含义 |
|---|---|
| `thread_id`, `turn_id`, `response_id` | 独立的会话、回合和响应身份；响应 ID 可省略。 |
| `model` | 当前显示标签，有服务报告标签时优先使用它。 |
| `requested_model`, `reported_model` | 请求标签与服务报告标签；缺少报告时后者可省略。 |
| `route`, `color` | 标签比较结果及考虑健康、异常优先级后的显示色。 |
| `activity`, `activity_scope` | 活动状态及其属于整个回合还是单个响应。 |
| `health_valid` | 当前行已关联到健康状态中有效的客户端 PID。 |
| `turn_anomaly` | 当前回合保留的标签不匹配证据。 |
| `started_at`, `ended_at` | 开始与结束时间；尚未结束时结束时间可能为空值或省略。 |
| `outputting` | 当前响应未结束，且最近可见输出仍新鲜。 |
| `output_activity` | 可见字符/秒的活动量，可省略。 |
| `estimated_visible_tokens_per_second` | 可见文字近似单位/秒，可省略。 |
| `tokens_per_second` | 有效完成响应的测量速率，可省略。 |
| `speed_valid`, `speed_fresh`, `speed` | 完成测量的有效性、新鲜度和格式化文本；不是运行估值的精度标记。 |

## 4. 路由、活动与颜色

### 4.1 路由状态

| `route` | 含义 |
|---|---|
| `pending` | 请求或报告标签缺失，证据尚不完整。 |
| `match` | 请求与报告标签忽略大小写后相同。 |
| `version_variant` | 已识别版本变体，包括可解析的日期后缀。 |
| `conflicting_evidence` | 已有协议证据相互冲突。 |
| `mismatch` | 已取得不同的请求／报告标签，并记录为本回合异常。 |

这些状态比较协议标签，不独立证明服务实际运行的模型权重。版本变体也不是相同权重的认证。

### 4.2 活动状态

| `activity` | 含义 |
|---|---|
| `running` | 回合或响应仍在运行；不保证此刻正在输出文字。 |
| `tool_wait` | 工具阶段或等待工具。 |
| `completed` | 已收到相应生命周期的完成事件。 |
| `failed` | 已收到相应生命周期的失败事件。 |
| `cancelled` | 已收到相应生命周期的取消事件。 |

`activity_scope=turn` 表示整个任务回合，`activity_scope=response` 表示没有独立任务回合的辅助响应，例如自动命名。`ResponseFinished` 只结束该响应；工具或多响应回合仍需真正的回合结束事件。后到的辅助请求不会替换同线程尚在进行的已知任务回合。

### 4.3 颜色优先级

| 颜色 | 条件 |
|---|---|
| green | `health_valid=true`、没有保留的本回合异常，且 `route=match`。 |
| red | `health_valid=true` 且 `turn_anomaly=true`；同回合后续 match 不会清掉已有异常。 |
| yellow | 健康或证据不足，或为 pending/variant/conflict 且没有更优先的异常；断线过期后也使用黄色。 |

颜色与活动是独立维度。红色不是所有错误的通用状态，绿色不是成功保证；例如 `activity=failed` 可以与匹配的模型标签同时存在。

## 5. 时间、速度和容量

| 项目 | 当前契约 |
|---|---|
| 健康新鲜度 | 默认 10 秒，行还需要有效客户端 PID 关联。 |
| 输出活跃 | 响应未结束，最近输出事件距快照时间小于 1500ms。 |
| 完成速度新鲜度 | 已结束且有效的响应，其速率证据年龄为 0–1100ms，右边界不含。 |
| 周额度 | 只接纳 10080 分钟窗口；fresh 默认 15 分钟。 |
| 结束过渡 | 正常结束保留 8 秒，有标签异常时保留 30 秒。 |
| 活动／状态淘汰 | 活动默认 2 小时；状态默认 24 小时，并受客户端退休及容量规则影响。 |
| 后端容量 | 最多 256 线程、每线程 32 响应、4096 去重项。 |
| 实际可见行 | 最多 16 行；工作区尺寸可进一步收紧，并保留滚动。 |

运行估值来自实际可见文字增量：ASCII 字符约 0.25 单位、汉字等约 1 单位、其他字符约 0.5 单位，采集输出至少间隔约 200ms 发布。没有该估值时，显示可使用 `output_activity × 0.4`。这不是服务 tokenization 或计费读数。

完成测量使用有效 `output_tokens / elapsed_seconds`，结合响应关联和时间覆盖判定 `speed_valid`；`speed_fresh` 另行控制尾显时效。显示采用 0.65 秒有界切线插值，每个已呈现帧按一位小数更新；插值不会回写原始接口值，也不会添加随机数字。稳定观测值可以稳定显示，无输出时不会假造持续输出。

网页接收层保留一个最新快照槽，校验 generation、sequence 和源时间顺序。接收年龄或 Snapshot.at 年龄超过 10 秒时，将状态改为未就绪、行标黄、`outputting=false`、`speed_fresh=false`、额度过期。重复读取或延迟投递不能刷新旧源证据。

原生 worker 在请求之间等待约 250ms，最多一个 HTTP 请求在途；慢请求会延长间隔。网页确认消费后才发送下一份，繁忙时合并为最新快照；不积压历史帧，也不承诺固定 4Hz 到达或逐 token 对齐。客户端不能仅因 `revision` 未变化就忽略新的时间相关状态。

## 6. 五秒 pulse 与手动 resync

正常 observed 连接默认每五秒尝试一次 `POST /api/routing/pulse`，可以从菜单关闭。pulse 只唤醒已存在的 liveFeedLoop，继续当前 epoch/sequence 游标；原约 200ms 的 feed 读取循环仍存在。

pulse 只有一个待处理槽。读取忙、已有待办、手动同步忙、demo/preview、owner 停止或循环尚未建立时，返回 `accepted:false`。这是正常跳过或合并，不一定是错误。失败和系统挂起后不补发积压请求。

pulse 不启动采集器、不创建另一套读取线程、不重放历史、不发送模型请求，也不重新配置网络。`accepted:true` 只确认唤醒被接受。

手动 `POST /api/routing/resync` 使用原有重连与 checkpoint 重放语义，保留源时间、身份、去重和异常记录。它不能恢复采集器从未收到的证据。POST 返回当刻状态，后续使用 `GET /api/display-controls` 读取结果。

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

| `route_sync.state` | 含义 |
|---|---|
| `idle` | 尚未执行同步。 |
| `syncing` | 正在重新同步。 |
| `health_pending` | 数据已同步，采集健康仍待确认。 |
| `collector_update_required` | 旧采集器可能漏记此类响应，需要更新。 |
| `awaiting_evidence` | 已重新同步，等待新的响应证据。 |
| `synced_with_anomaly` | 已重新同步，保留已有异常记录。 |
| `synced` | 当前模型证据已恢复，或当前没有活动会话；message 区分二者。 |

菜单同步结果按源 `at` 最多显示五分钟。读回窗口最多约 30 秒；长期未确认时释放 UI 锁并允许用户重试，不自动循环发送手动 resync。

## 7. 原控制台接口

这些接口由原后端保留，使用相同本地鉴权。新悬浮层只提供控制台入口，不加载会话历史或完成收件箱。具体操作字段沿用原控制台；以下为入口索引。

| 接口 | 方法与作用 |
|---|---|
| `/api/state` | `GET`；控制台状态、选择项、扫描与本地证据概况。 |
| `/api/history?key=<session-key>` | 推荐 `GET`；指定有效会话的事件与路由记录，分别最多 1200 项并返回截断标记。 |
| `/api/completions` | `GET`；待查看完成列表。 |
| `/api/completions/open` | `POST`；打开指定完成项，成功打开后才确认。 |
| `/api/settings` | `GET/POST`；配置与候选位置；扫描间隔 2–60 秒，最多 8 个显式 roots。 |
| `/api/pin` | `POST`；固定会话及可选本地显示名。 |
| `/api/mark` | `POST`；保存人工观察。 |
| `/api/export` | `POST`；导出本地证据 ZIP。 |
| `/api/probe` | `POST`；用户明确发起的独立新请求，可能消耗模型额度。 |
| `/api/cancel-probe` | `POST`；取消正在运行的独立检查。 |
| `/api/rescan` | `POST`；请求重扫本地索引。 |
| `/api/open-data` | `POST`；打开数据目录。 |
| `/api/self-test` | `POST`；执行离线检查并保存报告。 |

独立 probe 不复原历史 Desktop 回合；导出中的协议模型字段也不是第三方模型认证。新显示宿主不自动调用 probe。

## 8. 存储和扩展边界

新显示的磁盘状态仅为窗口偏好和 WebView2 缓存，内存保留当前状态和有限动画资源。原后端仍读取本地 Codex JSONL，保存索引、路由证据、完成状态、人工标记及可选导出。保持真实实例文件、数据目录、CA 私钥和配置备份私有。

更底层的元数据 feed 是独立 loopback 服务，由私有 `routing-live-feed.json` 发现；它使用 `Authorization: Bearer <token>` 请求 `/events?epoch=<epoch>&after=<sequence>`，不是显示 API 的 X-Observatory-Token 协议。普通显示集成使用 `/api/floating-state` 即可。

数据来源、代理与证书启用步骤见[接入指南](INTEGRATION.zh-CN.md)；画面与效果参数见[六层视觉](LAYERS.zh-CN.md)。
