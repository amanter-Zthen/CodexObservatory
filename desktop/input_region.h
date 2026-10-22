#pragma once
#include <windows.h>
#include <algorithm>
#include <cmath>

// Separate USER32 hit geometry from the full-size GPU render target.
// The render HWND uses WS_EX_LAYERED | WS_EX_TRANSPARENT, whose documented
// mouse pass-through applies to windows in other processes. HTTRANSPARENT
// alone only searches windows owned by the same thread.
// https://learn.microsoft.com/windows/win32/winmsg/window-features#layered-windows
// The owned input HWND has no redirection bitmap or visual content. Its region
// clips only input geometry; the visual HWND retains every glow-margin pixel.
class PanelInputRegion {
  HWND input = nullptr;
  int regionWidth = -1, regionHeight = -1, regionRadius = -1;
  RECT localBounds{};
public:
  HWND hwnd() const { return input; }
  RECT bounds() const { return localBounds; }
  int radius() const { return std::max(0, regionRadius); }

  static bool enableVisualPassThrough(HWND visual) {
    const auto style = GetWindowLongPtrW(visual, GWL_EXSTYLE);
    SetWindowLongPtrW(visual, GWL_EXSTYLE,
      style | WS_EX_LAYERED | WS_EX_TRANSPARENT | WS_EX_NOREDIRECTIONBITMAP);
    // Initialize the layered window at full opacity. Per-pixel alpha continues
    // to come from DirectComposition/WebView2, without a color key or readback.
    return SetLayeredWindowAttributes(visual, 0, 255, LWA_ALPHA) != FALSE;
  }

  bool create(HINSTANCE instance, HWND owner, WNDPROC procedure, void* context) {
    if (input) return true;
    constexpr const wchar_t* className = L"CodexObservatoryPanelInput-v1";
    WNDCLASSEXW wc{sizeof(wc)};
    wc.lpfnWndProc = procedure; wc.hInstance = instance;
    wc.hCursor = LoadCursorW(nullptr, IDC_ARROW); wc.lpszClassName = className;
    if (!RegisterClassExW(&wc) && GetLastError() != ERROR_CLASS_ALREADY_EXISTS) return false;
    input = CreateWindowExW(WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE | WS_EX_NOREDIRECTIONBITMAP,
      className, L"Codex Observatory panel input", WS_POPUP,
      0, 0, 1, 1, owner, nullptr, instance, context);
    return input != nullptr;
  }

  void hidden() {
    if (input && IsWindowVisible(input)) ShowWindow(input, SW_HIDE);
  }

  bool sync(HWND visual, double insetPixels, double cornerPixels, bool visible) {
    if (!input || !IsWindow(visual)) return false;
    if (!visible) hidden();
    RECT client{}; if (!GetClientRect(visual, &client)) return false;
    // Ceil keeps the input region on the inner side of fractional-DPI edges.
    const int inset = std::max(0, int(std::ceil(insetPixels)));
    const int width = std::max(0L, client.right - client.left - 2 * inset);
    const int height = std::max(0L, client.bottom - client.top - 2 * inset);
    localBounds = {inset, inset, inset + width, inset + height};
    if (!width || !height) { hidden(); return true; }
    const int radiusPixels = std::clamp(int(std::ceil(cornerPixels)), 0, std::min(width, height) / 2);
    if (width != regionWidth || height != regionHeight || radiusPixels != regionRadius) {
      HRGN region = CreateRoundRectRgn(0, 0, width, height, 2 * radiusPixels, 2 * radiusPixels);
      if (!region) return false;
      // USER32 takes ownership only on success; it releases replaced regions.
      if (!SetWindowRgn(input, region, FALSE)) { DeleteObject(region); return false; }
      regionWidth = width; regionHeight = height; regionRadius = radiusPixels;
    }
    POINT origin{inset, inset}; if (!ClientToScreen(visual, &origin)) return false;
    RECT current{}; GetWindowRect(input, &current);
    if (current.left != origin.x || current.top != origin.y ||
        current.right - current.left != width || current.bottom - current.top != height) {
      // Owned-window ordering follows the render HWND. Moving the input proxy
      // must not raise a non-topmost monitor above an unrelated application.
      if (!SetWindowPos(input, nullptr, origin.x, origin.y, width, height,
          SWP_NOACTIVATE | SWP_NOZORDER | SWP_NOOWNERZORDER)) return false;
    }
    if (visible && !IsWindowVisible(input)) ShowWindow(input, SW_SHOWNOACTIVATE);
    return true;
  }

  void destroyed(HWND window) { if (input == window) input = nullptr; }
  void destroy() {
    HWND old = input; input = nullptr;
    if (old && IsWindow(old)) DestroyWindow(old);
  }
};
