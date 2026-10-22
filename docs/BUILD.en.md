# Build and package

[中文](BUILD.zh-CN.md) · [Home](../README.en.md) · [Integration](INTEGRATION.en.md)

The desktop target is Windows x64. Builds and tests generate files inside your checkout. These commands do not install capture, change Codex connectivity, or start the production monitor.

## 1. Dependencies

| Component | Requirement |
| --- | --- |
| Go | 1.25 or newer; 1.25.14 used for this release; module dependencies are in `native/vendor/` |
| Node.js | 20 or newer, for overlay builds, packaging, and test scripts |
| MSVC + Windows SDK | C++17 x64 toolchain; Visual Studio 2019 Build Tools used for this release |
| WebView2 SDK | Pinned `Microsoft.Web.WebView2` **1.0.2903.40** |
| WebView2 Runtime | Supplied by the system at runtime; no full browser is bundled |
| Browser tests | `npm ci` installs pinned Playwright; use local Chrome/Edge, or `npx playwright install chromium` |

Download and extract the SDK from the [official Microsoft NuGet package](https://www.nuget.org/packages/Microsoft.Web.WebView2/1.0.2903.40). The `-Sdk` directory should contain `Microsoft.Web.WebView2.nuspec` and `build/native/`. The script checks the version, headers, and static loader. Build tools do not download the SDK automatically.

## 2. Build the complete backend

Run from the repository root. Omit the first line if Go is on PATH, or supply the full path to your compiler.

```powershell
$env:OBSERVATORY_GO = 'C:\Tools\Go\bin\go.exe'
node .\deployment\build-overlay.cjs
```

The script checks `deployment/native-source-manifest.json`, projects the 0.4.9 changes through a Go overlay, runs isolated fixtures, and builds the backend, watcher, and compatibility bridge. Original `native/` files remain unchanged before and after the build. Dependencies use vendor, with module downloads disabled during compilation. `OBSERVATORY_GO_CACHE` can select the build cache; otherwise Go uses its normal cache.

Outputs are `deployment/build/CodexObservatory.exe`, `CodexObservedLauncher.exe`, and `CodexRoutingBridge.exe`, with `deployment/build-report.json`. Building native alone does not include the new display interfaces; it represents the preserved old baseline.

## 3. Build the transparent display

```powershell
.\tools\build-desktop.ps1 -Sdk 'C:\Tools\WebView2-1.0.2903.40' -VcVars 'C:\Path\To\VisualStudio\VC\Auxiliary\Build\vcvars64.bat'
```

Alternatively use `OBSERVATORY_WEBVIEW2_SDK` and `OBSERVATORY_VCVARS`, or the supported common VS / NuGet discovery locations. Ordinary Unicode path characters are supported; build paths containing `%`, `!`, double quotes, or newlines are unsupported. Output is `desktop/CodexObservatory.exe`, with `desktop/build/build-report.json`. The WebView2 loader is statically linked; system Runtime is still required.

## 4. Check

```powershell
npm ci
npm test
npm run test:current
npm run test:engineering
npm run test:controls
node .\tools\check-publication.cjs
.\tools\verify-native.ps1 -GoExe 'C:\Tools\Go\bin\go.exe'
```

`OBSERVATORY_CHROME` can point to a browser executable, and `OBSERVATORY_PLAYWRIGHT` to an existing Playwright package. Use `npm run test:stress` for the longer stress check. Tests use constructed state and isolated local services; see [validation](VALIDATION.en.md) for coverage and limits. Results are written to ignored `verification/` or `tests/` output files.

## 5. Assemble a runtime package

```powershell
node .\tools\package-release.cjs --check
node .\tools\package-release.cjs
Compress-Archive -LiteralPath '.\dist\CodexObservatory' -DestinationPath '.\dist\CodexObservatory-0.4.9-windows-x64.zip'
```

Packaging reads the current build reports and actual file hashes, producing `dist/CodexObservatory/`. Existing output is not overwritten. Use `--out dist/NAME` for a different empty destination. The package includes four runtime executables, current visual source, bilingual guides, layer previews and licenses, and a disabled display-configuration example. See [integration](INTEGRATION.en.md) to configure your instance.

The old native README and default directory candidates retain baseline semantics. Public integration uses explicitly configured `roots` / `codex_home`. This repository contains no production state, credentials, CA identities, or deployment records from the author's machine.
