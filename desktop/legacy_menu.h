#pragma once
#include <wincrypt.h>
#include <atomic>
#include <cmath>
#include <map>
#include <mutex>
#include <optional>

namespace observatory_menu {
enum Command : UINT {
  Console = 1, Hide = 2, Quit = 3, Auto = 10, Full = 11, Economy = 12,
  Still = 13, Replay = 14, ResetZoom = 15, Enlarge = 16, Shrink = 17,
  Resync = 18, Topmost = 20, Balanced = 21, AutoSync = 22, FirstThread = 100
};
struct View {
  double targetZoom = 1, appliedZoom = 1, maximumZoom = 2;
  int quality = 0;
  bool automatic = true, topmost = true, connected = false, synthetic = false;
  bool syncing = false, commandPending = false, automaticSync = true;
  std::wstring status;
};
inline bool validThread(const std::string& value) {
  if (value.size() != 36) return false;
  for (size_t i = 0; i < value.size(); ++i) {
    const unsigned char c = value[i];
    if (i == 8 || i == 13 || i == 18 || i == 23) { if (c != '-') return false; }
    else if (!((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'))) return false;
  }
  return true;
}
inline std::wstring threadTag(const std::string& text) {
  HCRYPTPROV provider = 0; HCRYPTHASH hash = 0;
  BYTE digest[32]{}; DWORD bytes = sizeof(digest);
  bool ok = CryptAcquireContextW(&provider, nullptr, nullptr, PROV_RSA_AES, CRYPT_VERIFYCONTEXT) &&
    CryptCreateHash(provider, CALG_SHA_256, 0, 0, &hash) &&
    CryptHashData(hash, reinterpret_cast<const BYTE*>(text.data()), DWORD(text.size()), 0) &&
    CryptGetHashParam(hash, HP_HASHVAL, digest, &bytes, 0);
  if (hash) CryptDestroyHash(hash);
  if (provider) CryptReleaseContext(provider, 0);
  if (!ok) return L"------";
  const wchar_t* hex = L"0123456789abcdef";
  std::wstring value;
  for (int i = 0; i < 3; ++i) { value += hex[digest[i] >> 4]; value += hex[digest[i] & 15]; }
  return value;
}
inline std::wstring label(std::wstring text, size_t limit = 80) {
  if (text.size() > limit) { text.resize(limit); if (!text.empty() && text.back() >= 0xd800 && text.back() <= 0xdbff) text.pop_back(); text += L"…"; }
  std::wstring result;
  for (wchar_t c : text) {
    if (c == L'&') result += L"&&";
    else result += c < 32 ? L' ' : c;
  }
  return result;
}
struct Popup {
  HMENU handle = CreatePopupMenu();
  // This mapping is a snapshot taken before TrackPopupMenu enters its nested loop.
  // Subsequent row reordering cannot redirect an already displayed command.
  std::map<UINT, std::string> threads;
  ~Popup() { if (handle) DestroyMenu(handle); }
  Popup() = default;
  Popup(const Popup&) = delete;
  void item(UINT id, const std::wstring& text, bool enabled = true, bool checked = false) {
    AppendMenuW(handle, MF_STRING | (enabled ? 0 : MF_GRAYED) | (checked ? MF_CHECKED : 0), id, text.c_str());
  }
  void separator() { AppendMenuW(handle, MF_SEPARATOR, 0, nullptr); }
  void build(const Json& snapshot, const View& v) {
    if (snapshot.contains("rows") && snapshot["rows"].is_array()) {
      UINT next = FirstThread;
      for (const auto& row : snapshot["rows"]) {
        if (next >= FirstThread + 256) break;
        if (!row.is_object()) continue;
        if (row.contains("activity") && row["activity"].is_string() && row["activity"] == "completed") continue;
        const auto id = row.contains("thread_id") && row["thread_id"].is_string() ? row["thread_id"].get<std::string>() : "";
        const auto model = row.contains("model") && row["model"].is_string() ? row["model"].get<std::string>() : "—";
        const bool usable = v.connected && !v.synthetic && validThread(id);
        item(next, label(wide(model)) + L" · " + threadTag(id), usable);
        if (usable) threads[next] = id;
        ++next;
      }
      if (next != FirstThread) separator();
    }
    std::wstring size = L"窗口大小：" + std::to_wstring(int(std::round(v.targetZoom * 100))) + L"%";
    if (std::abs(v.targetZoom - v.appliedZoom) > .001) size += L"（稍后应用）";
    item(0, size, false);
    item(Enlarge, L"放大 25%", v.targetZoom < v.maximumZoom - .001);
    item(Shrink, L"缩小 25%", v.targetZoom > .7501);
    item(ResetZoom, L"恢复 100%", std::abs(v.targetZoom - 1) > .001);
    separator();
    item(Resync, v.syncing ? L"正在重新同步路由…" : L"重新同步路由",
      v.connected && !v.synthetic && !v.syncing && !v.commandPending);
    item(AutoSync, L"每 5 秒同步当前路由", v.connected && !v.synthetic, v.automaticSync);
    if (!v.status.empty()) item(0, label(v.status, 120), false);
    separator();
    item(Console, L"打开总控制台", v.connected && !v.synthetic);
    item(Replay, L"重播启动动画");
    item(Auto, L"光效：自动调节", true, v.automatic);
    item(Full, L"光效：完整", true, !v.automatic && v.quality == 0);
    item(Balanced, L"光效：均衡", true, !v.automatic && v.quality == 1);
    item(Economy, L"光效：节能", true, !v.automatic && v.quality == 2);
    item(Still, L"光效：减少动态", true, !v.automatic && v.quality == 3);
    item(Topmost, L"窗口置顶", true, v.topmost);
    separator();
    item(Hide, L"柔和隐藏（继续采集）");
    item(Quit, v.connected ? L"退出监视器" : L"关闭预览窗口", !v.commandPending);
  }
};

enum class Control : unsigned { None, Refresh, Resync, Shutdown, Pulse };
// The backend emits UTC RFC3339Nano. Keep the result's original five-minute
// lifetime instead of giving an old result five new minutes on receipt.
inline std::optional<ULONGLONG> statusAge(const std::string& at) {
  if (at.size() < 20 || at.size() > 30 || at.back() != 'Z' ||
      at[4] != '-' || at[7] != '-' || at[10] != 'T' || at[13] != ':' || at[16] != ':') return {};
  for (size_t i = 0; i < 19; ++i) {
    if (i == 4 || i == 7 || i == 10 || i == 13 || i == 16) continue;
    if (at[i] < '0' || at[i] > '9') return {};
  }
  unsigned millis = 0;
  if (at.size() != 20) {
    if (at[19] != '.' || at.size() < 22) return {};
    for (size_t i = 20; i + 1 < at.size(); ++i) {
      if (at[i] < '0' || at[i] > '9') return {};
      if (i < 23) millis += unsigned(at[i] - '0') * (i == 20 ? 100 : i == 21 ? 10 : 1);
    }
  }
  const auto number = [&](size_t offset, size_t count) {
    unsigned value = 0;
    for (size_t i = offset; i < offset + count; ++i) value = value * 10 + unsigned(at[i] - '0');
    return WORD(value);
  };
  SYSTEMTIME time{};
  time.wYear=number(0,4); time.wMonth=number(5,2); time.wDay=number(8,2);
  time.wHour=number(11,2); time.wMinute=number(14,2); time.wSecond=number(17,2); time.wMilliseconds=WORD(millis);
  FILETIME source{}, currentTime{};
  if (!SystemTimeToFileTime(&time,&source)) return {};
  GetSystemTimeAsFileTime(&currentTime);
  ULARGE_INTEGER a{},b{};
  a.LowPart=source.dwLowDateTime; a.HighPart=source.dwHighDateTime;
  b.LowPart=currentTime.dwLowDateTime; b.HighPart=currentTime.dwHighDateTime;
  return b.QuadPart > a.QuadPart ? (b.QuadPart-a.QuadPart)/10000 : 0;
}
// Pumped by the same native worker as the current snapshot GET. No extra HTTP thread.
class Controls {
  std::atomic<unsigned> requested{0};
  std::atomic<unsigned> busy{0};
  std::mutex lock;
  Json current = Json::object();
  std::wstring message;
  bool syncing = false;
  ULONGLONG statusUntil = 0, lastRefresh = 0, pollUntil = 0, nextPulse = 0;
public:
  std::atomic<bool> automatic{true};
  std::atomic<bool> shutdownAccepted{false};
  std::atomic<unsigned> pulseAttempts{0};
  bool queue(Control command) {
    unsigned pending = requested.load();
    const auto active = Control(busy.load());
    if (active == Control::Resync || active == Control::Shutdown) return false;
    if (command == Control::Refresh) {
      if (busy.load() || pending) return false;
      return requested.compare_exchange_strong(pending, unsigned(command));
    }
    if (pending != 0 && pending != unsigned(Control::Refresh)) return false;
    return requested.compare_exchange_strong(pending, unsigned(command));
  }
  void view(View& v) {
    std::lock_guard<std::mutex> guard(lock);
    const auto active = Control(busy.load()), pending = Control(requested.load());
    v.commandPending = active == Control::Resync || active == Control::Shutdown ||
      pending == Control::Resync || pending == Control::Shutdown;
    v.syncing = syncing;
    v.automaticSync = automatic.load();
    v.status = statusUntil && GetTickCount64() <= statusUntil ? message : L"";
  }
  void reset() {
    std::lock_guard<std::mutex> guard(lock);
    current = Json::object(); message.clear(); syncing = false; statusUntil = pollUntil = nextPulse = 0;
    requested = 0; busy = 0;
    shutdownAccepted = false;
  }
  void receive(const Json& response) {
    if (!response.contains("route_sync") || !response["route_sync"].is_object()) return;
    const auto& status = response["route_sync"];
    const std::string state = status.contains("state") && status["state"].is_string() ? status["state"].get<std::string>() : "";
    const std::string text = status.contains("message") && status["message"].is_string() ? status["message"].get<std::string>() : "";
    std::lock_guard<std::mutex> guard(lock);
    const bool changed = current != status;
    current = status; syncing = state == "syncing";
    if (changed) {
      message.clear(); statusUntil = 0;
      const auto age = status.contains("at") && status["at"].is_string() ?
        statusAge(status["at"].get<std::string>()) : std::optional<ULONGLONG>{};
      if (state != "idle" && age && *age <= 300000) {
        message = wide(text.substr(0, 2048));
        statusUntil = GetTickCount64() + (300000 - *age);
      }
    }
    if (syncing && !pollUntil) pollUntil = GetTickCount64() + 30000;
    if (!syncing) pollUntil = 0;
  }
  bool pump(const Endpoint& endpoint, const std::atomic<bool>* stopping, bool allowAutomatic = false, ULONGLONG now = GetTickCount64()) {
    auto command = Control(requested.exchange(0));
    bool polling = false, syncInProgress = false;
    {
      std::lock_guard<std::mutex> guard(lock);
      if (syncing && pollUntil && now >= pollUntil) {
        // An unconfirmed readback is no evidence that the backend stays busy.
        // Release the UI gate after the bounded readback window; the backend
        // still owns duplicate suppression if a user explicitly retries.
        syncing = false; pollUntil = 0;
        message = L"同步状态未确认，可重新同步"; statusUntil = now + 300000;
      }
      syncInProgress=syncing;
      polling = syncing && now < pollUntil && now - lastRefresh >= 1000;
    }
    if (allowAutomatic && automatic.load()) {
      if (!nextPulse) nextPulse = now + 5000;
      if (now >= nextPulse) {
        nextPulse = now + 5000; // Skip missed intervals; never build a catch-up queue.
        if (command == Control::None && !syncInProgress) command = Control::Pulse;
      }
    } else nextPulse = 0;
    if (command == Control::None && !polling) return false;
    busy = unsigned(command == Control::None ? Control::Refresh : command);
    const wchar_t* route = command == Control::Resync ? L"/api/routing/resync" :
      command == Control::Shutdown ? L"/api/shutdown" : command == Control::Pulse ? L"/api/routing/pulse" : L"/api/display-controls";
    const bool post = command == Control::Resync || command == Control::Shutdown || command == Control::Pulse;
    if (command == Control::Pulse) ++pulseAttempts;
    const auto response = requestJson(endpoint, route, stopping, post ? L"POST" : L"GET");
    if (response) {
      receive(*response);
      if (command == Control::Shutdown && response->contains("ok") &&
          (*response)["ok"].is_boolean() && (*response)["ok"].get<bool>()) shutdownAccepted = true;
    }
    else if (post && command != Control::Pulse) {
      std::lock_guard<std::mutex> guard(lock);
      message = L"操作未确认，请稍后查看状态"; statusUntil = now + 300000;
    }
    lastRefresh = now; busy = 0;
    return true;
  }
};
}
