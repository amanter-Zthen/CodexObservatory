'use strict';
// Script-contract tests only: synthetic files/PE headers and a fake compiler.
// No Go test, real MSVC compilation, GPU rendering or application launch is performed here.
const assert = require('assert');
const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');
const release = require('../tools/release-common.cjs');
const portable = path.resolve(__dirname, '..');
const fixtureParent = fs.mkdtempSync(path.join(portable, '.build-script-tests-'));
const root = path.join(fixtureParent, 'repository with spaces');
const checks = [];
let failure = null;
function write(file, data) {
  const target = path.join(root, file);
  fs.mkdirSync(path.dirname(target), { recursive: true });
  fs.writeFileSync(target, data);
}
function json(file, data) { write(file, JSON.stringify(data, null, 2)); }
function test(name, action) {
  try { action(); checks.push({ name, pass: true }); }
  catch (error) { checks.push({ name, pass: false, error: error.message }); throw error; }
}
function runPackage(args = []) {
  return spawnSync(process.execPath, [path.join(root, 'tools/package-release.cjs'), ...args], { cwd: root, encoding: 'utf8', windowsHide: true, timeout: 30000, maxBuffer: 1024 * 1024 });
}
function packagePass(args = []) {
  const result = runPackage(args);
  assert.equal(result.status, 0, result.stderr || result.error?.message);
  return JSON.parse(result.stdout);
}
function packageFail(pattern, args = ['--check']) {
  const result = runPackage(args);
  assert.notEqual(result.status, 0, 'Expected packaging to reject the fixture');
  assert.match(result.stderr, pattern);
}
function fakePE(subsystem, marker) {
  const image = Buffer.alloc(256);
  image.writeUInt16LE(0x5a4d, 0); image.writeUInt32LE(0x40, 0x3c);
  image.writeUInt32LE(0x4550, 0x40); image.writeUInt16LE(0x8664, 0x44);
  image.writeUInt16LE(0x20b, 0x58); image.writeUInt16LE(subsystem, 0x58 + 68);
  image[250] = marker;
  return image;
}

try {
  for (const file of ['tools/release-common.cjs', 'tools/package-release.cjs', 'tools/build-desktop.ps1', 'deployment/build-overlay.cjs']) write(file, fs.readFileSync(path.join(portable, file)));
  const overlayNames = ['display_host_windows.go', 'display_host_windows_test.go', 'display_controls_windows.go', 'display_controls_windows_test.go', 'route_pulse_windows.go', 'route_pulse_windows_test.go', 'startup_visibility_windows_test.go'];
  for (const file of overlayNames) write('deployment/overlay/' + file, '// synthetic script-contract fixture\n');
  for (const file of ['native/fixture.go', 'native/.fixture-dotfile', 'native/LICENSE.txt', 'native/THIRD_PARTY_NOTICES.md', 'native/licenses/Go-LICENSE.txt', 'native/vendor/dependency/LICENSE']) write(file, 'Synthetic fixture only; not a product build.\n');
  const nativePaths = release.walk(root, 'native');
  const nativeManifest = { files: nativePaths.map(file => ({ path: file, sha256: release.hashFile(path.join(root, file)) })) };
  json('deployment/native-source-manifest.json', nativeManifest);
  test('native manifest covers complete tree including dotfiles', () => assert.equal(release.verifyNative(root).files, nativePaths.length));
  test('changed native source is rejected', () => {
    const source = fs.readFileSync(path.join(root, 'native/fixture.go'));
    write('native/fixture.go', 'changed');
    assert.throws(() => release.verifyNative(root), /hash mismatch/);
    write('native/fixture.go', source);
  });
  test('extra native file is rejected', () => {
    write('native/unlisted.go', 'extra');
    assert.throws(() => release.verifyNative(root), /exact tree inventory/);
    fs.unlinkSync(path.join(root, 'native/unlisted.go'));
  });
  test('manifest traversal and case-insensitive duplicates are rejected', () => {
    json('deployment/native-source-manifest.json', { files: [{ path: 'native/../escape', sha256: '0'.repeat(64) }] });
    assert.throws(() => release.verifyNative(root), /Unsafe relative path/);
    json('deployment/native-source-manifest.json', { files: [...nativeManifest.files, { ...nativeManifest.files[0], path: nativeManifest.files[0].path.toUpperCase().replace(/^NATIVE\//, 'native/') }] });
    assert.throws(() => release.verifyNative(root), /Duplicate native manifest path/);
    json('deployment/native-source-manifest.json', nativeManifest);
  });
  const runtime = Array.from({ length: 6 }, (_, i) => (i ? 'engine/' : 'background/') + 'layer-' + i + '.js');
  for (const file of runtime) write('visual/' + file, '// synthetic layer fixture\n');
  const html = runtime.map(file => '<script src="' + file + '"></script>').join('\n');
  write('visual/index.html', html);
  const visualLock = { version: 'TEST-FIXTURE-ONLY', runtimeHashes: Object.fromEntries(runtime.map(file => [file, release.hashFile(path.join(root, 'visual', file))])), previewHtmlSha256: release.sha256(html), layers: runtime.map((file, i) => ({ id: String(i), files: [file] })) };
  json('layers.lock.json', visualLock);
  test('six-layer lock and HTML load list agree', () => assert.equal(release.verifyVisual(root).scripts, 6));
  test('unlocked visual JavaScript is rejected', () => {
    write('visual/engine/unlisted.js', 'extra');
    assert.throws(() => release.verifyVisual(root), /inventory differs/);
    fs.unlinkSync(path.join(root, 'visual/engine/unlisted.js'));
  });
  test('HTML script omissions are rejected even with a refreshed HTML digest', () => {
    const changed = html.replace(/<script[^>]*>[^<]*<\/script>/, '');
    write('visual/index.html', changed); json('layers.lock.json', { ...visualLock, previewHtmlSha256: release.sha256(changed) });
    assert.throws(() => release.verifyVisual(root), /script list/);
    write('visual/index.html', html); json('layers.lock.json', visualLock);
  });
  for (const file of ['desktop/host.cpp', 'desktop/composition_host.h', 'desktop/input_region.h', 'desktop/legacy_menu.h', 'desktop/vendor/json.hpp']) write(file, '// synthetic source fixture\n');
  for (const file of ['json-LICENSE.MIT', 'WebView2-LICENSE.txt', 'WebView2-NOTICE.txt']) write('desktop/vendor/' + file, 'Synthetic license fixture.\n');
  for (const file of ['LICENSE.RhineLabUI.txt', 'SOURCES.txt', 'ARCHIVE_REFERENCE.txt']) write('visual/references/' + file, 'Synthetic source fixture.\n');
  for (const file of ['LICENSE', 'ACKNOWLEDGMENTS.md', 'THIRD_PARTY_NOTICES.md', 'README.md', 'README.en.md', 'docs/图层定稿.md', 'docs/玻璃参数.md']) write(file, 'SCRIPT CONTRACT TEST FIXTURE ONLY\n');
  for (const guide of ['BUILD', 'INTEGRATION', 'API', 'LAYERS', 'MEDIA', 'VALIDATION']) for (const language of ['zh-CN', 'en']) write('docs/' + guide + '.' + language + '.md', 'Synthetic public guide fixture.\n');
  for (let i = 0; i < 6; i++) { json('layers/' + i + '.json', { fixture: true, layer: i }); write('media/layer-' + i + '.png', 'Synthetic image placeholder; not a real image.'); }
  write('media/poster.jpg', 'Synthetic media fixture.'); write('media/preview.gif', 'Synthetic media fixture.');
  json('deployment/display-host.schema.json', { fixture: true }); json('interfaces/snapshot-fields.json', { fixture: true });
  const artifacts = {};
  for (const [name, filename, subsystem, marker] of [['backend', 'CodexObservatory.exe', 2, 1], ['watcher', 'CodexObservedLauncher.exe', 2, 2], ['bridge', 'CodexRoutingBridge.exe', 3, 3]]) {
    const file = 'deployment/build/' + filename; write(file, fakePE(subsystem, marker));
    artifacts[name] = { path: file, ...release.inspectPE(path.join(root, file), subsystem) };
  }
  const baseline = release.verifyNative(root);
  const backend = { formatVersion: 1, kind: 'codex-observatory-backend-build', result: 'PASS', testsPassed: true, nativeUnchanged: true, nativeManifestSHA256: baseline.manifestSHA256, nativeTreeSHA256: release.sha256(JSON.stringify(baseline.hashes)), sourceHashes: release.hashFiles(root, ['deployment/build-overlay.cjs', 'deployment/native-source-manifest.json', 'tools/release-common.cjs', ...overlayNames.map(file => 'deployment/overlay/' + file)]), artifacts, testSummary: { topLevelPasses: 26, failures: 0, names: ['SIMULATED_REPORT_FOR_SCRIPT_TEST_ONLY_NO_GO_TESTS_RUN'] } };
  json('deployment/build-report.json', backend);
  if (process.platform !== 'win32') throw new Error('This contract test includes Windows PowerShell argument handling; run it on Windows.');
  write('.sdk/build/native/include/WebView2.h', '// synthetic SDK header\n');
  write('.sdk/build/native/x64/WebView2LoaderStatic.lib', 'synthetic SDK library');
  function sdkVersion(version) { write('.sdk/Microsoft.Web.WebView2.nuspec', '<package><metadata><id>Microsoft.Web.WebView2</id><version>' + version + '</version></metadata></package>'); }
  sdkVersion('1.0.2903.40');
  write('.compiler/vcvars64.bat', '@set "PATH=%~dp0;%PATH%"\r\n@exit /b 0\r\n');
  write('.compiler/cl.cmd', '@powershell.exe -NoProfile -NonInteractive -File "%~dp0fake-compiler.ps1"\r\n@exit /b %errorlevel%\r\n');
  write('.compiler/fake-compiler.ps1', "$ErrorActionPreference='Stop'\n$bytes=[Convert]::FromBase64String('" + fakePE(2, 4).toString('base64') + "')\n[IO.File]::WriteAllBytes((Join-Path (Get-Location) '..\\CodexObservatory.exe'),$bytes)\nAdd-Content -LiteralPath 'mock-compiler-runs.txt' -Value 'fixture'\nexit 0\n");
  function desktopBuild(explicit = true) {
    const args = ['-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', path.join(root, 'tools/build-desktop.ps1')];
    if (explicit) args.push('-Sdk', path.join(root, '.sdk'), '-VcVars', path.join(root, '.compiler/vcvars64.bat'));
    const env = { ...process.env, OBSERVATORY_WEBVIEW2_SDK: path.join(root, explicit ? '.invalid-sdk' : '.sdk'), OBSERVATORY_VCVARS: path.join(root, explicit ? '.invalid-vcvars' : '.compiler/vcvars64.bat') };
    return spawnSync('powershell.exe', args, { cwd: root, env, encoding: 'utf8', windowsHide: true, timeout: 30000, maxBuffer: 1024 * 1024 });
  }
  test('desktop explicit parameters override environment and support spaces/Unicode checkout paths', () => {
    const result = desktopBuild(true); assert.equal(result.status, 0, result.stderr || result.stdout);
    const report = release.readJSON(root, 'desktop/build/build-report.json'); assert.equal(report.result, 'PASS'); assert.equal(report.artifact.subsystem, 2);
    assert(!JSON.stringify(report).includes(root), 'Build report must use relative source paths');
  });
  test('desktop environment dependency selection works without explicit parameters', () => {
    const result = desktopBuild(false); assert.equal(result.status, 0, result.stderr || result.stdout);
  });
  test('SDK version mismatch fails before compiler and invalidates stale success report', () => {
    const count = fs.readFileSync(path.join(root, 'desktop/build/mock-compiler-runs.txt'), 'utf8'); sdkVersion('9.9.9');
    const result = desktopBuild(); assert.notEqual(result.status, 0); assert.equal(release.readJSON(root, 'desktop/build/build-report.json').result, 'FAIL');
    assert.equal(fs.readFileSync(path.join(root, 'desktop/build/mock-compiler-runs.txt'), 'utf8'), count);
    packageFail(/successful current desktop build/);
    sdkVersion('1.0.2903.40'); const repaired = desktopBuild(); assert.equal(repaired.status, 0, repaired.stderr || repaired.stdout);
  });
  test('package check validates complete current inputs without generating output', () => {
    assert.equal(packagePass(['--check']).mode, 'CHECK_ONLY'); assert(!fs.existsSync(path.join(root, 'dist')));
  });
  test('complete runtime layout, media, public docs and SHA256 inventories are assembled', () => {
    packagePass(); const output = path.join(root, 'dist/CodexObservatory');
    const manifest = JSON.parse(fs.readFileSync(path.join(output, 'manifest.json'), 'utf8'));
    for (const file of manifest.files) assert.equal(release.hashFile(path.join(output, file.path)), file.sha256);
    const expected = ['CodexObservatory.exe', 'CodexObservedLauncher.exe', 'CodexRoutingBridge.exe', 'desktop/CodexObservatory.exe', 'visual/index.html', 'README.md', 'README.en.md', 'RUNTIME.md', 'docs/BUILD.en.md', 'docs/VALIDATION.zh-CN.md', 'media/preview.gif', 'layers/0.json', 'licenses/native/vendor/dependency/LICENSE'];
    for (const file of expected) assert(manifest.files.some(item => item.path === file), 'Missing package file ' + file);
    assert.equal(fs.readFileSync(path.join(output, 'README.md'), 'utf8'), fs.readFileSync(path.join(root, 'README.md'), 'utf8'));
    assert.equal(JSON.parse(fs.readFileSync(path.join(output, 'display-host.example.json'))).enabled, false);
    assert(!fs.existsSync(path.join(output, 'display-host.json'))); assert(!fs.existsSync(path.join(output, 'desktop/state')));
    for (const line of fs.readFileSync(path.join(output, 'SHA256SUMS.txt'), 'utf8').trim().split('\n')) { const digest = line.slice(0, 64), file = line.slice(66); assert.equal(release.hashFile(path.join(output, file)), digest); }
  });
  test('existing outputs and escaped destinations are rejected', () => { packageFail(/already exists/, []); packageFail(/Unsafe relative path/, ['--out', '../escape']); packageFail(/below dist/, ['--out', 'desktop/package']); });
  test('binary tampering is rejected', () => { const file = 'desktop/CodexObservatory.exe', data = fs.readFileSync(path.join(root, file)); write(file, fakePE(2, 99)); packageFail(/differs from its current build/); write(file, data); });
  test('post-build C++ source edits are rejected', () => { const file = 'desktop/legacy_menu.h', data = fs.readFileSync(path.join(root, file)); write(file, 'changed'); packageFail(/hash mismatch/); write(file, data); });
  test('failed overlay tests and a missing watcher are rejected', () => {
    json('deployment/build-report.json', { ...backend, testsPassed: false }); packageFail(/successful current backend build/); json('deployment/build-report.json', backend);
    const file = 'deployment/build/CodexObservedLauncher.exe', data = fs.readFileSync(path.join(root, file)); fs.unlinkSync(path.join(root, file)); packageFail(/missing/); write(file, data);
  });
  test('missing public guide blocks incomplete release', () => { const file = 'docs/MEDIA.en.md', data = fs.readFileSync(path.join(root, file)); fs.unlinkSync(path.join(root, file)); packageFail(/missing/); write(file, data); });
} catch (error) {
  failure = error.message;
  process.exitCode = 1;
  console.error(error.stack || error);
} finally {
  const report = { scope: 'SCRIPT_CONTRACTS_WITH_SYNTHETIC_FILES_AND_FAKE_COMPILER_ONLY', result: !failure && checks.length && checks.every(check => check.pass) ? 'PASS' : 'FAIL', actualCompilationPerformed: false, applicationsStarted: false, gpuTestsPerformed: false, fixtureDirectory: path.relative(portable, root).replaceAll('\\', '/'), checks, ...(failure ? { error: failure } : {}) };
  fs.writeFileSync(path.join(__dirname, 'publication-build-results.json'), JSON.stringify(report, null, 2));
  console.log(JSON.stringify({ result: report.result, checks: checks.length, report: 'tests/publication-build-results.json', scope: report.scope }, null, 2));
}
