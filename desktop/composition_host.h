#pragma once
#include <dcomp.h>
#include <windowsx.h>

// WebView2 owns the rendered surfaces. This small tree submits them directly to
// DWM, retaining per-pixel alpha and avoiding a CPU readback or a color key.
class CompositionHost {
  Microsoft::WRL::ComPtr<IDCompositionDevice> device;
  Microsoft::WRL::ComPtr<IDCompositionTarget> target;
  Microsoft::WRL::ComPtr<IDCompositionVisual> root, content;
  Microsoft::WRL::ComPtr<ICoreWebView2CompositionController> composition;
  EventRegistrationToken cursorToken{};
  HWND window = nullptr, inputWindow = nullptr;
  bool cursorRegistered = false, trackingMouse = false;
public:
  bool attached = false;
  unsigned mouseMessages = 0, wheelMessages = 0, inputFailures = 0;
  BYTE backgroundAlpha = 255;

  HRESULT attach(HWND hwnd, ICoreWebView2CompositionController* view,
                 ICoreWebView2Controller* controller, HWND input = nullptr) {
    window = hwnd; inputWindow = input ? input : hwnd; composition = view;
    HRESULT hr = DCompositionCreateDevice2(nullptr, IID_PPV_ARGS(&device));
    if (SUCCEEDED(hr)) hr = device->CreateTargetForHwnd(hwnd, TRUE, &target);
    if (SUCCEEDED(hr)) hr = device->CreateVisual(&root);
    if (SUCCEEDED(hr)) hr = device->CreateVisual(&content);
    if (SUCCEEDED(hr)) hr = root->AddVisual(content.Get(), FALSE, nullptr);
    if (SUCCEEDED(hr)) hr = target->SetRoot(root.Get());
    if (SUCCEEDED(hr)) hr = composition->put_RootVisualTarget(content.Get());
    Microsoft::WRL::ComPtr<ICoreWebView2Controller2> controller2;
    if (SUCCEEDED(hr)) hr = controller->QueryInterface(IID_PPV_ARGS(&controller2));
    if (SUCCEEDED(hr)) hr = controller2->put_DefaultBackgroundColor({0, 0, 0, 0});
    COREWEBVIEW2_COLOR color{};
    if (SUCCEEDED(hr)) hr = controller2->get_DefaultBackgroundColor(&color);
    if (SUCCEEDED(hr)) { backgroundAlpha = color.A; hr = device->Commit(); }
    if (SUCCEEDED(hr)) {
      hr = composition->add_CursorChanged(
        Microsoft::WRL::Callback<ICoreWebView2CursorChangedEventHandler>(
          [this](ICoreWebView2CompositionController*, IUnknown*) -> HRESULT {
            POINT p{}; GetCursorPos(&p);
            if (WindowFromPoint(p) == inputWindow) setCursor();
            return S_OK;
          }).Get(), &cursorToken);
      cursorRegistered = SUCCEEDED(hr);
    }
    attached = SUCCEEDED(hr);
    if (!attached) detach();
    return hr;
  }
  void detach() {
    if (GetCapture() == window || GetCapture() == inputWindow) ReleaseCapture();
    if (composition) {
      if (cursorRegistered) composition->remove_CursorChanged(cursorToken);
      composition->put_RootVisualTarget(nullptr);
    }
    if (target) target->SetRoot(nullptr);
    if (root) root->RemoveAllVisuals();
    if (device) device->Commit();
    cursorRegistered = false; attached = false; trackingMouse = false;
    composition.Reset(); content.Reset(); root.Reset(); target.Reset(); device.Reset();
  }
  bool setCursor() {
    if (!composition || !attached) return false;
    HCURSOR cursor = nullptr;
    if (FAILED(composition->get_Cursor(&cursor))) return false;
    SetCursor(cursor); return true;
  }
  void resize(const RECT& bounds) {
    if (!attached || !root) return;
    // The submitted canvas already includes its page-negotiated transparent glow margin.
    // Clip only at the full HWND extent, never at the rounded panel itself.
    root->SetClip(D2D_RECT_F{0, 0, float(bounds.right - bounds.left), float(bounds.bottom - bounds.top)});
    device->Commit();
  }
  bool mouse(UINT message, WPARAM wp, LPARAM lp, HWND source = nullptr) {
    if (!composition || !attached) return false;
    switch (message) {
    case WM_MOUSEMOVE: case WM_MOUSELEAVE:
    case WM_LBUTTONDOWN: case WM_LBUTTONUP: case WM_LBUTTONDBLCLK:
    case WM_RBUTTONDOWN: case WM_RBUTTONUP: case WM_RBUTTONDBLCLK:
    case WM_MBUTTONDOWN: case WM_MBUTTONUP: case WM_MBUTTONDBLCLK:
    case WM_XBUTTONDOWN: case WM_XBUTTONUP: case WM_XBUTTONDBLCLK:
    case WM_MOUSEWHEEL: case WM_MOUSEHWHEEL: break;
    default: return false;
    }
    if (!source) source = window;
    POINT point{GET_X_LPARAM(lp), GET_Y_LPARAM(lp)};
    DWORD data = 0;
    auto keys = COREWEBVIEW2_MOUSE_EVENT_VIRTUAL_KEYS(GET_KEYSTATE_WPARAM(wp));
    if (message == WM_MOUSEWHEEL || message == WM_MOUSEHWHEEL) {
      ScreenToClient(window, &point);
      data = DWORD(int(GET_WHEEL_DELTA_WPARAM(wp))); ++wheelMessages;
    } else {
      if (source != window && message != WM_MOUSELEAVE) MapWindowPoints(source, window, &point, 1);
      if (message == WM_XBUTTONDOWN || message == WM_XBUTTONUP || message == WM_XBUTTONDBLCLK)
        data = GET_XBUTTON_WPARAM(wp);
    }
    if (message == WM_MOUSELEAVE) {
      point = {}; keys = COREWEBVIEW2_MOUSE_EVENT_VIRTUAL_KEYS_NONE;
      trackingMouse = false;
    } else if (message == WM_MOUSEMOVE && !trackingMouse && IsWindowVisible(source)) {
      TRACKMOUSEEVENT tracking{sizeof(tracking), TME_LEAVE, source, 0};
      trackingMouse = TrackMouseEvent(&tracking) != FALSE;
    }
    const bool down = message == WM_LBUTTONDOWN || message == WM_RBUTTONDOWN ||
      message == WM_MBUTTONDOWN || message == WM_XBUTTONDOWN;
    const bool up = message == WM_LBUTTONUP || message == WM_RBUTTONUP ||
      message == WM_MBUTTONUP || message == WM_XBUTTONUP;
    if (down && IsWindowVisible(source)) SetCapture(source);
    const HRESULT hr = composition->SendMouseInput(COREWEBVIEW2_MOUSE_EVENT_KIND(message), keys, data, point);
    if (up && GetCapture() == source && !(LOWORD(wp) & (MK_LBUTTON | MK_RBUTTON | MK_MBUTTON | MK_XBUTTON1 | MK_XBUTTON2)))
      ReleaseCapture();
    ++mouseMessages; if (FAILED(hr)) ++inputFailures;
    return true;
  }
};
