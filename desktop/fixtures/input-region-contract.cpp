#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include <filesystem>
#include <fstream>
#include <string>
#include <vector>
#include "../vendor/json.hpp"
#include "../input_region.h"
using Json = nlohmann::json;
namespace fs = std::filesystem;

LRESULT CALLBACK inputProcedure(HWND hwnd, UINT message, WPARAM wp, LPARAM lp) {
  auto* region = reinterpret_cast<PanelInputRegion*>(GetWindowLongPtrW(hwnd, GWLP_USERDATA));
  if (message == WM_NCCREATE) {
    region = static_cast<PanelInputRegion*>(reinterpret_cast<CREATESTRUCTW*>(lp)->lpCreateParams);
    SetWindowLongPtrW(hwnd, GWLP_USERDATA, reinterpret_cast<LONG_PTR>(region));
  }
  if (message == WM_NCDESTROY && region) region->destroyed(hwnd);
  return DefWindowProcW(hwnd, message, wp, lp);
}

int wmain(int argc, wchar_t** argv) {
  if (argc != 2) return 2;
  SetProcessDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);
  HINSTANCE instance = GetModuleHandleW(nullptr);
  WNDCLASSW type{}; type.hInstance = instance; type.lpfnWndProc = DefWindowProcW;
  type.lpszClassName = L"CodexObservatoryInputRegionContract"; RegisterClassW(&type);
  HWND visual = CreateWindowExW(WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE | WS_EX_NOREDIRECTIONBITMAP,
    type.lpszClassName, L"Hidden input-region contract", WS_POPUP, -340, -180, 900, 700,
    nullptr, nullptr, instance, nullptr);
  PanelInputRegion region; Json checks = Json::array(); bool passed = visual != nullptr;
  auto check = [&](const char* name, bool value) { checks.push_back({{"name", name}, {"pass", value}}); passed = passed && value; };
  check("layered render pass-through style initialized", visual && PanelInputRegion::enableVisualPassThrough(visual));
  check("input window owned by renderer", region.create(instance, visual, inputProcedure, &region) && GetWindow(region.hwnd(), GW_OWNER) == visual);
  const auto ex = GetWindowLongPtrW(visual, GWL_EXSTYLE);
  check("full render uses layered transparent no-redirection styles", (ex & (WS_EX_LAYERED | WS_EX_TRANSPARENT | WS_EX_NOREDIRECTIONBITMAP)) == (WS_EX_LAYERED | WS_EX_TRANSPARENT | WS_EX_NOREDIRECTIONBITMAP));
  COLORREF key{}; BYTE alpha = 0; DWORD flags = 0;
  check("render alpha remains 255 with no color key", GetLayeredWindowAttributes(visual, &key, &alpha, &flags) && alpha == 255 && flags == LWA_ALPHA);
  const auto inputEx = GetWindowLongPtrW(region.hwnd(), GWL_EXSTYLE);
  check("input has no layered or transparent style", !(inputEx & (WS_EX_LAYERED | WS_EX_TRANSPARENT)) && (inputEx & WS_EX_NOREDIRECTIONBITMAP));
  int cases = 0; bool geometry = true, corners = true, renderUnclipped = true;
  const DWORD gdiBefore = GetGuiResources(GetCurrentProcess(), GR_GDIOBJECTS);
  for (const double dpi : {96., 144., 192., 216.}) for (const double zoom : {.75, 1., 1.25, 1.5, 2.}) for (const int bodyHeight : {112, 188, 500}) {
    const double scale = dpi / 96. * zoom;
    const int w = int(std::ceil((388 + 256) * scale)), h = int(std::ceil((bodyHeight + 256) * scale));
    SetWindowPos(visual, nullptr, -340, -180, w, h, SWP_NOZORDER | SWP_NOACTIVATE);
    geometry = geometry && region.sync(visual, 128 * scale, 14 * scale, false);
    RECT outer{}, input{}; GetWindowRect(visual, &outer); GetWindowRect(region.hwnd(), &input);
    const int inset = int(std::ceil(128 * scale));
    geometry = geometry && input.left == outer.left + inset && input.top == outer.top + inset &&
      input.right == outer.right - inset && input.bottom == outer.bottom - inset;
    HRGN shape = CreateRectRgn(0, 0, 0, 0), visualShape = CreateRectRgn(0, 0, 0, 0);
    GetWindowRgn(region.hwnd(), shape);
    const int iw = input.right - input.left, ih = input.bottom - input.top;
    corners = corners && !PtInRegion(shape, 1, 1) && !PtInRegion(shape, iw-2, 1) &&
      !PtInRegion(shape, 1, ih-2) && !PtInRegion(shape, iw-2, ih-2) &&
      PtInRegion(shape, iw/2, ih/2) && PtInRegion(shape, iw/2, 1) && PtInRegion(shape, 1, ih/2);
    renderUnclipped = renderUnclipped && GetWindowRgn(visual, visualShape) == ERROR;
    DeleteObject(shape); DeleteObject(visualShape); ++cases;
  }
  check("60 geometry cases including negative coordinates", geometry && cases == 60);
  check("all rounded corners pass while body remains hit-testable", corners);
  check("no render-window region clips the glow canvas", renderUnclipped);
  const DWORD gdiAfter = GetGuiResources(GetCurrentProcess(), GR_GDIOBJECTS);
  check("resizing replaces regions without accumulating GDI objects", gdiAfter <= gdiBefore + 2);
  SetWindowPos(visual, HWND_NOTOPMOST, 0, 0, 0, 0, SWP_NOMOVE | SWP_NOSIZE | SWP_NOACTIVATE);
  check("owned input follows non-topmost owner", !(GetWindowLongPtrW(region.hwnd(), GWL_EXSTYLE) & WS_EX_TOPMOST));
  SetWindowPos(visual, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE | SWP_NOSIZE | SWP_NOACTIVATE);
  check("owned input follows topmost owner", (GetWindowLongPtrW(region.hwnd(), GWL_EXSTYLE) & WS_EX_TOPMOST) != 0);
  const HWND priorInput = region.hwnd(); DestroyWindow(visual);
  check("owner destruction removes input window", !IsWindow(priorInput) && region.hwnd() == nullptr);
  const fs::path report = fs::absolute(argv[1]); fs::create_directories(report.parent_path());
  std::ofstream out(report, std::ios::binary); out << Json{{"result", passed ? "PASS" : "FAIL"}, {"mode", "hidden-native-region-contract"}, {"gpuStarted", false}, {"realMouseInputTested", false}, {"geometryCases", cases}, {"gdiBefore", gdiBefore}, {"gdiAfter", gdiAfter}, {"checks", checks}}.dump(2);
  return passed ? 0 : 2;
}
