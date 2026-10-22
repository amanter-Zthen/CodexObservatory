// Isolated contract harness: no WinHTTP, no backend, no HWND and no shown menus.
#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include <algorithm>
#include <atomic>
#include <chrono>
#include <cmath>
#include <cstdio>
#include <filesystem>
#include <fstream>
#include <functional>
#include <iostream>
#include <mutex>
#include <optional>
#include <regex>
#include <stdexcept>
#include <string>
#include <vector>
#include "../vendor/json.hpp"

using Json = nlohmann::json;
namespace fixture {
ULONGLONG tick = 1000000, wallBase = 0;
size_t requestCount = 0, menuCount = 0;
std::vector<Json> calls;
std::function<std::optional<Json>(const std::wstring&, const std::wstring&)> responder;
ULONGLONG WINAPI now() { return tick; }
void WINAPI wall(FILETIME* output) {
  ULARGE_INTEGER value{};
  value.QuadPart = wallBase + tick * 10000;
  output->dwLowDateTime = value.LowPart;
  output->dwHighDateTime = value.HighPart;
}
HMENU WINAPI menu() { ++menuCount; return ::CreatePopupMenu(); }
void reset() {
  tick = 1000000;
  SYSTEMTIME time{}; time.wYear = 2026; time.wMonth = 10; time.wDay = 21;
  FILETIME result{};
  if (!SystemTimeToFileTime(&time, &result)) throw std::runtime_error("fixed wall clock");
  ULARGE_INTEGER value{}; value.LowPart=result.dwLowDateTime; value.HighPart=result.dwHighDateTime;
  wallBase = value.QuadPart;
  calls.clear();
  responder = {};
}
std::string at(long long deltaMillis = 0) {
  ULARGE_INTEGER value{}; value.QuadPart = wallBase + (tick + deltaMillis) * 10000;
  FILETIME ft{value.LowPart,value.HighPart}; SYSTEMTIME st{};
  if (!FileTimeToSystemTime(&ft, &st)) throw std::runtime_error("fixture date");
  char text[40]{};
  sprintf_s(text, "%04u-%02u-%02uT%02u:%02u:%02u.%03uZ", st.wYear,st.wMonth,st.wDay,
    st.wHour,st.wMinute,st.wSecond,st.wMilliseconds);
  return text;
}
Json status(const std::string& state, const std::string& message, long long age = 0) {
  return {{"route_sync",{{"state",state},{"message",message},{"at",at(-age)}}}};
}
void expect(bool value, const char* text) {
  if (!value) throw std::runtime_error(text);
}
}
std::wstring wide(const std::string& value) {
  if (value.empty()) return {};
  int count=MultiByteToWideChar(CP_UTF8,MB_ERR_INVALID_CHARS,value.data(),int(value.size()),nullptr,0);
  if (!count) return {};
  std::wstring output(count,L'\0');
  MultiByteToWideChar(CP_UTF8,MB_ERR_INVALID_CHARS,value.data(),int(value.size()),output.data(),count);
  return output;
}
std::string utf8(const std::wstring& value) {
  if (value.empty()) return {};
  int count=WideCharToMultiByte(CP_UTF8,0,value.data(),int(value.size()),nullptr,0,nullptr,nullptr);
  std::string output(count,'\0');
  WideCharToMultiByte(CP_UTF8,0,value.data(),int(value.size()),output.data(),count,nullptr,nullptr);
  return output;
}
struct Endpoint { unsigned short port=0; std::string token; };
std::optional<Json> requestJson(const Endpoint&, const wchar_t* route, const std::atomic<bool>* stopping,
                               const wchar_t* method=L"GET") {
  if (stopping && stopping->load()) return {};
  ++fixture::requestCount;
  fixture::calls.push_back({{"method",utf8(method)},{"route",utf8(route)},{"at",fixture::tick}});
  if (fixture::responder) return fixture::responder(route,method);
  return Json{{"accepted",true}};
}
// Clock substitution applies only to the contract header, never Windows declarations.
#define GetTickCount64 fixture::now
#define GetSystemTimeAsFileTime fixture::wall
#define CreatePopupMenu fixture::menu
#include "../legacy_menu.h"
#undef CreatePopupMenu
#undef GetSystemTimeAsFileTime
#undef GetTickCount64

namespace fixture {
using namespace observatory_menu;
const std::string first="11111111-2222-3333-4444-555555555555";
const std::string second="aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee";
Endpoint endpoint{0,"isolated-no-http"};
std::atomic<bool> stopping{false};
Json row(const std::string& id, const std::string& model="model", const std::string& activity="running") {
  return {{"thread_id",id},{"model",model},{"activity",activity}};
}
Json rows(std::initializer_list<Json> values) { return {{"rows",Json(values)}}; }
View live() { View v; v.connected=true; return v; }
std::wstring menuText(HMENU menu, UINT command) {
  wchar_t text[1024]{};
  if (!GetMenuStringW(menu,command,text,1024,MF_BYCOMMAND)) throw std::runtime_error("missing menu command");
  return text;
}
bool enabled(HMENU menu, UINT command) {
  UINT flags=GetMenuState(menu,command,MF_BYCOMMAND);
  expect(flags!=UINT(-1),"missing menu flags");
  return !(flags & (MF_DISABLED|MF_GRAYED));
}
bool checked(HMENU menu, UINT command) {
  UINT flags=GetMenuState(menu,command,MF_BYCOMMAND);
  expect(flags!=UINT(-1),"missing menu flags");
  return (flags & MF_CHECKED)!=0;
}
size_t routeCalls(const char* route) {
  return std::count_if(calls.begin(),calls.end(),[&](const Json& call){return call["route"]==route;});
}
bool pump(Controls& controls, ULONGLONG now, bool automatic=true) {
  tick=now;
  return controls.pump(endpoint,&stopping,automatic,now);
}
View view(Controls& controls) { View v=live(); controls.view(v); return v; }
}

int wmain(int argc, wchar_t** argv) {
  using namespace fixture;
  using namespace observatory_menu;
  Json results=Json::array();
  const auto began=std::chrono::steady_clock::now();
  auto test=[&](const char* name, const std::function<void()>& run) {
    reset(); stopping=false;
    try { run(); results.push_back({{"case",name},{"pass",true}}); }
    catch (const std::exception& error) { results.push_back({{"case",name},{"pass",false},{"error",error.what()}}); }
  };
  test("uuid-exact-shape-and-case",[]{
    expect(validThread(first)&&validThread(second)&&validThread("ABCDEF01-2345-6789-ABCD-EF0123456789"),"valid UUID");
    for (const std::string value : {"","fixture-1","111111112222-3333-4444-555555555555",
        "11111111-2222-3333-4444-55555555555g","11111111-2222-3333-4444-555555555555/other",
        "11111111-2222-3333-4444-555555555555\n"})
      expect(!validThread(value),"invalid UUID accepted");
  });
  test("sha256-six-lowercase-hex",[]{
    expect(threadTag("")==L"e3b0c4" && threadTag("hello")==L"2cf24d","SHA256 known vectors");
    expect(threadTag(first).size()==6 && threadTag(first)!=wide(first.substr(0,6)),"tag is a SHA digest, not UUID prefix");
  });
  test("completed-excluded-invalid-and-synthetic-disabled",[]{
    Popup p; p.build(rows({row(first),row(second,"finished","completed"),row("bad","invalid")}),live());
    expect(p.threads.size()==1&&p.threads.at(FirstThread)==first,"only current valid mapping");
    expect(!enabled(p.handle,FirstThread+1),"invalid UUID disabled");
    auto synthetic=live(); synthetic.synthetic=true;
    Popup s; s.build(rows({row(first)}),synthetic);
    expect(s.threads.empty()&&!enabled(s.handle,FirstThread)&&!enabled(s.handle,Resync)&&!enabled(s.handle,Console),"synthetic commands disabled");
    auto offline=live(); offline.connected=false;
    Popup o; o.build(rows({row(first)}),offline);
    expect(o.threads.empty()&&!enabled(o.handle,AutoSync),"disconnected commands disabled");
  });
  test("malformed-row-does-not-hide-later-valid-thread",[]{
    Popup p; p.build(rows({nullptr,row(first),42,row(second)}),live());
    expect(p.threads.size()==2,"malformed row aborted all later rows");
  });
  test("popup-command-map-is-owned-snapshot",[]{
    auto source=rows({row(first,"one"),row(second,"two")});
    Popup p; p.build(source,live());
    source["rows"]=Json::array({row(second,"new two"),row(first,"new one")});
    Popup later; later.build(source,live());
    expect(p.threads.at(FirstThread)==first&&later.threads.at(FirstThread)==second,"old command redirected");
    expect(menuText(p.handle,FirstThread)==L"one · "+threadTag(first),"old label changed");
  });
  test("menu-capacity-and-label-controls",[]{
    Json source={{"rows",Json::array()}};
    for(int i=0;i<400;++i) source["rows"].push_back(row(first));
    Popup p; p.build(source,live());
    expect(p.threads.size()==256,"thread menu is not capped at 256");
    expect(label(L"A&B\r\nC\tD")==L"A&&B  C D","menu label escaping");
    std::wstring text(79,L'x'); text+=wchar_t(0xd83d);text+=wchar_t(0xde00);text+=L'z';
    const auto bounded=label(text);
    expect(bounded==std::wstring(79,L'x')+L"…","UTF16 truncation split a surrogate");
  });
  test("zoom-min-max-dpi-and-pending-label",[]{
    auto v=live(); v.targetZoom=.75;v.appliedZoom=.75;
    Popup low;low.build(Json::object(),v);
    expect(!enabled(low.handle,Shrink)&&enabled(low.handle,Enlarge)&&enabled(low.handle,ResetZoom),"75% boundary");
    v.targetZoom=2;v.appliedZoom=2;
    Popup high;high.build(Json::object(),v);
    expect(!enabled(high.handle,Enlarge)&&enabled(high.handle,Shrink),"200% boundary");
    v.maximumZoom=1.25;v.targetZoom=1.25;v.appliedZoom=1;
    Popup dpi;dpi.build(Json::object(),v);
    expect(!enabled(dpi.handle,Enlarge)&&menuText(dpi.handle,0)==L"窗口大小：125%（稍后应用）","high DPI/pending hint");
    v.targetZoom=v.appliedZoom=1;
    Popup normal;normal.build(Json::object(),v);
    expect(!enabled(normal.handle,ResetZoom),"100% reset boundary");
  });
  test("legacy-actions-and-manual-quality-selection",[]{
    auto v=live();v.automatic=false;v.quality=2;
    Popup p;p.build(Json::object(),v);
    expect(menuText(p.handle,Console)==L"打开总控制台"&&menuText(p.handle,Hide)==L"柔和隐藏（继续采集）"&&
      menuText(p.handle,Replay)==L"重播启动动画"&&menuText(p.handle,Quit)==L"退出监视器","legacy action labels");
    expect(checked(p.handle,Economy)&&!checked(p.handle,Auto)&&!checked(p.handle,Full),"manual selection");
    v.automatic=true;
    Popup a;a.build(Json::object(),v);
    expect(checked(a.handle,Auto)&&!checked(a.handle,Economy),"automatic quality selection");
  });
  test("status-ttl-is-source-relative",[]{
    Controls c;c.receive(status("synced","recent result",299000));
    expect(view(c).status==L"recent result","fresh source status absent");
    tick+=1001;
    expect(view(c).status.empty(),"old source status gained a fresh five-minute lifetime");
  });
  test("same-status-refresh-does-not-renew-ttl",[]{
    Controls c;auto s=status("synced","one result");
    c.receive(s);tick+=299000;c.receive(s);
    expect(!view(c).status.empty(),"recent result lost");
    tick+=1001;c.receive(s);
    expect(view(c).status.empty(),"unchanged response renewed result");
  });
  test("idle-malformed-and-expired-status-hidden",[]{
    for(const auto& s : std::vector<Json>{
      status("idle","should be empty"),status("synced","expired",300001),
      {{"route_sync",{{"state","synced"},{"message","bad date"},{"at","2026-10-21T00:16:40garbageZ"}}}},
      {{"route_sync",{{"state","synced"},{"message","missing date"}}}}}) {
      Controls c;c.receive(s);expect(view(c).status.empty(),"idle/invalid/stale status shown");
    }
  });
  test("single-slot-refresh-coalesces-and-manual-replaces-refresh",[]{
    Controls c;expect(c.queue(Control::Refresh),"initial refresh");
    for(int i=0;i<10000;++i)expect(!c.queue(Control::Refresh),"refresh queue grew");
    expect(c.queue(Control::Resync),"manual command must replace pending refresh");
    for(int i=0;i<10000;++i)expect(!c.queue(Control::Resync),"manual queue grew");
    expect(view(c).commandPending,"pending command not reflected");
    pump(c,tick,false);
    expect(calls.size()==1&&calls[0]["route"]=="/api/routing/resync"&&calls[0]["method"]=="POST","wrong manual route");
    expect(!view(c).commandPending&&!pump(c,tick+1,false),"manual was replayed");
  });
  test("pulse-five-second-cadence-and-no-catchup",[]{
    Controls c;const auto start=tick;
    expect(!pump(c,start)&&!pump(c,start+4999),"pulse early");
    expect(pump(c,start+5000)&&c.pulseAttempts==1,"first pulse missing");
    expect(!pump(c,start+5000),"same tick duplicated pulse");
    expect(pump(c,start+60000)&&c.pulseAttempts==2,"suspend must produce only one current pulse");
    for(int i=0;i<10000;++i)expect(!pump(c,start+60000),"catchup queued");
    expect(!pump(c,start+64999)&&pump(c,start+65000),"new interval not reset from resume");
    for(const auto& call:calls)expect(call["route"]=="/api/routing/pulse"&&call["method"]=="POST","automatic pulse used manual resync");
  });
  test("automatic-disabled-and-reenable-starts-fresh-interval",[]{
    Controls c;const auto start=tick;
    pump(c,start);c.automatic=false;
    expect(!pump(c,start+5000)&&!pump(c,start+60000),"disabled pulse ran");
    c.automatic=true;
    expect(!pump(c,start+70000)&&!pump(c,start+74999)&&pump(c,start+75000),"enable inherited old deadline");
    expect(!pump(c,start+100000,false),"disconnected/preview allowed automatic");
    expect(!pump(c,start+110000,true)&&pump(c,start+115000,true),"allowAutomatic restore inherited deadline");
  });
  test("pulse-failure-drops-attempt-without-backlog",[]{
    Controls c;responder=[](const std::wstring&,const std::wstring&)->std::optional<Json>{return {};};
    const auto start=tick;pump(c,start);expect(pump(c,start+5000),"failed attempt absent");
    expect(!view(c).commandPending&&view(c).status.empty(),"pulse failure changed manual status");
    for(int i=1;i<5000;++i)expect(!pump(c,start+5000+i),"failure queued retry before cadence");
    responder={};expect(pump(c,start+10000)&&c.pulseAttempts==2,"failure permanently disabled pulse");
  });
  test("manual-command-wins-at-pulse-deadline",[]{
    Controls c;const auto start=tick;pump(c,start);
    expect(c.queue(Control::Resync),"manual queue");
    expect(pump(c,start+5000)&&c.pulseAttempts==0,"pulse ran before manual");
    expect(!pump(c,start+5001),"missed pulse was replayed after manual");
    expect(pump(c,start+10000)&&c.pulseAttempts==1,"future pulse did not recover");
  });
  test("manual-accepted-during-pulse-then-next-pump-only",[]{
    Controls c;const auto start=tick;pump(c,start);
    responder=[&](const std::wstring& route,const std::wstring&)->std::optional<Json>{
      if(route==L"/api/routing/pulse") {
        expect(c.queue(Control::Resync),"manual rejected during automatic request");
        expect(!c.queue(Control::Resync),"second pending manual accepted");
        expect(view(c).commandPending,"queued manual is not visible");
      }
      return Json{{"accepted",true}};
    };
    pump(c,start+5000);
    expect(calls.size()==1,"nested request or more than one request in pump");
    pump(c,start+5001);
    expect(calls.size()==2&&calls[1]["route"]=="/api/routing/resync","manual not served next");
  });
  test("busy-manual-rejects-duplicates-and-pulse",[]{
    Controls c;c.queue(Control::Resync);
    responder=[&](const std::wstring&,const std::wstring&)->std::optional<Json>{
      expect(!c.queue(Control::Refresh)&&!c.queue(Control::Resync)&&!c.queue(Control::Shutdown),"busy manual accepted extra operation");
      return status("syncing","working");
    };
    const auto start=tick;pump(c,start);
    responder=[](const std::wstring&,const std::wstring&)->std::optional<Json>{return status("syncing","working");};
    pump(c,start+5000);
    expect(routeCalls("/api/routing/pulse")==0&&calls.back()["method"]=="GET","busy status allowed pulse or polling POST");
  });
  test("readback-failure-releases-stuck-sync-after-bounded-window",[]{
    Controls c;c.receive(status("syncing","working"));
    responder=[](const std::wstring&,const std::wstring&)->std::optional<Json>{return {};};
    const auto start=tick;pump(c,start+1000);
    expect(view(c).syncing,"syncing cleared prematurely");
    pump(c,start+30001);
    expect(!view(c).syncing,"failed readback permanently locked syncing");
    Popup p;p.build(Json::object(),view(c));
    expect(enabled(p.handle,Resync),"manual resync remains disabled after unknown outcome");
    expect(c.queue(Control::Resync),"manual retry could not queue");
  });
  test("manual-failure-has-status-and-no-automatic-resync-retry",[]{
    Controls c;c.queue(Control::Resync);
    responder=[](const std::wstring&,const std::wstring&)->std::optional<Json>{return {};};
    const auto start=tick;pump(c,start,false);
    expect(!view(c).commandPending&&view(c).status==L"操作未确认，请稍后查看状态","failure status missing");
    expect(!pump(c,start+1,false)&&routeCalls("/api/routing/resync")==1,"manual failure retried");
  });
  test("reset-clears-mapping-state-and-stale-deadline",[]{
    Controls c;c.receive(status("syncing","old"));c.queue(Control::Resync);c.reset();
    expect(!view(c).syncing&&!view(c).commandPending&&view(c).status.empty(),"reset retained session state");
    expect(!pump(c,tick)&&calls.empty(),"reset replayed a pending operation");
    expect(pump(c,tick+5000),"reset suppressed future cadence");
  });
  test("shutdown-is-explicit-post-and-not-a-pulse",[]{
    Controls c;expect(c.queue(Control::Shutdown),"shutdown queue");
    expect(view(c).commandPending,"shutdown pending missing");pump(c,tick,false);
    expect(calls.size()==1&&calls[0]["method"]=="POST"&&calls[0]["route"]=="/api/shutdown","shutdown contract");
  });
  test("shutdown-accepted-requires-own-explicit-boolean-ack",[]{
    for(const auto& response:std::vector<Json>{{{"ok",false}},{{"ok",1}},{{"ok","true"}},{{"accepted",true}},Json::object()}) {
      Controls c;c.queue(Control::Shutdown);
      responder=[response](const std::wstring&,const std::wstring&)->std::optional<Json>{return response;};
      pump(c,tick,false);expect(!c.shutdownAccepted,"non-acknowledgement closed the display");
    }
    Controls c;
    responder=[](const std::wstring&,const std::wstring&)->std::optional<Json>{return Json{{"ok",true}};};
    c.queue(Control::Refresh);pump(c,tick,false);
    expect(!c.shutdownAccepted,"unrelated ok response acknowledged shutdown");
    c.queue(Control::Shutdown);pump(c,tick,false);
    expect(c.shutdownAccepted,"successful shutdown acknowledgement absent");
    c.reset();expect(!c.shutdownAccepted,"reset retained old shutdown acknowledgement");
  });
  test("stop-token-prevents-http-and-busy-state-released",[]{
    Controls c;c.queue(Control::Resync);stopping=true;pump(c,tick,false);
    expect(calls.empty()&&!view(c).commandPending,"stop token reached network or retained busy state");
  });
  size_t passed=0;for(const auto& r:results) if(r["pass"]==true)++passed;
  Json report={{"result",passed==results.size()?"PASS":"FAIL"},{"passed",passed},{"total",results.size()},
    {"elapsedMs",std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now()-began).count()},
    {"isolation",{{"httpImplementation","in-memory stub; no network linked"},{"actualHttpRequests",0},
      {"createdMenus",menuCount},{"shownMenus",0},{"createdWindows",0},{"productionTouched",false},
      {"clock","fixed 2026-10-21 UTC and simulated monotonic ticks"}}},{"stubRequests",requestCount},{"cases",results}};
  if(argc!=2){std::cerr<<"Usage: menu-contract.exe <report.json>\n";return 2;}
  const std::filesystem::path output(argv[1]);
  std::filesystem::create_directories(output.parent_path());
  std::ofstream stream(output,std::ios::binary|std::ios::trunc);stream<<report.dump(2)<<"\n";stream.close();
  std::cout<<report["result"].get<std::string>()<<" "<<passed<<"/"<<results.size()<<" contract cases\n";
  for(const auto& r:results)if(r["pass"]==false)std::cout<<r["case"].get<std::string>()<<": "<<r["error"].get<std::string>()<<"\n";
  return passed==results.size()?0:1;
}
