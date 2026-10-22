// Separate-process underlay for real Computer Use mouse delivery checks.
// This program records only messages actually delivered by USER32. It does not
// inject input, forward mouse messages, read an instance, or open a network client.
#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include <windowsx.h>
#include <shellapi.h>
#include <algorithm>
#include <filesystem>
#include <fstream>
#include <string>
#include "../vendor/json.hpp"
using Json = nlohmann::json;
namespace fs = std::filesystem;

struct Harness {
  HWND window = nullptr, visual = nullptr, input = nullptr;
  PROCESS_INFORMATION child{};
  fs::path host, folder;
  unsigned leftDown = 0, leftUp = 0, rightUp = 0, wheel = 0;
  Json events = Json::array();
  bool childLaunched = false;
  ULONGLONG began = GetTickCount64();

  bool write(const fs::path& file, const Json& value) {
    fs::create_directories(file.parent_path()); auto temporary = file; temporary += L".tmp";
    { std::ofstream out(temporary, std::ios::binary | std::ios::trunc); out << value.dump(2); if (!out.good()) return false; }
    return MoveFileExW(temporary.c_str(), file.c_str(), MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH) != FALSE;
  }
  static BOOL CALLBACK findHost(HWND hwnd, LPARAM context) {
    auto* self = reinterpret_cast<Harness*>(context); DWORD pid = 0; GetWindowThreadProcessId(hwnd, &pid);
    if (pid != self->child.dwProcessId) return TRUE;
    wchar_t name[256]{}; GetClassNameW(hwnd, name, 256);
    if (wcsncmp(name, L"CodexObservatoryDisplay-", 24) == 0) self->visual = hwnd;
    else if (wcscmp(name, L"CodexObservatoryPanelInput-v1") == 0) self->input = hwnd;
    return TRUE;
  }
  void discover() { visual = input = nullptr; if (child.dwProcessId) EnumWindows(findHost, reinterpret_cast<LPARAM>(this)); }
  void launch() {
    if (childLaunched) return; childLaunched = true;
    const fs::path state = folder / L"host-state";
    POINT origin{30, 95}; ClientToScreen(window, &origin);
    write(state / L"display-settings.json", {{"zoom", 1.}, {"quality", 0}, {"topmost", true},
      {"automaticQuality", true}, {"automaticRouteSync", false}, {"x", origin.x}, {"y", origin.y}, {"positionSaved", true}});
    std::wstring command = L"\"" + host.wstring() + L"\" --preview --inspect-shell --state \"" + state.wstring() +
      L"\" --owner-pid " + std::to_wstring(GetCurrentProcessId());
    STARTUPINFOW startup{sizeof(startup)}; startup.dwFlags = STARTF_USESHOWWINDOW; startup.wShowWindow = SW_HIDE;
    if (!CreateProcessW(host.c_str(), command.data(), nullptr, nullptr, FALSE, 0, nullptr, host.parent_path().c_str(), &startup, &child))
      write(folder / L"startup-error.json", {{"error", "Could not create the isolated host child"}, {"win32", GetLastError()}});
    save();
  }
  Json rectangle(HWND hwnd) {
    RECT r{}; if (hwnd) GetWindowRect(hwnd, &r);
    return {r.left, r.top, r.right, r.bottom};
  }
  void save() {
    discover();
    Json hits = Json::array();
    if (visual && input && IsWindowVisible(input)) {
      RECT v{}, in{}; GetWindowRect(visual, &v); GetWindowRect(input, &in);
      const POINT points[] = {{(v.left+in.left)/2, (in.top+in.bottom)/2},
        {(v.right+in.right)/2, (in.top+in.bottom)/2}, {(in.left+in.right)/2, (v.top+in.top)/2},
        {(in.left+in.right)/2, (v.bottom+in.bottom)/2}, {in.left+2,in.top+2},
        {in.right-3,in.top+2}, {in.left+2,in.bottom-3}, {in.right-3,in.bottom-3},
        {(in.left+in.right)/2,(in.top+in.bottom)/2}};
      const char* names[] = {"left-margin","right-margin","top-margin","bottom-margin","top-left-corner","top-right-corner","bottom-left-corner","bottom-right-corner","panel-body"};
      for (int i=0;i<9;++i) {
        const HWND target = WindowFromPoint(points[i]); DWORD targetPid = 0; GetWindowThreadProcessId(target, &targetPid);
        const bool pass = i == 8 ? target == input : target == window;
        hits.push_back({{"name",names[i]}, {"screenPoint",{points[i].x,points[i].y}}, {"targetPid",targetPid},
          {"expected",i == 8 ? "host-input-window" : "separate-underlay-process"}, {"pass",pass}});
      }
    }
    write(folder / L"underlay.json", {{"mode","cross-process-real-input-underlay"},{"network","NONE"},
      {"underlayPid",GetCurrentProcessId()},{"hostPid",child.dwProcessId}, {"ownerBinding",true},
      {"underlayRect",rectangle(window)},{"renderRect",rectangle(visual)},{"inputRect",rectangle(input)},
      {"renderVisible",visual && IsWindowVisible(visual)!=FALSE},{"inputVisible",input && IsWindowVisible(input)!=FALSE},
      {"renderExtendedStyle",visual?DWORD(GetWindowLongPtrW(visual,GWL_EXSTYLE)):0},
      {"inputExtendedStyle",input?DWORD(GetWindowLongPtrW(input,GWL_EXSTYLE)):0},
      {"leftDown",leftDown},{"leftUp",leftUp},{"rightUp",rightUp},{"wheel",wheel},
      {"events",events},{"windowFromPointChecks",hits},
      {"evidenceRule","WindowFromPoint is diagnostic only. Cross-process pass-through requires the real CUA clicks and resulting underlay events."}});
  }
  void record(const char* kind, LPARAM point, bool screenCoordinates = false, int delta = 0) {
    POINT p{GET_X_LPARAM(point),GET_Y_LPARAM(point)};
    if (!screenCoordinates) ClientToScreen(window, &p);
    events.push_back({{"kind",kind},{"screenPoint",{p.x,p.y}},{"messageTime",DWORD(GetMessageTime())},{"wheelDelta",delta}});
    if(events.size()>32)events.erase(events.begin());
    save(); InvalidateRect(window, nullptr, FALSE);
  }
  void paint(HDC dc) {
    RECT r{}; GetClientRect(window,&r);
    HBRUSH a=CreateSolidBrush(RGB(126,165,199)), b=CreateSolidBrush(RGB(205,177,125));
    for(int y=0;y<r.bottom;y+=72)for(int x=0;x<r.right;x+=72){RECT box{x,y,x+72,y+72};FillRect(dc,&box,((x/72+y/72)&1)?a:b);}
    DeleteObject(a);DeleteObject(b); SetBkMode(dc,OPAQUE);SetBkColor(dc,RGB(249,249,245));SetTextColor(dc,RGB(25,35,42));
    const std::wstring label=L"Independent underlay process. Real clicks on glow margins/cut corners increment these counts.\r\nLeft down: "+std::to_wstring(leftDown)+L"  Left up: "+std::to_wstring(leftUp)+L"  Right up: "+std::to_wstring(rightUp)+L"  Wheel: "+std::to_wstring(wheel)+L"\r\nF1: show/reopen the owned test display. Closing this underlay closes only its test child.";
    RECT labelRect{12,8,r.right-12,88}; DrawTextW(dc,label.c_str(),-1,&labelRect,DT_LEFT|DT_TOP);
  }
};

LRESULT CALLBACK procedure(HWND hwnd,UINT message,WPARAM wp,LPARAM lp) {
  auto* self=reinterpret_cast<Harness*>(GetWindowLongPtrW(hwnd,GWLP_USERDATA));
  if(message==WM_NCCREATE){self=static_cast<Harness*>(reinterpret_cast<CREATESTRUCTW*>(lp)->lpCreateParams);self->window=hwnd;SetWindowLongPtrW(hwnd,GWLP_USERDATA,reinterpret_cast<LONG_PTR>(self));}
  if(!self)return DefWindowProcW(hwnd,message,wp,lp);
  switch(message){
  case WM_LBUTTONDOWN:++self->leftDown;self->record("left-down",lp);return 0;
  case WM_LBUTTONUP:++self->leftUp;self->record("left-up",lp);return 0;
  case WM_RBUTTONUP:++self->rightUp;self->record("right-up",lp);return 0;
  case WM_MOUSEWHEEL:++self->wheel;self->record("wheel",lp,true,GET_WHEEL_DELTA_WPARAM(wp));return 0;
  case WM_KEYDOWN:if(wp==VK_F1){self->discover();if(self->visual)PostMessageW(self->visual,WM_APP+2,0,0);}return 0;
  case WM_PAINT:{PAINTSTRUCT ps{};HDC dc=BeginPaint(hwnd,&ps);self->paint(dc);EndPaint(hwnd,&ps);return 0;}
  case WM_ERASEBKGND:return 1;
  case WM_TIMER:self->launch();self->save();if(GetTickCount64()-self->began>20*60*1000)PostMessageW(hwnd,WM_CLOSE,0,0);return 0;
  case WM_CLOSE:self->discover();if(self->visual)PostMessageW(self->visual,WM_CLOSE,0,0);DestroyWindow(hwnd);return 0;
  case WM_DESTROY:self->save();PostQuitMessage(0);return 0;
  }
  return DefWindowProcW(hwnd,message,wp,lp);
}

int WINAPI wWinMain(HINSTANCE instance,HINSTANCE,PWSTR,int) {
  SetProcessDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);
  Harness state; int argc=0;LPWSTR* argv=CommandLineToArgvW(GetCommandLineW(),&argc);
  for(int i=1;i<argc;++i){std::wstring arg=argv[i];if(i+1>=argc){LocalFree(argv);return 2;}if(arg==L"--host")state.host=fs::absolute(argv[++i]);else if(arg==L"--output")state.folder=fs::absolute(argv[++i]);else{LocalFree(argv);return 2;}}
  LocalFree(argv);if(state.host.empty()||state.folder.empty()||!fs::is_regular_file(state.host))return 2;
  WNDCLASSEXW wc{sizeof(wc)};wc.hInstance=instance;wc.lpfnWndProc=procedure;wc.hCursor=LoadCursorW(nullptr,IDC_ARROW);wc.lpszClassName=L"CodexObservatoryInputBoundaryUnderlay";RegisterClassExW(&wc);
  RECT work{};SystemParametersInfoW(SPI_GETWORKAREA,0,&work,0);
  HWND hwnd=CreateWindowExW(0,wc.lpszClassName,L"Codex Observatory - isolated cross-process input underlay",WS_OVERLAPPEDWINDOW,
    work.left+30,work.top+30,work.right-work.left-60,work.bottom-work.top-60,nullptr,nullptr,instance,&state);
  if(!hwnd)return 2;
  ShowWindow(hwnd,SW_SHOWDEFAULT);ShowWindow(hwnd,SW_SHOWNORMAL);UpdateWindow(hwnd);SetTimer(hwnd,1,350,nullptr);
  MSG message{};while(GetMessageW(&message,nullptr,0,0)>0){TranslateMessage(&message);DispatchMessageW(&message);}
  if(state.child.hProcess){WaitForSingleObject(state.child.hProcess,8000);CloseHandle(state.child.hProcess);}if(state.child.hThread)CloseHandle(state.child.hThread);
  return 0;
}
