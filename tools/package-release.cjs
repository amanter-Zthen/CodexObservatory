'use strict';
// Assemble a complete runtime from this checkout's build records. No programs are started.
const fs = require('fs');
const path = require('path');
const release = require('./release-common.cjs');
const root = path.resolve(__dirname, '..');
let outRelative = 'dist/CodexObservatory';
let checkOnly = false;
const args = process.argv.slice(2);
for (let i = 0; i < args.length; i++) {
  if (args[i] === '--help') {
    console.log('Usage: node tools/package-release.cjs [--check] [--out dist/NAME]\nRequires current backend and desktop build reports, the native source manifest, six-layer visual lock, root LICENSE and ACKNOWLEDGMENTS.md. Output is a new directory below dist/. Existing directories are never overwritten. Packaging does not launch, install or configure applications.');
    process.exit(0);
  }
  if (args[i] === '--check') checkOnly = true;
  else if (args[i] === '--out' && args[i + 1]) outRelative = args[++i];
  else throw new Error('Unknown or incomplete argument: ' + args[i]);
}

function requireSourceSet(record, files, label) {
  if (!record || JSON.stringify(Object.keys(record).sort()) !== JSON.stringify([...files].sort())) throw new Error(label + ' does not cover the expected source inputs');
  release.checkHashes(root, record, label);
}

function checkArtifact(record, expectedPath, subsystem) {
  if (!record || record.path !== expectedPath || !release.validHash(record.sha256)) throw new Error('Build artifact record is incomplete: ' + expectedPath);
  const actual = release.inspectPE(release.regularFile(root, expectedPath), subsystem);
  if (actual.sha256 !== record.sha256.toLowerCase() || actual.bytes !== record.bytes) throw new Error('Artifact differs from its current build report: ' + expectedPath);
  return actual;
}

function main() {
  release.relativePath(outRelative);
  if (!outRelative.startsWith('dist/')) throw new Error('Release output must be a new directory below dist/');
  const output = release.projectPath(root, outRelative, true);
  const native = release.verifyNative(root);
  const visual = release.verifyVisual(root);
  const backendReportPath = 'deployment/build-report.json';
  const desktopReportPath = 'desktop/build/build-report.json';
  const backend = release.readJSON(root, backendReportPath);
  const desktop = release.readJSON(root, desktopReportPath);
  if (backend.formatVersion !== 1 || backend.kind !== 'codex-observatory-backend-build' || backend.result !== 'PASS' || backend.testsPassed !== true || backend.nativeUnchanged !== true || backend.nativeManifestSHA256 !== native.manifestSHA256 || backend.nativeTreeSHA256 !== release.sha256(JSON.stringify(native.hashes))) {
    throw new Error('A successful current backend build and unchanged native source are required');
  }
  if (!backend.testSummary || backend.testSummary.topLevelPasses < 26 || backend.testSummary.failures !== 0) throw new Error('The required isolated overlay tests did not pass');
  if (desktop.formatVersion !== 1 || desktop.kind !== 'codex-observatory-desktop-build' || desktop.result !== 'PASS' || desktop.sourceUnchanged !== true || desktop.webview2Sdk !== '1.0.2903.40') {
    throw new Error('A successful current desktop build with the pinned WebView2 SDK is required');
  }
  const overlayNames = ['display_host_windows.go', 'display_host_windows_test.go', 'display_controls_windows.go', 'display_controls_windows_test.go', 'route_pulse_windows.go', 'route_pulse_windows_test.go', 'startup_visibility_windows_test.go'];
  requireSourceSet(backend.sourceHashes, ['deployment/build-overlay.cjs', 'deployment/native-source-manifest.json', 'tools/release-common.cjs', ...overlayNames.map(file => 'deployment/overlay/' + file)], 'Backend build sources');
  requireSourceSet(desktop.sourceHashes, ['desktop/host.cpp', 'desktop/composition_host.h', 'desktop/input_region.h', 'desktop/legacy_menu.h', 'desktop/vendor/json.hpp', 'tools/build-desktop.ps1'], 'Desktop build sources');
  const artifacts = {
    backend: checkArtifact(backend.artifacts && backend.artifacts.backend, 'deployment/build/CodexObservatory.exe', 2),
    watcher: checkArtifact(backend.artifacts && backend.artifacts.watcher, 'deployment/build/CodexObservedLauncher.exe', 2),
    bridge: checkArtifact(backend.artifacts && backend.artifacts.bridge, 'deployment/build/CodexRoutingBridge.exe', 3),
    desktop: checkArtifact(desktop.artifact, 'desktop/CodexObservatory.exe', 2)
  };

  const copies = new Map();
  function add(source, destination = source, expectedHash) {
    release.relativePath(destination);
    if (copies.has(destination)) throw new Error('Duplicate package destination: ' + destination);
    const file = release.regularFile(root, source);
    const digest = release.hashFile(file);
    if (expectedHash && digest !== expectedHash.toLowerCase()) throw new Error('Package source changed: ' + source);
    copies.set(destination, { source, sha256: digest });
  }
  add('deployment/build/CodexObservatory.exe', 'CodexObservatory.exe', artifacts.backend.sha256);
  add('deployment/build/CodexObservedLauncher.exe', 'CodexObservedLauncher.exe', artifacts.watcher.sha256);
  add('deployment/build/CodexRoutingBridge.exe', 'CodexRoutingBridge.exe', artifacts.bridge.sha256);
  add('desktop/CodexObservatory.exe', 'desktop/CodexObservatory.exe', artifacts.desktop.sha256);
  for (const [file, digest] of Object.entries(visual.hashes)) add(file, file, digest);
  add('deployment/display-host.schema.json');
  add('interfaces/snapshot-fields.json');
  for (const file of ['docs/图层定稿.md', 'docs/玻璃参数.md']) add(file);
  for (const guide of ['BUILD', 'INTEGRATION', 'API', 'LAYERS', 'MEDIA', 'VALIDATION']) {
    for (const language of ['zh-CN', 'en']) add('docs/' + guide + '.' + language + '.md');
  }
  for (const file of ['README.md', 'README.en.md', 'THIRD_PARTY_NOTICES.md']) add(file);
  const layerDescriptions = release.walk(root, 'layers').filter(file => /^layers\/[^/]+\.json$/.test(file));
  if (layerDescriptions.length !== visual.lock.layers.length) throw new Error('Public layer descriptions must cover all six layers');
  for (const file of layerDescriptions) add(file);
  add('media/poster.jpg');
  add('media/preview.gif');
  const layerImages = release.walk(root, 'media').filter(file => /^media\/layer-[^/]+\.png$/.test(file));
  if (layerImages.length !== visual.lock.layers.length) throw new Error('Rendered layer images must cover all six layers');
  for (const file of layerImages) add(file);
  const ownLicense = ['LICENSE', 'LICENSE.txt'].find(file => fs.existsSync(path.join(root, file)));
  if (!ownLicense) throw new Error('A root project LICENSE or LICENSE.txt is required');
  add(ownLicense, 'LICENSE');
  add('ACKNOWLEDGMENTS.md');
  add('native/LICENSE.txt', 'licenses/native/LICENSE.txt');
  add('native/THIRD_PARTY_NOTICES.md', 'licenses/native/THIRD_PARTY_NOTICES.md');
  for (const file of release.walk(root, 'native/licenses')) add(file, 'licenses/' + file);
  for (const file of release.walk(root, 'native/vendor').filter(file => /^(?:LICENSE|COPYING|NOTICE)(?:[._-].*)?$/i.test(path.basename(file)))) add(file, 'licenses/' + file);
  for (const file of ['json-LICENSE.MIT', 'WebView2-LICENSE.txt', 'WebView2-NOTICE.txt']) add('desktop/vendor/' + file, 'licenses/desktop/' + file);
  add('visual/references/LICENSE.RhineLabUI.txt', 'licenses/visual/LICENSE.RhineLabUI.txt');
  add('visual/references/SOURCES.txt', 'licenses/visual/SOURCES.txt');
  add('visual/references/ARCHIVE_REFERENCE.txt', 'licenses/visual/ARCHIVE_REFERENCE.txt');

  if (checkOnly) {
    console.log(JSON.stringify({ result: 'PASS', mode: 'CHECK_ONLY', nativeFiles: native.files, visualScripts: visual.scripts, layers: visual.lock.layers.length, runtimeExecutables: Object.keys(artifacts), overlayTests: backend.testSummary, destination: outRelative }, null, 2));
    return;
  }
  if (fs.existsSync(output)) throw new Error('Output already exists; select a fresh --out dist/NAME directory');
  fs.mkdirSync(path.dirname(output), { recursive: true });
  fs.mkdirSync(output);
  const manifest = {
    formatVersion: 1,
    name: 'CodexObservatory',
    version: visual.lock.version,
    revision: visual.lock.publicRelease || visual.lock.shellRevision || visual.lock.engineeringRevision || visual.lock.version,
    assembledAt: new Date().toISOString(),
    platform: 'windows-amd64',
    status: 'ASSEMBLED_FROM_CURRENT_BUILD_RECORDS',
    browserBundled: false,
    liveInstanceIncluded: false,
    programsStartedByPackaging: false,
    visualRuntimeExecutionTestedByPackaging: false,
    entries: { backend: 'CodexObservatory.exe', watcher: 'CodexObservedLauncher.exe', compatibilityBridge: 'CodexRoutingBridge.exe', display: 'desktop/CodexObservatory.exe', preview: 'visual/index.html', displayConfigurationExample: 'display-host.example.json' },
    validation: { nativeSourceManifestSHA256: native.manifestSHA256, backendBuildReportSHA256: release.hashFile(release.regularFile(root, backendReportPath)), desktopBuildReportSHA256: release.hashFile(release.regularFile(root, desktopReportPath)), visualLockSHA256: visual.hashes['layers.lock.json'], packagingScriptSHA256: release.hashFile(__filename), webview2Sdk: desktop.webview2Sdk, overlayTests: backend.testSummary },
    files: []
  };
  function write(relative, data) {
    const file = release.projectPath(output, relative, true);
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, data, { flag: 'wx' });
    manifest.files.push({ path: relative, bytes: fs.statSync(file).size, sha256: release.hashFile(file) });
  }
  for (const [destination, input] of [...copies].sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0)) {
    const data = fs.readFileSync(release.regularFile(root, input.source));
    if (release.sha256(data) !== input.sha256) throw new Error('Source changed during packaging: ' + input.source);
    write(destination, data);
  }
  write('display-host.example.json', JSON.stringify({ enabled: false, executable: 'C:\\Path\\To\\CodexObservatory\\desktop\\CodexObservatory.exe', state: 'C:\\Path\\To\\CodexObservatory\\state', sha256: artifacts.desktop.sha256 }, null, 2) + '\n');
  write('RUNTIME.md', `# Codex Observatory ${visual.lock.version} runtime archive\n\n本目录包含后端、watcher、兼容路由桥、透明悬浮显示、完整六层视觉、公开文档与许可。请保持目录结构；中文首页见 README.md，接口接入见 docs/INTEGRATION.zh-CN.md。包内 display-host.example.json 只是禁用状态的路径示例，实际启用和采集配置由使用者明确操作。\n\nThis archive contains the Go backend, watcher, compatibility routing bridge, transparent Windows display host, all six visual layers and third-party licenses. Keep the directory layout intact. The public README.md / README.en.md and bilingual guides remain unchanged.\n\n- Preview the six layers with visual/index.html, or run desktop/CodexObservatory.exe for the separate display. The display needs the installed Microsoft Edge WebView2 Runtime. No full browser is bundled.\n- To integrate with a backend, review display-host.example.json and docs/INTEGRATION.en.md, replace both example paths with canonical local absolute paths, select enabled=true, and save the result as display-host.json beside the backend. The included hash belongs to this archive's display executable. Recreate that configuration after moving the archive.\n- An explicitly supplied --instance argument connects the display to the chosen backend instance. The native host keeps the access token outside the web page. No instance or credentials are included here.\n- The watcher and compatibility bridge are supplied for complete source parity. Their configuration and follow-mode setup are explicit operations described in the integration guide; this packaging step installs no tasks and launches no program.\n- Source and build instructions: https://github.com/amanter-Zthen/CodexObservatory and docs/BUILD.en.md. Rebuilding requires the source checkout; this archive contains runtime files. See ACKNOWLEDGMENTS.md, THIRD_PARTY_NOTICES.md and licenses/ for upstream contributions.\n\nmanifest.json records all payload files. SHA256SUMS.txt covers those files plus manifest.json; neither file hashes itself. The package checks current build records, isolated overlay tests, PE format and source locks. It does not claim a live connection or visual GPU acceptance on the recipient's computer.\n`);

  // Recheck source records after copying; a concurrent edit invalidates this package.
  release.verifyNative(root);
  release.checkHashes(root, backend.sourceHashes, 'Backend build sources');
  release.checkHashes(root, desktop.sourceHashes, 'Desktop build sources');
  release.checkHashes(root, visual.hashes, 'Visual source lock');
  for (const input of copies.values()) if (release.hashFile(release.regularFile(root, input.source)) !== input.sha256) throw new Error('A package input changed while assembling');
  manifest.files.sort((a, b) => a.path < b.path ? -1 : a.path > b.path ? 1 : 0);
  manifest.payloadBytes = manifest.files.reduce((sum, file) => sum + file.bytes, 0);
  const manifestData = JSON.stringify(manifest, null, 2) + '\n';
  fs.writeFileSync(path.join(output, 'manifest.json'), manifestData, { flag: 'wx' });
  const sums = [...manifest.files.map(file => ({ path: file.path, sha256: file.sha256 })), { path: 'manifest.json', sha256: release.sha256(manifestData) }].sort((a, b) => a.path < b.path ? -1 : a.path > b.path ? 1 : 0);
  fs.writeFileSync(path.join(output, 'SHA256SUMS.txt'), sums.map(file => file.sha256 + '  ' + file.path).join('\n') + '\n', { flag: 'wx' });
  for (const file of manifest.files) if (release.hashFile(release.regularFile(output, file.path)) !== file.sha256) throw new Error('Output verification failed: ' + file.path);
  console.log(JSON.stringify({ result: 'PASS', directory: outRelative, payloadFiles: manifest.files.length, filesIncludingManifests: manifest.files.length + 2, payloadBytes: manifest.payloadBytes, manifestSHA256: release.sha256(manifestData) }, null, 2));
}

try { main(); }
catch (error) { console.error('Release packaging failed: ' + error.message); process.exitCode = 1; }
