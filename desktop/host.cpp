// Isolated display host. The 0.4.8 monitor owns observation and evidence.
// Build: tools/build-desktop.ps1. This executable does not launch a collector.
#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include <shellapi.h>
#include <shlwapi.h>
#include <tlhelp32.h>
#include <winhttp.h>
#include <wrl.h>
#include <WebView2.h>
#include <WebView2EnvironmentOptions.h>
#include <algorithm>
#include <atomic>
#include <chrono>
#include <filesystem>
#include <fstream>
#include <mutex>
#include <optional>
#include <regex>
#include <string>
#include <thread>
#include <vector>
#include "vendor/json.hpp"
#include "composition_host.h"

using Json = nlohmann::json;
using Microsoft::WRL::Callback;
using Microsoft::WRL::ComPtr;
namespace fs = std::filesystem;
constexpr UINT WM_LATEST = WM_APP + 1, WM_SHOW_DISPLAY = WM_APP + 2;
constexpr UINT_PTR TIMER_CLOSE = 10, TIMER_VERIFY = 11, TIMER_OWNER = 12, TIMER_HIDE = 13;
constexpr size_t MAX_BODY = 2 * 1024 * 1024;

std::wstring wide(const std::string& s) {
  if (s.empty()) return {};
  int n = MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, s.data(), int(s.size()), nullptr, 0);
  if (!n) return L"";
  std::wstring result(n, L'\0');
  MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, s.data(), int(s.size()), result.data(), n);
  return result;
}
std::string utf8(const std::wstring& s) {
  if (s.empty()) return {};
  int n = WideCharToMultiByte(CP_UTF8, 0, s.data(), int(s.size()), nullptr, 0, nullptr, nullptr);
  std::string result(n, '\0');
  WideCharToMultiByte(CP_UTF8, 0, s.data(), int(s.size()), result.data(), n, nullptr, nullptr);
  return result;
}
std::string isoNow() {
  SYSTEMTIME s; GetSystemTime(&s); char text[40];
  sprintf_s(text, "%04u-%02u-%02uT%02u:%02u:%02u.%03uZ",
    s.wYear, s.wMonth, s.wDay, s.wHour, s.wMinute, s.wSecond, s.wMilliseconds);
  return text;
}
std::optional<Json> readJson(const fs::path& p, size_t limit) {
  try {
    if (!fs::is_regular_file(p) || fs::file_size(p) > limit) return {};
    std::ifstream in(p, std::ios::binary);
    auto j = Json::parse(in, nullptr, false);
    if (j.is_discarded()) return {};
    return j;
  } catch (...) { return {}; }
}
bool saveJson(const fs::path& p, const Json& j) {
  try {
    fs::create_directories(p.parent_path());
    fs::path temp = p; temp += L".tmp";
    { std::ofstream out(temp, std::ios::binary | std::ios::trunc); out << j.dump(2); if (!out.good()) return false; }
    return MoveFileExW(temp.c_str(), p.c_str(), MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH) != 0;
  } catch (...) { return false; }
}
uint64_t identity(const std::wstring& s) {
  uint64_t h = 1469598103934665603ull;
  for (auto c : s) { h ^= uint16_t(towlower(c)); h *= 1099511628211ull; }
  return h;
}
struct Options {
  fs::path directory, state, fixture, instance, verify, liveCheck, assets;
  bool forceFallback = false, stateExplicit = false, ownerExplicit = false, verifyReopen = false;
  bool preview = false, verifyShell = false, inspectShell = false;
  DWORD ownerPid = 0;
  bool checking() const { return !verify.empty() || !liveCheck.empty(); }
};
struct UniqueHandle {
  HANDLE value = nullptr;
  explicit UniqueHandle(HANDLE h = nullptr) : value(h) {}
  ~UniqueHandle() { if (value && value != INVALID_HANDLE_VALUE) CloseHandle(value); }
  UniqueHandle(const UniqueHandle&) = delete;
  UniqueHandle& operator=(const UniqueHandle&) = delete;
  UniqueHandle(UniqueHandle&& other) noexcept : value(other.value) { other.value = nullptr; }
  UniqueHandle& operator=(UniqueHandle&& other) noexcept {
    if (value && value != INVALID_HANDLE_VALUE) CloseHandle(value);
    value = other.value; other.value = nullptr; return *this;
  }
  operator bool() const { return value && value != INVALID_HANDLE_VALUE; }
};
UniqueHandle verifiedOwner(DWORD expected) {
  if (!expected || expected == GetCurrentProcessId()) throw std::runtime_error("Invalid owner");
  UniqueHandle snapshot(CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0));
  PROCESSENTRY32W item{}; item.dwSize = sizeof(item);
  DWORD actualParent = 0;
  if (snapshot && Process32FirstW(snapshot.value, &item)) do {
    if (item.th32ProcessID == GetCurrentProcessId()) { actualParent = item.th32ParentProcessID; break; }
  } while (Process32NextW(snapshot.value, &item));
  if (actualParent != expected) throw std::runtime_error("Owner is not the direct parent");
  UniqueHandle owner(OpenProcess(SYNCHRONIZE | PROCESS_QUERY_LIMITED_INFORMATION, FALSE, expected));
  FILETIME ownerCreated{}, selfCreated{}, exited{}, kernel{}, user{};
  if (!owner || !GetProcessTimes(owner.value, &ownerCreated, &exited, &kernel, &user) ||
      !GetProcessTimes(GetCurrentProcess(), &selfCreated, &exited, &kernel, &user) ||
      CompareFileTime(&ownerCreated, &selfCreated) > 0 ||
      WaitForSingleObject(owner.value, 0) != WAIT_TIMEOUT)
    throw std::runtime_error("Owner is unavailable or has been replaced");
  // Retain this exact process handle: later PID reuse cannot change ownership.
  return owner;
}
struct Endpoint { INTERNET_PORT port = 0; std::string token; };
std::optional<Endpoint> parseEndpoint(const std::string& url) {
  // Exact local endpoint. No userinfo, DNS, query, alternate path or proxy port.
  static const std::regex pattern(R"(^http://127\.0\.0\.1:([0-9]{1,5})/#([A-Za-z0-9_.=~-]{1,512})$)");
  std::smatch m;
  if (!std::regex_match(url, m, pattern)) return {};
  const int port = std::stoi(m[1]);
  if (port <= 0 || port > 65535 || port == 10854) return {};
  return Endpoint{INTERNET_PORT(port), m[2]};
}
std::optional<Endpoint> readEndpoint(const fs::path& path) {
  auto j = readJson(path, 65536);
  if (!j || !j->is_object() || !j->contains("url") || !(*j)["url"].is_string()) return {};
  return parseEndpoint((*j)["url"].get<std::string>());
}
Json endpointChecks() {
  Json results = Json::array();
  for (const auto& test : std::vector<std::pair<std::string, bool>>{
      {"http://127.0.0.1:32123/#synthetic-token", true},
      {"http://127.0.0.1:10854/#synthetic-token", false},
      {"http://127.0.0.1:0/#synthetic-token", false},
      {"http://127.0.0.1:65536/#synthetic-token", false},
      {"http://localhost:32123/#synthetic-token", false},
      {"http://example.com:32123/#synthetic-token", false},
      {"https://127.0.0.1:32123/#synthetic-token", false},
      {"http://user@127.0.0.1:32123/#synthetic-token", false},
      {"http://127.0.0.1:32123/api/#synthetic-token", false},
      {"http://127.0.0.1:32123/?x=1#synthetic-token", false},
      {"http://127.0.0.1:32123/#", false},
      {"http://127.0.0.1:32123/#token\r\nX-Injected:1", false}}) {
    results.push_back({{"case", results.size() + 1},
      {"pass", parseEndpoint(test.first).has_value() == test.second}});
  }
  return results;
}
struct HttpHandle {
  HINTERNET value = nullptr;
  explicit HttpHandle(HINTERNET v = nullptr) : value(v) {}
  ~HttpHandle() { if (value) WinHttpCloseHandle(value); }
  operator HINTERNET() const { return value; }
  HttpHandle(const HttpHandle&) = delete;
};
std::optional<Json> requestJson(const Endpoint& e, const wchar_t* route, const std::atomic<bool>* stopping, const wchar_t* method = L"GET") {
  const auto started = std::chrono::steady_clock::now();
  if (stopping->load()) return {};
  const bool post = wcscmp(method, L"POST") == 0;
  const bool permitted = post
    ? wcscmp(route, L"/api/routing/resync") == 0 || wcscmp(route, L"/api/routing/pulse") == 0 || wcscmp(route, L"/api/shutdown") == 0
    : wcscmp(method, L"GET") == 0 && (wcscmp(route, L"/api/floating-state") == 0 || wcscmp(route, L"/api/display-controls") == 0);
  if (!permitted) return {};
  // Per-client direct loopback access; this does not change WinHTTP settings.
  HttpHandle session(WinHttpOpen(L"CodexObservatory-Display/0.4.9",
    WINHTTP_ACCESS_TYPE_NO_PROXY, WINHTTP_NO_PROXY_NAME, WINHTTP_NO_PROXY_BYPASS, 0));
  if (!session) return {};
  if (!WinHttpSetTimeouts(session, 750, 750, 1000, 1000)) return {};
  HttpHandle connection(WinHttpConnect(session, L"127.0.0.1", e.port, 0));
  if (!connection) return {};
  HttpHandle request(WinHttpOpenRequest(connection, method, route, nullptr,
    WINHTTP_NO_REFERER, WINHTTP_DEFAULT_ACCEPT_TYPES, 0));
  if (!request) return {};
  DWORD policy = WINHTTP_OPTION_REDIRECT_POLICY_NEVER;
  if (!WinHttpSetOption(request, WINHTTP_OPTION_REDIRECT_POLICY, &policy, sizeof(policy))) return {};
  const std::wstring headers = L"X-Observatory-Token: " + wide(e.token) + L"\r\nAccept: application/json\r\n" +
    (post ? L"Content-Type: application/json\r\n" : L"");
  char body[] = "{}";
  if (!WinHttpSendRequest(request, headers.c_str(), DWORD(-1),
      post ? body : WINHTTP_NO_REQUEST_DATA, post ? 2 : 0, post ? 2 : 0, 0) || !WinHttpReceiveResponse(request, nullptr)) return {};
  DWORD status = 0, size = sizeof(status);
  if (!WinHttpQueryHeaders(request, WINHTTP_QUERY_STATUS_CODE | WINHTTP_QUERY_FLAG_NUMBER,
      WINHTTP_HEADER_NAME_BY_INDEX, &status, &size, WINHTTP_NO_HEADER_INDEX) || status != 200) return {};
  std::string text;
  for (;;) {
    if (stopping->load() || std::chrono::steady_clock::now() - started > std::chrono::milliseconds(3500)) return {};
    DWORD available = 0, got = 0;
    if (!WinHttpQueryDataAvailable(request, &available)) return {};
    if (!available) break;
    if (available > MAX_BODY || text.size() > MAX_BODY - available) return {};
    const auto begin = text.size(); text.resize(begin + available);
    if (!WinHttpReadData(request, text.data() + begin, available, &got)) return {};
    text.resize(begin + got);
    if (!got) break;
  }
  Json value = Json::parse(text, nullptr, false);
  if (value.is_discarded() || !value.is_object()) return {};
  return value;
}
#include "legacy_menu.h"
std::wstring fileUri(const fs::path& file) {
  const auto raw = fs::absolute(file).generic_u8string();
  std::string encoded = "file:///";
  constexpr char digits[] = "0123456789ABCDEF";
  for (unsigned char c : raw) {
    if ((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
        c == '/' || c == ':' || c == '-' || c == '_' || c == '.' || c == '~') encoded += char(c);
    else { encoded += '%'; encoded += digits[c >> 4]; encoded += digits[c & 15]; }
  }
  return wide(encoded);
}
Json emptySnapshot(const char* mode = "unconnected") {
  return {{"rows", Json::array()}, {"quota", {{"known", false}, {"fresh", false}, {"remaining_percent", 0}}},
    {"ready", false}, {"mode", mode}, {"revision", 0}, {"at", isoNow()}, {"pending_completed", 0}};
}
Json verificationFixture(int count = 3) {
  Json s = emptySnapshot("synthetic"); s["ready"] = true;
  s["quota"] = {{"known", true}, {"fresh", true}, {"remaining_percent", 87}};
  for (int i = 0; i < count; ++i) {
    s["rows"].push_back({{"thread_id", "fixture-" + std::to_string(i)}, {"turn_id", "fixture-turn"},
      {"response_id", "fixture-response"}, {"model", i == 0 ? "gpt-6.1-sol" : i == 1 ? "gpt-6-sol" : "local-fixture"},
      {"color", i == 0 ? "green" : i == 1 ? "red" : "yellow"}, {"activity", "running"},
      {"outputting", i == 0}, {"estimated_visible_tokens_per_second", i == 0 ? 19.6 : 0},
      {"speed_valid", false}, {"speed_fresh", false}, {"health_valid", true}});
  }
  return s;
}

class Host {
public:
  Options opt;
  UniqueHandle owner;
  HWND window = nullptr;
  ComPtr<ICoreWebView2Environment> environment;
  ComPtr<ICoreWebView2Controller> controller;
  ComPtr<ICoreWebView2> web;
  CompositionHost composition;
  observatory_menu::Controls controls;
  std::wstring allowedUri, browserVersion;
  std::mutex mailbox;
  Json latest, display;
  std::string connectionStatus = "未连接：使用 --instance 指定监视器实例，或 --fixture 指定测试数据";
  std::string fallbackReason;
  uint64_t sequence = 0, latestSequence = 0, latestGeneration = 0, sentSequence = 0, ackSequence = 0;
  std::atomic<bool> posted{false}, stopping{false};
  HANDLE wake = CreateEventW(nullptr, FALSE, FALSE, nullptr);
  std::thread worker;
  bool ready = false, waiting = false, fallback = false, menuOpen = false, closing = false;
  bool automaticQuality = true, hiding = false, hiddenByUser = false, minimized = false;
  bool topmost = true, verifyStarted = false, verifyFinished = false, liveFinished = false, liveInspecting = false, ownerExited = false;
  int shellProbePhase = 0;
  unsigned dragRequests = 0, menuRequests = 0, wheelBridgeMessages = 0;
  int wheelOffsetAfterDown = -1;
  int reopenProbePhase = 0;
  uint64_t reopenBaseline = 0;
  bool reopenCloseStarted = false, reopenAccepted = false;
  double zoom = 1, appliedZoom = 1, renderMargin = 40;
  int quality = 0;
  unsigned delivered = 0, acknowledgements = 0, menuOpens = 0, fallbackPaintMessages = 0;
  std::atomic<unsigned> maximumPending{0}, maximumHttp{0};
  std::atomic<unsigned> httpAttempts{0}, validSnapshotsReceived{0};
  std::atomic<ULONGLONG> lastReceiveTick{0};
  unsigned sourceGeneration = 1;
  int exitCode = 0;
  SIZE panel{468, 240};
  RECT savedPosition{0, 0, 0, 0};
  std::chrono::steady_clock::time_point launched = std::chrono::steady_clock::now();

  explicit Host(Options o, UniqueHandle parent) : opt(std::move(o)), owner(std::move(parent)), display(emptySnapshot()) {}
  ~Host() {
    stopping = true; SetEvent(wake);
    if (worker.joinable()) worker.join();
    composition.detach();
    if (controller) controller->Close();
    web.Reset(); controller.Reset(); environment.Reset();
    CloseHandle(wake);
  }
  UINT dpi() const {
    using Fn = UINT(WINAPI*)(HWND);
    static auto fn = (Fn)GetProcAddress(GetModuleHandleW(L"user32.dll"), "GetDpiForWindow");
    return fn && window ? fn(window) : 96;
  }
  double limitZoom(double z) const {
    if (!std::isfinite(z) || z <= 0) z = 1;
    const RECT work = workArea();
    const double dip = 96. / dpi();
    const double fitWidth = (work.right - work.left) * dip / (388 + 2 * renderMargin);
    const double fitHeight = (work.bottom - work.top) * dip / (112 + 2 * renderMargin);
    const double upper = std::max(.75, std::floor(std::min({2., 4. / std::max(1., dpi() / 96.), fitWidth, fitHeight}) * 4) / 4);
    return std::clamp(std::round(z * 4) / 4, .75, upper);
  }
  RECT workArea() const {
    MONITORINFO mi{sizeof(mi)};
    GetMonitorInfoW(MonitorFromWindow(window, MONITOR_DEFAULTTONEAREST), &mi);
    return mi.rcWork;
  }
  void loadSettings() {
    if (opt.checking()) return;
    auto j = readJson(opt.state / L"display-settings.json", 65536);
    if (!j || !j->is_object()) return;
    try {
      zoom = limitZoom(j->value("zoom", 1.));
      quality = std::clamp(j->value("quality", 0), 0, 3);
      automaticQuality = j->value("automaticQuality", quality == 0);
      controls.automatic = j->value("automaticRouteSync", true);
      topmost = j->value("topmost", true);
      savedPosition.left = j->value("x", LONG(0)); savedPosition.top = j->value("y", LONG(0));
      savedPosition.right = j->value("positionSaved", false) ? 1 : 0;
    } catch (...) {}
  }
  void saveSettings() {
    if (opt.checking()) return;
    RECT r; GetWindowRect(window, &r);
    saveJson(opt.state / L"display-settings.json", {{"zoom", zoom}, {"quality", quality},
      {"automaticQuality", automaticQuality}, {"automaticRouteSync", controls.automatic.load()},
      {"topmost", topmost}, {"x", r.left}, {"y", r.top}, {"positionSaved", true}});
  }
  void resizeToPanel() {
    if (!window) return;
    RECT work = workArea(), desired{0, 0,
      LONG(std::clamp(panel.cx, LONG(300), LONG(1600)) * dpi() / 96.),
      LONG(std::clamp(panel.cy, LONG(100), LONG(1600)) * dpi() / 96.)};
    AdjustWindowRectEx(&desired, DWORD(GetWindowLongPtrW(window, GWL_STYLE)), FALSE, DWORD(GetWindowLongPtrW(window, GWL_EXSTYLE)));
    int w = std::min(desired.right - desired.left, work.right - work.left);
    int h = std::min(desired.bottom - desired.top, work.bottom - work.top);
    RECT current; GetWindowRect(window, &current);
    int x = std::clamp(current.left, work.left, std::max(work.left, work.right - w));
    int y = std::clamp(current.top, work.top, std::max(work.top, work.bottom - h));
    SetWindowPos(window, topmost ? HWND_TOPMOST : HWND_NOTOPMOST, x, y, w, h, SWP_NOACTIVATE);
    if (controller) { RECT r; GetClientRect(window, &r); controller->put_Bounds(r); composition.resize(r); }
  }
  void post(const Json& j) {
    if (web && ready && !fallback) web->PostWebMessageAsJson(wide(j.dump()).c_str());
  }
  void sendView() {
    const RECT work = workArea();
    post({{"kind", "view"}, {"zoom", zoom}, {"quality", quality}, {"automatic", automaticQuality},
      {"workHeightDIP", (work.bottom - work.top) * 96. / dpi()}});
  }
  void scrollRows(int delta) {
    if (!ready || fallback || closing || !delta) return;
    // A fixed action avoids WebView2 133's unreliable composition wheel hit
    // testing and keeps the existing canvas row-offset mechanism.
    ++wheelBridgeMessages;
    post({{"kind", "wheel"}, {"delta", delta > 0 ? -1 : 1}});
  }
  void offer(Json s, uint64_t generation) {
    {
      std::lock_guard<std::mutex> guard(mailbox);
      latest = std::move(s); latestSequence = ++sequence; latestGeneration = generation;
      maximumPending = 1;
      lastReceiveTick = GetTickCount64();
    }
    if (!posted.exchange(true)) PostMessageW(window, WM_LATEST, 0, 0);
  }
  void drain() {
    posted = false;
    Json value; uint64_t seq, generation;
    {
      std::lock_guard<std::mutex> guard(mailbox);
      if (latest.is_null()) { if (fallback) InvalidateRect(window, nullptr, FALSE); return; }
      display = latest;
      if (waiting || !ready || fallback) { InvalidateRect(window, nullptr, FALSE); return; }
      value = std::move(latest); latest = nullptr; seq = latestSequence; generation = latestGeneration;
    }
    waiting = true; sentSequence = seq; ++delivered;
    post({{"kind", "snapshot"}, {"snapshot", value}, {"generation", generation}, {"sequence", seq}});
  }
  void startWorker() {
    worker = std::thread([this] {
      std::optional<Endpoint> previous;
      uint64_t generation = 1;
      while (!stopping) {
        if (!opt.instance.empty()) {
          auto endpoint = readEndpoint(opt.instance);
          if (endpoint) {
            if (!previous || previous->port != endpoint->port || previous->token != endpoint->token) { ++generation; controls.reset(); }
            previous = endpoint;
            maximumHttp = 1;
            ++httpAttempts;
            auto s = requestJson(*endpoint, L"/api/floating-state", &stopping);
            bool automaticAllowed = false;
            if (s && s->contains("rows") && (*s)["rows"].is_array() && (*s)["rows"].size() <= 256) {
              automaticAllowed = s->contains("mode") && (*s)["mode"].is_string() && (*s)["mode"] == "observed";
              ++validSnapshotsReceived;
              { std::lock_guard<std::mutex> guard(mailbox); connectionStatus = "已连接本地监视器"; }
              offer(std::move(*s), generation);
            } else {
              std::lock_guard<std::mutex> guard(mailbox);
              connectionStatus = "连接中断：保留画面，等待重新取得新鲜快照";
            }
            // Diagnostics remain GET-only. Normal controls use this same bounded
            // worker, so a slow read skips timer opportunities instead of queuing.
            if (!opt.checking()) {
              controls.pump(*endpoint, &stopping, automaticAllowed);
              if (controls.shutdownAccepted.exchange(false)) PostMessageW(window, WM_CLOSE, 0, 0);
            }
          } else {
            controls.reset();
            std::lock_guard<std::mutex> guard(mailbox);
            connectionStatus = "实例描述不可用：等待有效的本地监视器实例";
          }
        } else if (!opt.fixture.empty()) {
          auto j = readJson(opt.fixture, MAX_BODY);
          if (j && j->contains("snapshot")) j = (*j)["snapshot"];
          if (j && j->is_object() && j->contains("rows") && (*j)["rows"].is_array()) {
            { std::lock_guard<std::mutex> guard(mailbox); connectionStatus = "隔离测试数据"; }
            offer(*j, generation);
          }
        } else {
          Json s = opt.verify.empty() && !opt.preview ? emptySnapshot() : verificationFixture(opt.verifyShell ? 20 : 3);
          offer(std::move(s), generation);
        }
        if (!posted.exchange(true)) PostMessageW(window, WM_LATEST, 0, 0);
        if (WaitForSingleObject(wake, opt.instance.empty() ? 1000 : 250) == WAIT_OBJECT_0 && stopping) break;
      }
    });
  }
  void useFallback(const char* reason) {
    fallback = true; ready = false; waiting = false;
    fallbackReason = reason;
    { std::lock_guard<std::mutex> guard(mailbox); connectionStatus = reason; }
    if (controller) controller->put_IsVisible(FALSE);
    composition.detach();
    // A failed renderer has no GPU surface. Restore a GDI redirection surface
    // only for this explicit, readable fallback; normal rendering stays alpha.
    const auto style = GetWindowLongPtrW(window, GWL_EXSTYLE);
    SetWindowLongPtrW(window, GWL_EXSTYLE, style & ~WS_EX_NOREDIRECTIONBITMAP);
    SetWindowPos(window, nullptr, 0, 0, 0, 0, SWP_NOMOVE | SWP_NOSIZE | SWP_NOZORDER | SWP_NOACTIVATE | SWP_FRAMECHANGED);
    InvalidateRect(window, nullptr, TRUE);
    if (!opt.verify.empty() && opt.forceFallback) SetTimer(window, TIMER_VERIFY, 200, nullptr);
  }
  void drawFallback(HDC dc) {
    RECT r; GetClientRect(window, &r);
    HBRUSH brush = CreateSolidBrush(RGB(243, 239, 229)); FillRect(dc, &r, brush); DeleteObject(brush);
    SetBkMode(dc, TRANSPARENT); SetTextColor(dc, RGB(45, 78, 63));
    HFONT font = CreateFontW(-MulDiv(16, dpi(), 96), 0, 0, 0, FW_NORMAL, FALSE, FALSE, FALSE,
      DEFAULT_CHARSET, OUT_DEFAULT_PRECIS, CLIP_DEFAULT_PRECIS, CLEARTYPE_QUALITY, DEFAULT_PITCH, L"Segoe UI");
    auto old = SelectObject(dc, font);
    Json s; std::string state;
    { std::lock_guard<std::mutex> guard(mailbox); s = display; state = connectionStatus; }
    std::wstring text = L"CODEX OBSERVATORY · 简化显示\r\n\r\n" + wide(state) + L"\r\n\r\n";
    const bool stale = !lastReceiveTick.load() || GetTickCount64() - lastReceiveTick.load() > 10000;
    if (stale) text += L"数据已过期，等待当前状态。\r\n\r\n";
    if (s.contains("rows") && s["rows"].is_array()) for (size_t i = 0; i < std::min<size_t>(16, s["rows"].size()); ++i) {
      const auto& row = s["rows"][i];
      if (!row.is_object()) continue;
      const std::string model = row.contains("model") && row["model"].is_string() ? row["model"].get<std::string>() : "—";
      const std::string speed = row.contains("speed") && row["speed"].is_string() ? row["speed"].get<std::string>() : "—/s";
      const std::string color = row.contains("color") && row["color"].is_string() ? row["color"].get<std::string>() : "yellow";
      const std::wstring label = stale ? L"待确认" : color == "red" ? L"异常" : color == "green" ? L"正常" : L"待确认";
      text += L"[" + label + L"] " + wide(model).substr(0, 100) + L"    " + (stale ? L"—/s" : wide(speed).substr(0, 40)) + L"\r\n";
    }
    text += L"\r\n右键：窗口大小、静态模式与关闭。";
    InflateRect(&r, -18, -18); DrawTextW(dc, text.c_str(), -1, &r, DT_LEFT | DT_TOP | DT_WORDBREAK);
    SelectObject(dc, old); DeleteObject(font);
  }
  Json fallbackPixels() {
    RECT r; GetClientRect(window, &r);
    const int w = std::max(1L, r.right), h = std::max(1L, r.bottom);
    BITMAPINFO info{}; info.bmiHeader.biSize = sizeof(BITMAPINFOHEADER);
    info.bmiHeader.biWidth = w; info.bmiHeader.biHeight = -h;
    info.bmiHeader.biPlanes = 1; info.bmiHeader.biBitCount = 32; info.bmiHeader.biCompression = BI_RGB;
    void* pixels = nullptr; HDC dc = CreateCompatibleDC(nullptr);
    HBITMAP bitmap = CreateDIBSection(dc, &info, DIB_RGB_COLORS, &pixels, nullptr, 0);
    if (!dc || !bitmap) { if (bitmap) DeleteObject(bitmap); if (dc) DeleteDC(dc); return {{"nonBackgroundPixels", 0}}; }
    auto old = SelectObject(dc, bitmap); drawFallback(dc); GdiFlush();
    size_t nonBackground = 0;
    for (size_t i = 0; i < size_t(w) * h; ++i)
      if ((((DWORD*)pixels)[i] & 0xffffff) != ((243u << 16) | (239u << 8) | 229u)) ++nonBackground;
    SelectObject(dc, old); DeleteObject(bitmap); DeleteDC(dc);
    return {{"fallback", true}, {"pixels", size_t(w) * h}, {"nonBackgroundPixels", nonBackground}};
  }
  void beginClose() {
    // A user may cancel an ordinary close by reopening from the tray.
    // Keep its existing worker alive until WM_DESTROY. Terminal ownership or
    // diagnostic completion stops new reads immediately and cannot be reopened.
    if (ownerExited || verifyFinished || liveFinished) { stopping = true; SetEvent(wake); }
    if (closing) return;
    hiding = false; KillTimer(window, TIMER_HIDE);
    closing = true; post({{"kind", "close"}});
    SetTimer(window, TIMER_CLOSE, 6000, nullptr);
    if (!ready || fallback) DestroyWindow(window);
  }
  bool showWindow() {
    if (ownerExited || verifyFinished || liveFinished || stopping.load()) return false;
    if (owner && WaitForSingleObject(owner.value, 0) != WAIT_TIMEOUT) { ownerTick(); return false; }
    closing = hiding = hiddenByUser = false;
    KillTimer(window, TIMER_CLOSE); KillTimer(window, TIMER_HIDE);
    if (!opt.checking()) ShowWindow(window, opt.inspectShell ? SW_SHOWNORMAL : SW_SHOWNOACTIVATE);
    if (controller) controller->put_IsVisible(TRUE);
    ComPtr<ICoreWebView2_3> w3; if (web && SUCCEEDED(web.As(&w3))) w3->Resume();
    post({{"kind", "show"}}); drain();
    return true;
  }
  void finishHide() {
    if (!hiding || closing) return;
    hiding = false; hiddenByUser = true; KillTimer(window, TIMER_HIDE);
    post({{"kind", "hidden"}});
    if (controller) controller->put_IsVisible(FALSE);
    ShowWindow(window, SW_HIDE);
  }
  void beginHide() {
    if (closing || hiding || hiddenByUser) return;
    hiding = true; post({{"kind", "hide"}});
    SetTimer(window, TIMER_HIDE, 6000, nullptr);
    if (!ready || fallback) finishHide();
  }
  void showMenu() {
    if (menuOpen || closing) return;
    menuOpen = true; ++menuOpens;
    using namespace observatory_menu;
    const auto endpoint = opt.instance.empty() ? std::optional<Endpoint>{} : readEndpoint(opt.instance);
    Json snapshot;
    { std::lock_guard<std::mutex> guard(mailbox); snapshot = display; }
    View v; v.targetZoom = zoom; v.appliedZoom = appliedZoom; v.maximumZoom = limitZoom(2);
    v.quality = quality; v.automatic = automaticQuality; v.topmost = topmost; v.connected = endpoint.has_value();
    v.synthetic = opt.preview || !opt.fixture.empty() ||
      (snapshot.contains("mode") && snapshot["mode"].is_string() && (snapshot["mode"] == "synthetic" || snapshot["mode"] == "demo"));
    controls.view(v);
    Popup menu; menu.build(snapshot, v);
    if (v.connected && !v.synthetic && controls.queue(Control::Refresh)) SetEvent(wake);
    POINT p; GetCursorPos(&p); SetForegroundWindow(window);
    UINT choice = TrackPopupMenu(menu.handle, TPM_RETURNCMD | TPM_RIGHTBUTTON | TPM_NOANIMATION, p.x, p.y, 0, window, nullptr);
    PostMessageW(window, WM_NULL, 0, 0); menuOpen = false;
    if (!choice || closing || ownerExited) return;
    if (auto thread = menu.threads.find(choice); thread != menu.threads.end()) {
      ShellExecuteW(nullptr, L"open", wide("codex://threads/" + thread->second).c_str(), nullptr, nullptr, SW_SHOWNORMAL);
      return;
    }
    switch (choice) {
      case Enlarge: zoom = limitZoom(zoom + .25); break;
      case Shrink: zoom = limitZoom(zoom - .25); break;
      case ResetZoom: zoom = limitZoom(1); break;
      case Auto: automaticQuality = true; quality = 0; break;
      case Full: automaticQuality = false; quality = 0; break;
      case Balanced: automaticQuality = false; quality = 1; break;
      case Economy: automaticQuality = false; quality = 2; break;
      case Still: automaticQuality = false; quality = 3; break;
      case Topmost: topmost = !topmost; resizeToPanel(); break;
      case AutoSync: if (v.connected && !v.synthetic) controls.automatic = !controls.automatic.load(); break;
      case Replay: post({{"kind", "replay"}}); return;
      case Hide: beginHide(); return;
      case Resync: if (v.connected && !v.synthetic && controls.queue(Control::Resync)) SetEvent(wake); return;
      case Quit:
        if (v.connected && !v.synthetic) { if (controls.queue(Control::Shutdown)) SetEvent(wake); }
        else beginClose();
        return;
      case Console:
        if (endpoint && !v.synthetic) {
          const auto url = L"http://127.0.0.1:" + std::to_wstring(endpoint->port) + L"/#" + wide(endpoint->token);
          ShellExecuteW(nullptr, L"open", url.c_str(), nullptr, nullptr, SW_SHOWNORMAL);
        }
        return;
      default: return;
    }
    sendView(); saveSettings();
  }
  void onMessage(ICoreWebView2WebMessageReceivedEventArgs* args) {
    LPWSTR value = nullptr, source = nullptr;
    if (FAILED(args->get_Source(&source))) return;
    const bool trusted = source && std::wstring(source) == allowedUri + L"?desktop=1&zoom=1";
    CoTaskMemFree(source); if (!trusted || FAILED(args->get_WebMessageAsJson(&value))) return;
    const auto j = Json::parse(utf8(value ? value : L""), nullptr, false); CoTaskMemFree(value);
    if (!j.is_object() || !j.contains("kind") || !j["kind"].is_string()) return;
    const std::string kind = j["kind"];
    if (kind == "ready") {
      if (opt.verifyShell) zoom = limitZoom(2);
      ready = true; fallback = false; waiting = false; sendView(); drain();
      if (opt.checking()) SetTimer(window, TIMER_VERIFY, 200, nullptr);
    } else if (kind == "consumed" && j.contains("sequence") && j["sequence"].is_number_unsigned()) {
      uint64_t ack = j["sequence"];
      if (waiting && ack == sentSequence) { waiting = false; ackSequence = ack; ++acknowledgements; drain(); }
    } else if (kind == "size" && j.contains("width") && j.contains("height") &&
        j["width"].is_number() && j["height"].is_number()) {
      const double w = j["width"], h = j["height"];
      if (std::isfinite(w) && std::isfinite(h) && w >= 100 && w <= 1600 && h >= 50 && h <= 1600) {
        if (j.contains("margin") && j["margin"].is_number()) {
          const double m = j["margin"];
          if (std::isfinite(m) && m >= 0 && m <= 160 && renderMargin != m) {
            renderMargin = m; zoom = limitZoom(zoom); sendView();
          }
        }
        SIZE next{LONG(std::ceil(w)), LONG(std::ceil(h))};
        if (panel.cx != next.cx || panel.cy != next.cy) { panel = next; resizeToPanel(); }
      }
    } else if (kind == "view-state" && j.contains("zoom") && j["zoom"].is_number()) {
      const double z = j["zoom"]; if (std::isfinite(z) && z >= .75 && z <= 2) appliedZoom = z;
    } else if (kind == "hidden-complete" && hiding) finishHide();
    else if (kind == "menu") { ++menuRequests; if (!opt.checking()) PostMessageW(window, WM_CONTEXTMENU, 0, 0); }
    else if (kind == "drag") {
      ++dragRequests;
      if (!opt.checking() && !closing) {
        ReleaseCapture(); SendMessageW(window, WM_NCLBUTTONDOWN, HTCAPTION, 0);
      }
    } else if (kind == "closed" && closing) DestroyWindow(window);
  }
  void initializeWebView() {
    if (opt.forceFallback) { useFallback("隔离故障注入：原生简化显示"); return; }
    auto options = Microsoft::WRL::Make<CoreWebView2EnvironmentOptions>();
    options->put_AdditionalBrowserArguments(L"--disable-background-networking --disable-component-update --disable-sync --no-first-run");
    auto hr = CreateCoreWebView2EnvironmentWithOptions(nullptr, (opt.state / L"WebView2").c_str(), options.Get(),
      Callback<ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler>([this](HRESULT result, ICoreWebView2Environment* env)->HRESULT {
        if (stopping) return S_OK;
        if (FAILED(result) || !env) { useFallback("WebView2 不可用：使用简化显示"); return S_OK; }
        environment = env;
        LPWSTR version = nullptr; env->get_BrowserVersionString(&version);
        if (version) { browserVersion = version; CoTaskMemFree(version); }
        ComPtr<ICoreWebView2Environment3> environment3;
        if (FAILED(environment.As(&environment3))) { useFallback("WebView2 不支持合成显示：使用简化显示"); return S_OK; }
        const HRESULT createResult = environment3->CreateCoreWebView2CompositionController(window,
          Callback<ICoreWebView2CreateCoreWebView2CompositionControllerCompletedHandler>([this](HRESULT status, ICoreWebView2CompositionController* cc)->HRESULT {
            if (stopping) return S_OK;
            if (FAILED(status) || !cc || FAILED(cc->QueryInterface(IID_PPV_ARGS(&controller)))) {
              useFallback("WebView2 合成控制器创建失败：使用简化显示"); return S_OK;
            }
            if (FAILED(composition.attach(window, cc, controller.Get()))) {
              useFallback("桌面透明合成初始化失败：使用简化显示"); return S_OK;
            }
            if (FAILED(controller->get_CoreWebView2(&web)) || !web) { useFallback("WebView2 页面不可用"); return S_OK; }
            RECT r; GetClientRect(window, &r); controller->put_Bounds(r); composition.resize(r);
            controller->put_IsVisible(TRUE);
            ComPtr<ICoreWebView2Settings> settings; web->get_Settings(&settings);
            settings->put_AreDefaultContextMenusEnabled(FALSE);
            settings->put_AreDevToolsEnabled(FALSE);
            settings->put_IsZoomControlEnabled(FALSE);
            settings->put_AreDefaultScriptDialogsEnabled(FALSE);
            EventRegistrationToken token;
            web->add_NavigationStarting(Callback<ICoreWebView2NavigationStartingEventHandler>(
              [this](ICoreWebView2*, ICoreWebView2NavigationStartingEventArgs* args)->HRESULT {
                LPWSTR uri = nullptr; args->get_Uri(&uri);
                const std::wstring u = uri ? uri : L""; CoTaskMemFree(uri);
                if (u != allowedUri + L"?desktop=1&zoom=1") args->put_Cancel(TRUE);
                return S_OK;
              }).Get(), &token);
            web->add_NewWindowRequested(Callback<ICoreWebView2NewWindowRequestedEventHandler>(
              [](ICoreWebView2*, ICoreWebView2NewWindowRequestedEventArgs* args)->HRESULT { args->put_Handled(TRUE); return S_OK; }).Get(), &token);
            web->add_PermissionRequested(Callback<ICoreWebView2PermissionRequestedEventHandler>(
              [](ICoreWebView2*, ICoreWebView2PermissionRequestedEventArgs* args)->HRESULT { args->put_State(COREWEBVIEW2_PERMISSION_STATE_DENY); return S_OK; }).Get(), &token);
            web->add_WebMessageReceived(Callback<ICoreWebView2WebMessageReceivedEventHandler>(
              [this](ICoreWebView2*, ICoreWebView2WebMessageReceivedEventArgs* args)->HRESULT { onMessage(args); return S_OK; }).Get(), &token);
            web->add_ProcessFailed(Callback<ICoreWebView2ProcessFailedEventHandler>(
              [this](ICoreWebView2*, ICoreWebView2ProcessFailedEventArgs*)->HRESULT { useFallback("绘制进程异常：原生简化显示仍可读取数据"); return S_OK; }).Get(), &token);
            web->add_NavigationCompleted(Callback<ICoreWebView2NavigationCompletedEventHandler>(
              [this](ICoreWebView2*, ICoreWebView2NavigationCompletedEventArgs* args)->HRESULT {
                BOOL success = FALSE; args->get_IsSuccess(&success);
                if (!success) useFallback("本地视觉入口无法加载：使用简化显示");
                return S_OK;
              }).Get(), &token);
            web->Navigate((allowedUri + L"?desktop=1&zoom=1").c_str());
            return S_OK;
          }).Get());
        if (FAILED(createResult)) useFallback("WebView2 合成控制器不可用：使用简化显示");
        return S_OK;
      }).Get());
    if (FAILED(hr)) useFallback("WebView2 加载器不可用：使用简化显示");
  }
  void finishVerification(const Json& page, bool passed) {
    if (verifyFinished) return;
    verifyFinished = true;
    RECT r; GetClientRect(window, &r);
    const Json endpoints = endpointChecks();
    for (const auto& test : endpoints) passed = passed && test.value("pass", false);
    const double maxZoom = limitZoom(100);
    const bool quarterStep = std::abs(maxZoom * 4 - std::round(maxZoom * 4)) < 1e-8;
    passed = passed && quarterStep;
    const auto style = DWORD(GetWindowLongPtrW(window, GWL_STYLE));
    const auto extended = DWORD(GetWindowLongPtrW(window, GWL_EXSTYLE));
    RECT outer{}; GetWindowRect(window, &outer);
    const RECT work = workArea();
    Json shell = {{"popup", (style & WS_POPUP) != 0}, {"caption", (style & WS_CAPTION) != 0},
      {"thickFrame", (style & WS_THICKFRAME) != 0}, {"toolWindow", (extended & WS_EX_TOOLWINDOW) != 0},
      {"noActivate", (extended & WS_EX_NOACTIVATE) != 0}, {"topmost", (extended & WS_EX_TOPMOST) != 0},
      {"noRedirectionBitmap", (extended & WS_EX_NOREDIRECTIONBITMAP) != 0},
      {"compositionAttached", composition.attached}, {"backgroundAlpha", composition.backgroundAlpha},
      {"clientEqualsWindow", outer.right - outer.left == r.right && outer.bottom - outer.top == r.bottom},
      {"mouseMessages", composition.mouseMessages}, {"wheelMessages", composition.wheelMessages},
      {"inputFailures", composition.inputFailures}, {"dragRequests", dragRequests}, {"menuRequests", menuRequests},
      {"renderMarginDIP", renderMargin}, {"nativeDragInsetPixels", std::round(renderMargin * appliedZoom * dpi() / 96.)},
      {"completeRenderFitsWorkArea", panel.cx * dpi() / 96. <= work.right-work.left+1 && panel.cy * dpi() / 96. <= work.bottom-work.top+1},
      {"workAreaPixels", {work.right-work.left, work.bottom-work.top}},
      {"wheelBridgeMessages", wheelBridgeMessages}, {"wheelOffsetAfterDown", wheelOffsetAfterDown}};
    if (!fallback) passed = passed && composition.attached && composition.backgroundAlpha == 0 &&
      shell.value("popup", false) && !shell.value("caption", true) && !shell.value("thickFrame", true) &&
      shell.value("toolWindow", false) && shell.value("noActivate", false) &&
      shell.value("noRedirectionBitmap", false) && shell.value("clientEqualsWindow", false) &&
      std::abs(page.value("renderMargin", -1.) - renderMargin) < .001;
    if (opt.verifyShell) passed = passed && page.contains("nativeInput") && page["nativeInput"].is_object() &&
      page["nativeInput"].value("mouseDown", 0) >= 2 && page["nativeInput"].value("contextMenu", 0) >= 1 &&
      page["nativeInput"].value("wheel", -1) == 0 && page["nativeInput"].value("trusted", false) &&
      wheelBridgeMessages == 2 && wheelOffsetAfterDown == 1 && page.value("rowOffset", -1) == 0 &&
      dragRequests > 0 && menuRequests > 0 && composition.inputFailures == 0 && shell.value("completeRenderFitsWorkArea", false);
    Json evidence = {{"at", isoNow()}, {"result", passed ? "PASS" : "FAIL"},
      {"mode", "isolated-hidden-HWND-composition-WebView2"}, {"productionConnected", false},
      {"browserVersion", utf8(browserVersion)}, {"ready", ready}, {"fallback", fallback},
      {"delivered", delivered}, {"acknowledgements", acknowledgements},
      {"maximumPendingSnapshots", maximumPending.load()}, {"maximumHttpInFlight", maximumHttp.load()},
      {"clientPixels", {r.right, r.bottom}}, {"dpi", dpi()}, {"zoom", zoom},
      {"maximumZoom", maxZoom}, {"quarterStepPreserved", quarterStep},
      {"stateExplicit", opt.stateExplicit}, {"stateDirectory", utf8(opt.state.wstring())},
      {"profileDirectory", utf8((opt.state / L"WebView2").wstring())},
      {"ownerBound", bool(owner)}, {"ownerExited", ownerExited},
      {"fallbackReason", fallbackReason},
      {"endpointValidation", endpoints}, {"fallbackPaintMessages", fallbackPaintMessages},
      {"windowShell", shell},
      {"page", page}, {"limitations", Json::array({"Hidden isolated window; human drag, cross-monitor movement and per-pixel desktop transparency not accepted by this probe.",
        "Fixture mode does not authorize or prove a production network connection."})}};
    if (!saveJson(opt.verify, evidence)) passed = false;
    exitCode = passed ? 0 : 2;
    PostMessageW(window, WM_CLOSE, 0, 0);
  }
  void finishLiveCheck(const Json& counts, bool passed, const char* reason) {
    if (liveFinished) return;
    liveFinished = true; KillTimer(window, TIMER_VERIFY);
    // Counts/booleans only. No endpoint, token, path, row/model/session text.
    Json evidence = {{"at", isoNow()}, {"result", passed ? "PASS" : "FAIL"},
      {"mode", "hidden-readonly-instance-live-check"}, {"reason", reason},
      {"productionMutation", "NONE"}, {"snapshotPersistence", "NONE"},
      {"httpAttempts", httpAttempts.load()}, {"validSnapshotsReceived", validSnapshotsReceived.load()},
      {"delivered", delivered}, {"acknowledgements", acknowledgements},
      {"maximumPendingSnapshots", maximumPending.load()}, {"maximumHttpInFlight", maximumHttp.load()},
      {"ownerBound", bool(owner)}, {"ownerExited", ownerExited},
      {"webViewReady", ready}, {"nativeFallback", fallback}, {"counts", counts}};
    if (!saveJson(opt.liveCheck, evidence)) passed = false;
    exitCode = passed ? 0 : 2;
    beginClose();
  }
  void ownerTick() {
    if (!owner || ownerExited) return;
    if (WaitForSingleObject(owner.value, 0) == WAIT_TIMEOUT) return;
    ownerExited = true; KillTimer(window, TIMER_OWNER);
    if (!opt.liveCheck.empty() && !liveFinished) finishLiveCheck(Json::object(), false, "owner-exited-before-acceptance");
    else if (!opt.verify.empty() && !verifyFinished) finishVerification({{"error", "Owner exited during isolated verification"}}, false);
    else beginClose();
  }
  void liveTick() {
    if (opt.liveCheck.empty() || liveFinished || closing) return;
    const auto age = std::chrono::duration<double>(std::chrono::steady_clock::now() - launched).count();
    if (age > 25) { finishLiveCheck(Json::object(), false, "no-valid-acknowledged-snapshot-before-deadline"); return; }
    if (!ready || !web || liveInspecting || acknowledgements == 0 || !validSnapshotsReceived.load()) {
      if (ready && web && !liveInspecting) {
        liveInspecting = true;
        web->ExecuteScript(L"(()=>{if(window.RA?.debug){RA.debug.setPlaying(false);RA.debug.advance(.05);}return true;})()",
          Callback<ICoreWebView2ExecuteScriptCompletedHandler>([this](HRESULT, LPCWSTR)->HRESULT { liveInspecting = false; return S_OK; }).Get());
      }
      return;
    }
    liveInspecting = true;
    web->ExecuteScript(L"(()=>{const d=window.RA?.debug,f=d?.feed,s=f?.current;if(!d||!s)return {accepted:false};const rows=Array.isArray(s.rows)?s.rows:[];return {accepted:true,sourceAtValid:Number.isFinite(Date.parse(s.at)),observed:s.mode==='observed',webGlAvailable:!!d.renderer?.gl,rows:rows.length,green:rows.filter(r=>r.color==='green').length,red:rows.filter(r=>r.color==='red').length,yellow:rows.filter(r=>r.color==='yellow').length,outputting:rows.filter(r=>r.outputting).length,sourceReady:s.ready===true,quotaKnown:s.quota?.known===true,quotaFresh:s.quota?.fresh===true,glError:d.renderer?.gl?.getError()??0};})()",
      Callback<ICoreWebView2ExecuteScriptCompletedHandler>([this](HRESULT status, LPCWSTR raw)->HRESULT {
        liveInspecting = false;
        const Json answer = Json::parse(utf8(raw ? raw : L""), nullptr, false);
        // Rebuild the report from a fixed allowlist even though this is a local page.
        Json counts = Json::object();
        if (answer.is_object()) {
          for (const char* key : {"accepted", "sourceAtValid", "observed", "webGlAvailable", "sourceReady", "quotaKnown", "quotaFresh"})
            counts[key] = answer.contains(key) && answer[key].is_boolean() && answer[key].get<bool>();
          for (const char* key : {"rows", "green", "red", "yellow", "outputting", "glError"})
            if (answer.contains(key) && answer[key].is_number_unsigned() && answer[key].get<uint64_t>() <= 65536) counts[key] = answer[key];
        }
        if (SUCCEEDED(status) && counts.value("accepted", false) && counts.value("sourceAtValid", false) &&
            counts.value("glError", -1) == 0 && acknowledgements > 0 && validSnapshotsReceived.load() > 0)
          finishLiveCheck(counts, true, "first-valid-snapshot-acknowledged");
        return S_OK;
      }).Get());
  }
  void verifyTick() {
    if (!opt.liveCheck.empty()) { liveTick(); return; }
    if (verifyFinished || opt.verify.empty()) return;
    const auto age = std::chrono::duration<double>(std::chrono::steady_clock::now() - launched).count();
    if (age > 25) { finishVerification({{"error", "WebView2 fixture readiness timed out"}}, false); return; }
    if (fallback && opt.forceFallback) {
      size_t rows;
      { std::lock_guard<std::mutex> guard(mailbox); rows = display.contains("rows") && display["rows"].is_array() ? display["rows"].size() : 0; }
      if (!rows) return;
      // Exercise the real HWND's paint dispatch after consuming fixture data.
      SendMessageW(window, WM_PAINT, 0, 0);
      Json pixels = fallbackPixels(); pixels["rows"] = rows;
      finishVerification(pixels, fallbackPaintMessages > 0 && pixels.value("nonBackgroundPixels", 0) > 100);
      return;
    }
    if (!ready || !web) return;
    // Hidden verification advances only its own page and never toggles production.
    if (!verifyStarted) {
      verifyStarted = true;
      web->ExecuteScript(L"(()=>{if(window.RA?.debug){RA.debug.setPlaying(false);RA.debug.advance(8);}return true;})()", nullptr);
      return;
    }
    if (acknowledgements == 0) {
      web->ExecuteScript(L"(()=>{if(window.RA?.debug)RA.debug.advance(.25);return true;})()", nullptr);
      return;
    }
    if (opt.verifyReopen) {
      if (reopenProbePhase == 0) {
        { std::lock_guard<std::mutex> guard(mailbox); reopenBaseline = sequence; }
        SendMessageW(window, WM_CLOSE, 0, 0);
        reopenCloseStarted = closing && !stopping.load();
        reopenProbePhase = 1;
        return;
      }
      if (reopenProbePhase == 1) {
        const LRESULT accepted = SendMessageW(window, WM_SHOW_DISPLAY, 0, 0);
        reopenAccepted = accepted == 1 && !closing && !stopping.load();
        reopenProbePhase = 2;
        if (!reopenCloseStarted || !reopenAccepted) {
          finishVerification({{"closeReopenAccepted", false}}, false); return;
        }
        return;
      }
      // Two newly produced snapshots after closing exclude an old queued ACK
      // or an in-flight callback masquerading as a surviving data worker.
      if (ackSequence < reopenBaseline + 2) {
        web->ExecuteScript(L"(()=>{if(window.RA?.debug)RA.debug.advance(.05);return true;})()", nullptr);
        return;
      }
    }
    if (opt.verifyShell) {
      if (shellProbePhase == 0) {
        shellProbePhase = 1;
        web->ExecuteScript(L"(()=>{const s=window.__shellInput={mouseDown:0,contextMenu:0,wheel:0,trusted:true};for(const [type,key] of [['mousedown','mouseDown'],['contextmenu','contextMenu'],['wheel','wheel']])document.addEventListener(type,e=>{s[key]++;s.trusted=s.trusted&&e.isTrusted;},{capture:true,passive:true});return true;})()",
          Callback<ICoreWebView2ExecuteScriptCompletedHandler>([this](HRESULT hr, LPCWSTR)->HRESULT {
            shellProbePhase = SUCCEEDED(hr) ? 2 : -1; return S_OK;
          }).Get());
        return;
      }
      if (shellProbePhase == 1) return;
      if (shellProbePhase == 2) {
        // Send input only to this test's HWND. No desktop pointer is moved.
        const int x = int(std::round((renderMargin + 20) * appliedZoom * dpi() / 96.));
        const int y = x;
        const LPARAM p = MAKELPARAM(x, y);
        SendMessageW(window, WM_MOUSEMOVE, 0, p);
        SendMessageW(window, WM_LBUTTONDOWN, MK_LBUTTON, p);
        SendMessageW(window, WM_LBUTTONUP, 0, p);
        SendMessageW(window, WM_RBUTTONDOWN, MK_RBUTTON, p);
        SendMessageW(window, WM_RBUTTONUP, 0, p);
        shellProbePhase = 3; return;
      }
      if (shellProbePhase == 3) {
        const int x = MulDiv(100, dpi(), 96), y = MulDiv(150, dpi(), 96);
        SendMessageW(window, WM_MOUSEMOVE, 0, MAKELPARAM(x, y));
        shellProbePhase = 4; return;
      }
      if (shellProbePhase == 4) {
        POINT wheel{MulDiv(100, dpi(), 96), MulDiv(150, dpi(), 96)}; ClientToScreen(window, &wheel);
        SendMessageW(window, WM_MOUSEWHEEL, MAKEWPARAM(0, SHORT(-WHEEL_DELTA)), MAKELPARAM(wheel.x, wheel.y));
        shellProbePhase = 5; return;
      }
      if (shellProbePhase == 5) {
        shellProbePhase = 6;
        web->ExecuteScript(L"(()=>{const d=window.RA?.debug;d?.advance(.3);return d?.anim.offset??-1;})()",
          Callback<ICoreWebView2ExecuteScriptCompletedHandler>([this](HRESULT hr, LPCWSTR raw)->HRESULT {
            const Json value = Json::parse(utf8(raw ? raw : L""), nullptr, false);
            if (SUCCEEDED(hr) && value.is_number_integer()) wheelOffsetAfterDown = value.get<int>();
            POINT wheel{MulDiv(100, dpi(), 96), MulDiv(150, dpi(), 96)}; ClientToScreen(window, &wheel);
            SendMessageW(window, WM_MOUSEWHEEL, MAKEWPARAM(0, WHEEL_DELTA), MAKELPARAM(wheel.x, wheel.y));
            shellProbePhase = 7; return S_OK;
          }).Get());
        return;
      }
      if (shellProbePhase == 6) return;
    }
    KillTimer(window, TIMER_VERIFY);
    web->ExecuteScript(L"(()=>{const d=window.RA?.debug;if(!d)return {initialized:false};d.advance(3);return {initialized:true,rows:d.anim.snapshot.rows.length,mode:d.anim.snapshot.mode,feedSequence:d.feed?.sequence,quality:d.opt.quality,zoom:d.opt.zoom,renderer:d.renderer?.gl?'WebGL2':'fallback',glError:d.renderer?.gl?.getError()??0,bodyWidth:document.body.scrollWidth,renderMargin:RA.MARGIN,bodyBackground:getComputedStyle(document.body).backgroundColor,premultipliedAlpha:d.renderer?.gl?.getContextAttributes()?.premultipliedAlpha,nativeInput:window.__shellInput??null,rowOffset:d.anim.offset};})()",
      Callback<ICoreWebView2ExecuteScriptCompletedHandler>([this](HRESULT status, LPCWSTR value)->HRESULT {
        auto page = Json::parse(utf8(value ? value : L""), nullptr, false);
        bool passed = SUCCEEDED(status) && page.is_object() && page.value("initialized", false) &&
          page.value("rows", 0) > 0 && page.value("glError", -1) == 0 && acknowledgements > 0 && maximumPending.load() <= 1;
        if (opt.verifyReopen && page.is_object()) {
          const bool live = reopenCloseStarted && reopenAccepted && !closing && !stopping.load() && ackSequence >= reopenBaseline + 2;
          page["closeReopen"] = {{"closeBeganWithWorkerAlive", reopenCloseStarted}, {"showAccepted", reopenAccepted},
            {"workerContinues", !stopping.load()}, {"newSnapshotsAcknowledged", ackSequence - reopenBaseline}, {"pass", live}};
          passed = passed && live;
        }
        finishVerification(page.is_discarded() ? Json{{"invalidResult", true}} : page, passed);
        return S_OK;
      }).Get());
  }
};

LRESULT CALLBACK windowProcedure(HWND hwnd, UINT msg, WPARAM wp, LPARAM lp) {
  Host* h = (Host*)GetWindowLongPtrW(hwnd, GWLP_USERDATA);
  if (msg == WM_NCCREATE) {
    h = (Host*)((CREATESTRUCTW*)lp)->lpCreateParams; h->window = hwnd;
    SetWindowLongPtrW(hwnd, GWLP_USERDATA, (LONG_PTR)h);
  }
  if (!h) return DefWindowProcW(hwnd, msg, wp, lp);
  if (msg == WM_LBUTTONDOWN && !h->opt.checking() && !h->closing) {
    RECT client{}; GetClientRect(hwnd, &client);
    const int margin = h->fallback ? 0 : int(std::round(h->renderMargin * h->appliedZoom * h->dpi() / 96.));
    const int x = GET_X_LPARAM(lp), y = GET_Y_LPARAM(lp);
    if (x >= margin && y >= margin && x < client.right - margin && y < client.bottom - margin) {
      // Start the native move loop while the actual button is still down.
      // An asynchronous WebView message can arrive after a quick drag ended.
      ++h->dragRequests;
      ReleaseCapture(); SendMessageW(hwnd, WM_NCLBUTTONDOWN, HTCAPTION, 0); return 0;
    }
  }
  if (msg == WM_MOUSEWHEEL && !h->fallback) { h->scrollRows(GET_WHEEL_DELTA_WPARAM(wp)); return 0; }
  if (!h->fallback && h->composition.mouse(msg, wp, lp)) return 0;
  switch (msg) {
  case WM_MOUSEACTIVATE: return h->opt.inspectShell ? MA_ACTIVATE : MA_NOACTIVATE;
  case WM_SETCURSOR:
    if (LOWORD(lp) == HTCLIENT && h->composition.setCursor()) return TRUE;
    break;
  case WM_MOVE: case WM_MOVING:
    if (h->controller) h->controller->NotifyParentWindowPositionChanged();
    break;
  case WM_LBUTTONDOWN:
    if (h->fallback && !h->opt.checking()) { ReleaseCapture(); SendMessageW(hwnd, WM_NCLBUTTONDOWN, HTCAPTION, 0); return 0; }
    break;
  case WM_LATEST: h->drain(); return 0;
  case WM_SHOW_DISPLAY: return h->showWindow() ? 1 : 0;
  case WM_SIZE: {
    const bool restoring = h->minimized && wp != SIZE_MINIMIZED;
    h->minimized = wp == SIZE_MINIMIZED;
    if (h->controller) { RECT r; GetClientRect(hwnd, &r); h->controller->put_Bounds(r); h->composition.resize(r); }
    if (h->controller) h->controller->put_IsVisible(wp != SIZE_MINIMIZED && !h->hiddenByUser);
    if (wp == SIZE_MINIMIZED) h->post({{"kind", "hidden"}});
    else if (restoring && h->ready && !h->closing && !h->hiding && !h->hiddenByUser) h->post({{"kind", "show"}});
    return 0;
  }
  case WM_DPICHANGED:
    if (lp) { const RECT& r = *(const RECT*)lp; SetWindowPos(hwnd, nullptr, r.left, r.top, r.right-r.left, r.bottom-r.top, SWP_NOZORDER | SWP_NOACTIVATE); }
    h->zoom = h->limitZoom(h->zoom); h->sendView(); h->resizeToPanel(); return 0;
  case WM_EXITSIZEMOVE: case WM_DISPLAYCHANGE:
    h->zoom = h->limitZoom(h->zoom); h->sendView(); h->resizeToPanel(); h->saveSettings(); return 0;
  case WM_CONTEXTMENU: h->showMenu(); return 0;
  case WM_PAINT: { PAINTSTRUCT p; HDC dc = BeginPaint(hwnd, &p); if (h->fallback) { h->drawFallback(dc); ++h->fallbackPaintMessages; } EndPaint(hwnd, &p); return 0; }
  case WM_ERASEBKGND: return 1;
  case WM_CLOSE:
    h->saveSettings();
    if (h->verifyFinished) DestroyWindow(hwnd); else h->beginClose();
    return 0;
  case WM_TIMER:
    if (wp == TIMER_CLOSE) { DestroyWindow(hwnd); return 0; }
    if (wp == TIMER_VERIFY) { h->verifyTick(); return 0; }
    if (wp == TIMER_OWNER) { h->ownerTick(); return 0; }
    if (wp == TIMER_HIDE) { h->finishHide(); return 0; }
    break;
  case WM_DESTROY:
    h->stopping = true; SetEvent(h->wake); KillTimer(hwnd, TIMER_CLOSE); KillTimer(hwnd, TIMER_VERIFY); KillTimer(hwnd, TIMER_OWNER); KillTimer(hwnd, TIMER_HIDE);
    PostQuitMessage(h->exitCode); return 0;
  }
  return DefWindowProcW(hwnd, msg, wp, lp);
}
int WINAPI wWinMain(HINSTANCE instance, HINSTANCE, PWSTR, int) {
  using SetDpiFn = BOOL(WINAPI*)(HANDLE);
  if (auto fn = (SetDpiFn)GetProcAddress(GetModuleHandleW(L"user32.dll"), "SetProcessDpiAwarenessContext")) fn((HANDLE)-4);
  CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED);
  int exitCode = 2;
  Options opt;
  try {
    wchar_t exe[32768]; GetModuleFileNameW(nullptr, exe, 32768);
    opt.directory = fs::path(exe).parent_path(); opt.state = opt.directory / L"state";
    opt.assets = opt.directory.parent_path() / L"visual" / L"index.html";
    int argc; LPWSTR* argv = CommandLineToArgvW(GetCommandLineW(), &argc);
    for (int i = 1; i < argc; ++i) {
      const std::wstring a = argv[i];
      if (a == L"--fallback") opt.forceFallback = true;
      else if (a == L"--verify-close-reopen") opt.verifyReopen = true;
      else if (a == L"--verify-shell") opt.verifyShell = true;
      else if (a == L"--preview") opt.preview = true;
      else if (a == L"--inspect-shell") opt.inspectShell = true;
      else if (i + 1 < argc) {
        const std::wstring raw = argv[++i];
        if (a == L"--owner-pid") {
          opt.ownerExplicit = true;
          if (raw.empty() || raw.size() > 10 || raw.find_first_not_of(L"0123456789") != std::wstring::npos)
            throw std::runtime_error("Invalid owner argument");
          const auto pid = std::stoull(raw);
          if (!pid || pid > MAXDWORD) throw std::runtime_error("Invalid owner argument");
          opt.ownerPid = DWORD(pid); continue;
        }
        const fs::path value = fs::absolute(raw);
        if (a == L"--fixture") opt.fixture = value;
        else if (a == L"--instance") opt.instance = value;
        else if (a == L"--state") { opt.state = value; opt.stateExplicit = true; }
        else if (a == L"--verify") opt.verify = value;
        else if (a == L"--live-check") opt.liveCheck = value;
        else throw std::runtime_error("Unknown argument");
      } else throw std::runtime_error("Missing argument value");
    }
    LocalFree(argv);
    if (!opt.verify.empty() && !opt.instance.empty()) throw std::runtime_error("Verification uses isolated fixtures only");
    if (!opt.fixture.empty() && !opt.instance.empty()) throw std::runtime_error("Select one data source");
    if (opt.preview && (!opt.instance.empty() || !opt.fixture.empty() || opt.checking())) throw std::runtime_error("Preview uses isolated synthetic data only");
    if (opt.inspectShell && !opt.preview) throw std::runtime_error("Inspectable shell is restricted to synthetic preview");
    if (opt.verifyShell && (opt.verify.empty() || opt.forceFallback)) throw std::runtime_error("Shell probe requires WebView2 fixture verification");
    if (opt.verifyReopen && (opt.verify.empty() || opt.forceFallback)) throw std::runtime_error("Reopen probe requires WebView2 fixture verification");
    if (!opt.liveCheck.empty() && (!opt.verify.empty() || opt.instance.empty() || !opt.fixture.empty() || opt.forceFallback))
      throw std::runtime_error("Live check requires only an explicit instance");
    UniqueHandle owner;
    if (opt.ownerExplicit) owner = verifiedOwner(opt.ownerPid);
    if (opt.checking() && !opt.stateExplicit) opt.state = opt.directory / L"state" /
      ((opt.liveCheck.empty() ? L"verify-" : L"live-check-") + std::to_wstring(identity((opt.liveCheck.empty() ? opt.verify : opt.liveCheck).wstring())));
    fs::create_directories(opt.state);
    const std::wstring className = L"CodexObservatoryDisplay-" + std::to_wstring(identity(opt.state.wstring())) +
      (opt.ownerExplicit ? L"-" + std::to_wstring(opt.ownerPid) : L"");
    HANDLE lock = CreateMutexW(nullptr, FALSE, (L"Local\\" + className).c_str());
    if (GetLastError() == ERROR_ALREADY_EXISTS) {
      HWND existing = FindWindowW(className.c_str(), nullptr); if (existing) PostMessageW(existing, WM_SHOW_DISPLAY, 0, 0);
      CloseHandle(lock); CoUninitialize(); return 0;
    }
    Host host(opt, std::move(owner));
    host.allowedUri = fileUri(opt.assets);
    host.loadSettings();
    WNDCLASSEXW wc{sizeof(wc)}; wc.lpfnWndProc = windowProcedure; wc.hInstance = instance;
    wc.hCursor = LoadCursorW(nullptr, IDC_ARROW); wc.hIcon = LoadIconW(nullptr, IDI_APPLICATION); wc.lpszClassName = className.c_str();
    RegisterClassExW(&wc);
    const wchar_t* title = !opt.liveCheck.empty() ? L"Codex Observatory 0.4.9 · 只读接口验收"
      : opt.instance.empty() && opt.fixture.empty() && !opt.checking() && !opt.preview
      ? L"Codex Observatory 0.4.9 · 未连接 · 独立显示候选"
      : opt.instance.empty() ? L"Codex Observatory 0.4.9 · 隔离测试 · 独立显示候选"
      : L"Codex Observatory 0.4.9 · 当前状态";
    RECT initialWork{}; SystemParametersInfoW(SPI_GETWORKAREA, 0, &initialWork, 0);
    const DWORD floatingStyles = (opt.inspectShell ? 0 : WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE) | WS_EX_NOREDIRECTIONBITMAP |
      (host.topmost ? WS_EX_TOPMOST : 0);
    HWND hwnd = CreateWindowExW(floatingStyles, className.c_str(), title,
      WS_POPUP, host.savedPosition.right ? host.savedPosition.left : initialWork.left + 20,
      host.savedPosition.right ? host.savedPosition.top : initialWork.top + 20, 500, 320, nullptr, nullptr, instance, &host);
    if (!hwnd) throw std::runtime_error("Window creation failed");
    host.zoom = host.limitZoom(host.zoom); host.resizeToPanel(); host.startWorker();
    if (host.owner) SetTimer(hwnd, TIMER_OWNER, 200, nullptr);
    // Diagnostic modes keep their real HWND hidden and drive only their own page.
    if (!opt.checking()) ShowWindow(hwnd, opt.inspectShell ? SW_SHOWNORMAL : SW_SHOWNOACTIVATE);
    else SetTimer(hwnd, TIMER_VERIFY, 200, nullptr);
    host.initializeWebView();
    MSG msg;
    while (GetMessageW(&msg, nullptr, 0, 0) > 0) { TranslateMessage(&msg); DispatchMessageW(&msg); }
    exitCode = host.exitCode;
    CloseHandle(lock);
  } catch (...) {
    // Deliberately generic: paths and instance content can include credentials.
    if (opt.checking()) saveJson(opt.liveCheck.empty() ? opt.verify : opt.liveCheck, {{"result", "FAIL"}, {"mode", "startup-validation"},
      {"productionConnected", false}, {"error", "Candidate verification rejected invalid startup arguments or unavailable local prerequisites"}});
    else if (!opt.ownerExplicit) MessageBoxW(nullptr, L"显示程序未能启动。请核对本地资源、参数与可写的独立状态目录。", L"Codex Observatory", MB_OK | MB_ICONERROR);
  }
  CoUninitialize(); return exitCode;
}
