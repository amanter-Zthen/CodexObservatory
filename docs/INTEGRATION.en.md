# Connect your Codex

[简体中文](INTEGRATION.zh-CN.md) · [Project home](../README.en.md) · [Build](BUILD.en.md) · [Six-layer visuals](LAYERS.en.md) · [API](API.en.md)

Codex Observatory configures display and capture separately: you can preview the visuals independently or connect an existing compatible backend. Enabling live capture for the first time requires explicit local proxy, certificate, and startup-follow configuration. Run the following steps manually and replace example paths with your own locations.

## 1. Prepare files and directories

The desktop target is Windows x64, using the installed system WebView2 Runtime. See the [build guide](BUILD.en.md) for source builds. Preserve this layout after extracting a Release, especially the relative locations of `desktop/` and `visual/`.

```text
CodexObservatory/
  CodexObservatory.exe
  CodexObservedLauncher.exe
  CodexRoutingBridge.exe
  display-host.example.json
  desktop/
    CodexObservatory.exe
  visual/
```

| Entry | Purpose |
|---|---|
| Root `CodexObservatory.exe` | Go backend: state, APIs, tray, and the original console. |
| `CodexObservedLauncher.exe` | Capture and startup following; normal running mode is `--watch`. |
| `CodexRoutingBridge.exe` | Legacy IPC/CLI compatibility bridge; the current native-home capture path does not require redirecting `CODEX_CLI_PATH` to it. |
| `desktop/CodexObservatory.exe` | C++ display host: the transparent floating window and WebView2. |
| `visual/` | Complete six-layer scene and animation source. |

Choose three additional directories: your actual **Codex Home**, separate **backend Data**, and separate **DisplayState**. Data must be outside the observed Codex Home. The examples use `C:\Tools\CodexObservatory`, `C:\CodexObservatoryData`, and `C:\CodexObservatoryState`; these paths are not required.

## 2. Preview independently first

Open `visual/index.html` in a browser, or run this from the Release root:

```powershell
.\desktop\CodexObservatory.exe --preview --state 'C:\CodexObservatoryPreview'
```

This uses synthetic data and does not connect to your Codex. The display executable without arguments shows an unconnected state; `--fixture <snapshot.json>` reads a local test snapshot without network access. See [six-layer visuals](LAYERS.en.md) for the layers and controls.

## 3. Select the new display host

The Release's `display-host.example.json` is disabled by default. Copy it to `display-host.json` in the root directory; edit the existing file if already configured. Enter canonical absolute Windows local-drive paths and the actual executable hash, then set `enabled` to `true`.

```powershell
(Get-FileHash -LiteralPath '.\desktop\CodexObservatory.exe' -Algorithm SHA256).Hash.ToLowerInvariant()
```

```json
{
  "enabled": true,
  "executable": "C:\\Tools\\CodexObservatory\\desktop\\CodexObservatory.exe",
  "state": "C:\\CodexObservatoryState",
  "sha256": "REPLACE_WITH_THE_64_HEX_DIGIT_DISPLAY_EXE_HASH"
}
```

Only these four fields are accepted. Use absolute local-drive paths; the display executable must differ from the root backend executable. Invalid configuration, a hash mismatch, or host launch failure falls back to the old display. Recompute and update the hash whenever the display executable changes. Without a valid selector, opening the root backend does not automatically switch to the new display.

For managed startup, the backend passes `--instance`, `--state`, and `--owner-pid`. The display verifies that the owner is its direct parent; ordinary manual startup does not require that argument.

## 4. Connect an existing compatible backend

If you already have a backend supporting the 0.4.9 display APIs, skip first-time capture setup and point the display at `instance.json` in its Data directory:

```powershell
.\desktop\CodexObservatory.exe --instance 'C:\CodexObservatoryData\instance.json' --state 'C:\CodexObservatoryState'
```

The backend generates `instance.json` with a dynamic local address and startup token. Keep it private and do not copy another person's instance file. A normal connection reads current state and sends a lightweight pulse every five seconds by default. Manual resynchronization and backend shutdown are triggered only by their corresponding menu actions. See the [API documentation](API.en.md).

The following check validates read-only snapshot delivery and page acknowledgement. It creates a hidden verification window, exits when finished, and sends no pulse, resync, or shutdown:

```powershell
.\desktop\CodexObservatory.exe --instance 'C:\CodexObservatoryData\instance.json' --state 'C:\CodexObservatoryCheck' --live-check 'C:\CodexObservatoryCheck\bridge.json'
```

A passing check establishes the read/display bridge, not validation of your entire capture and upstream connection.

## 5. Configure live capture for the first time

### 5.1 Review connection changes

Identify your Codex Desktop executable, Codex Home, existing proxy, and certificate settings. Arrange to exit Codex normally before enabling capture and reopen it afterward. Review compatibility with existing proxies, certificates, or other collectors first; the installer rejects directly overwriting network keys outside its managed block.

Enabling capture manages `HTTP_PROXY`, `HTTPS_PROXY`, `SSL_CERT_FILE`, and `NO_PROXY` in Codex Home's `.env`, saves a local CA/proxy identity and recovery material, and registers a current-user Run startup-follow entry. This changes Codex's trust and connection path; it is more than a display setting, and it does not by default import the local CA into the global system root store.

### 5.2 Configure capture

Create `bridge-config.json` in the Release root, beside `CodexObservedLauncher.exe`. Enter the actual installed Desktop and core paths, plus your own Home and Data directories.

```json
{
  "native_codex": "C:\\Path\\To\\Codex\\codex.exe",
  "observatory_data": "C:\\CodexObservatoryData",
  "capture_mode": "local-proxy",
  "loopback_fixture": false,
  "desktop_app": "C:\\Path\\To\\Codex\\ChatGPT.exe",
  "monitor_exe": "C:\\Tools\\CodexObservatory\\CodexObservatory.exe",
  "bridge_exe": "C:\\Tools\\CodexObservatory\\CodexRoutingBridge.exe",
  "codex_home": "C:\\Users\\YOUR_NAME\\.codex"
}
```

Use `local-proxy` for `capture_mode` and `false` for `loopback_fixture`. `native_codex` and `bridge_exe` retain legacy-bridge compatibility information; the key native-home fields are Desktop, Home, Data, and Monitor. Recheck these locations after Codex application-package or core-path updates.

### 5.3 Configure backend scan roots

Create the separate Data directory. Put the following configuration in a new `settings.json`; when editing an existing file, preserve other settings and change only the relevant fields. You can also save these settings through the backend console.

```json
{
  "roots": ["C:\\Users\\YOUR_NAME\\.codex"],
  "poll_seconds": 3,
  "aliases": {}
}
```

This is separate from the launcher's `codex_home`: the watcher starts the backend with `--no-open --data <Data>` and does not forward `--home` as a scan root. Saving explicit `roots` lets the backend read lifecycle and quota records from your selected location.

### 5.4 Enable, check, and open normally

After accepting the connection changes above, run this manually from the Release root:

```powershell
.\CodexObservedLauncher.exe --enable-follow --data 'C:\CodexObservatoryData' --home 'C:\Users\YOUR_NAME\.codex'
```

This configures the capture identity and managed environment block, registers startup following, and starts the watcher. Use `--enable-follow` for first-time installation; `--watch` is for follow operation after the identity exists.

Then run the read-only health check:

```powershell
.\CodexObservedLauncher.exe --health-check --data 'C:\CodexObservatoryData' --home 'C:\Users\YOUR_NAME\.codex'
```

Once the listener is ready, open Codex normally. Startup order is: **watcher/capture listener → normal Codex startup → watcher starts the backend → backend reads the valid selector and starts the display → snapshots enter the six-layer scene**. Zero-row standby is valid when no session is active.

First-time capture uses an OS-assigned loopback port, stores it in Data's `native-capture/identity.json`, and reuses it later. The backend API and metadata feed have separate dynamic local endpoints. You do not need to fix the capture port to 10854, and the capture address is not the display API.

## 6. Daily operation and disabling

The context menu provides quality modes, replay, zoom, current-session and console links, automatic-pulse control, manual resynchronization, and soft hide. Soft hide completes the exit animation, then pauses drawing while the backend keeps running; restore it through the original tray. The new floating layer does not restore the completion-history list.

“Exit Monitor” stops the backend. If the watcher and Codex are still running, the original follow rules may start it again. To disable the integration fully, first exit Codex normally, then run:

```powershell
.\CodexObservedLauncher.exe --disable-follow --data 'C:\CodexObservatoryData' --home 'C:\Users\YOUR_NAME\.codex'
```

Disabling removes this program's managed `.env` block and matching user Run entry. A running core may still hold its startup proxy settings, so do not substitute killing the watcher or deleting Data for disabling. Keep the identity and recovery material until the connection transition is complete.

## 7. Data and validation scope

The new display host retains only current snapshots, latest animation targets, and bounded drawing resources. DisplayState stores window preferences and WebView2 cache, not conversation bodies, conversation history, or a completion inbox. The original Go backend still reads local Codex records and stores indexes, routing evidence, completion state, and optional exports. Keep Data, real configuration, instance tokens, CA private keys, and `.env` backups private.

Capture parses supported requests/responses in memory to extract metadata. Displayed model names are requested and service-reported labels, not independent authentication of model weights. See the [API contract](API.en.md) for rate, color, freshness, and disconnection behavior.

These procedures and related tests have been validated in the development environment; first-time installation has not yet been revalidated on a second machine.
