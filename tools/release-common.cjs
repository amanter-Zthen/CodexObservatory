'use strict';
// Shared, dependency-free checks for the public build and release scripts.
const fs = require('fs');
const path = require('path');
const crypto = require('crypto');

const sha256 = data => crypto.createHash('sha256').update(data).digest('hex');
const hashFile = file => sha256(fs.readFileSync(file));
const validHash = value => typeof value === 'string' && /^[a-fA-F0-9]{64}$/.test(value);

function relativePath(value) {
  if (typeof value !== 'string' || !value || /[\\:\0\r\n<>"|?*]/.test(value) || value.startsWith('/')) {
    throw new Error('Expected a portable repository-relative path');
  }
  if (value.split('/').some(part => !part || part === '.' || part === '..' || /[. ]$/.test(part))) {
    throw new Error('Unsafe relative path: ' + value);
  }
  return value;
}

function projectPath(root, relative, allowMissing = false) {
  relativePath(relative);
  const base = path.resolve(root);
  let current = base;
  for (const part of relative.split('/')) {
    current = path.join(current, part);
    if (!fs.existsSync(current)) {
      if (allowMissing) continue;
      throw new Error('Required file or directory is missing: ' + relative);
    }
    if (fs.lstatSync(current).isSymbolicLink()) throw new Error('Links are not release inputs: ' + relative);
  }
  if (!current.startsWith(base + path.sep)) throw new Error('Path escaped the project');
  return current;
}

function regularFile(root, relative) {
  const file = projectPath(root, relative);
  if (!fs.statSync(file).isFile()) throw new Error('Expected a regular file: ' + relative);
  return file;
}

function readJSON(root, relative) {
  const file = regularFile(root, relative);
  if (fs.statSync(file).size > 16 * 1024 * 1024) throw new Error('JSON input is unexpectedly large: ' + relative);
  return JSON.parse(fs.readFileSync(file, 'utf8').replace(/^\uFEFF/, ''));
}

function walk(root, relative) {
  const directory = projectPath(root, relative);
  if (!fs.statSync(directory).isDirectory()) throw new Error('Expected a directory: ' + relative);
  const result = [];
  for (const entry of fs.readdirSync(directory, { withFileTypes: true }).sort((a, b) => a.name < b.name ? -1 : a.name > b.name ? 1 : 0)) {
    const name = relative + '/' + entry.name;
    if (entry.isSymbolicLink()) throw new Error('Links are not release inputs: ' + name);
    if (entry.isDirectory()) result.push(...walk(root, name));
    else if (entry.isFile()) result.push(name);
    else throw new Error('Unsupported source entry: ' + name);
  }
  return result;
}

function hashFiles(root, files) {
  return Object.fromEntries([...files].sort().map(file => [relativePath(file), hashFile(regularFile(root, file))]));
}

function checkHashes(root, hashes, label = 'Source') {
  if (!hashes || typeof hashes !== 'object' || Array.isArray(hashes) || !Object.keys(hashes).length) {
    throw new Error(label + ' hash record is empty or invalid');
  }
  for (const [relative, expected] of Object.entries(hashes)) {
    if (!validHash(expected) || hashFile(regularFile(root, relative)) !== expected.toLowerCase()) {
      throw new Error(label + ' hash mismatch: ' + relative);
    }
  }
}

function verifyNative(root) {
  const manifestPath = 'deployment/native-source-manifest.json';
  const manifest = readJSON(root, manifestPath);
  if (!Array.isArray(manifest.files) || !manifest.files.length) throw new Error('Native source manifest must contain files');
  const hashes = {}, folded = new Set();
  for (const item of manifest.files) {
    if (!item || typeof item !== 'object' || !validHash(item.sha256)) throw new Error('Invalid native manifest entry');
    const relative = relativePath(item.path);
    if (!relative.startsWith('native/')) throw new Error('Native manifest entry is outside native/: ' + relative);
    if (folded.has(relative.toLowerCase())) throw new Error('Duplicate native manifest path: ' + relative);
    folded.add(relative.toLowerCase());
    hashes[relative] = item.sha256.toLowerCase();
  }
  const actual = walk(root, 'native').sort(), expected = Object.keys(hashes).sort();
  if (JSON.stringify(actual) !== JSON.stringify(expected)) {
    const expectedSet = new Set(expected), actualSet = new Set(actual);
    const extra = actual.filter(file => !expectedSet.has(file));
    const missing = expected.filter(file => !actualSet.has(file));
    throw new Error('Native manifest is not an exact tree inventory; extra=' + extra.slice(0, 12).join(', ') + '; missing=' + missing.slice(0, 12).join(', '));
  }
  checkHashes(root, hashes, 'Native source');
  return { hashes: Object.fromEntries(expected.map(file => [file, hashes[file]])), files: expected.length, manifestPath, manifestSHA256: hashFile(regularFile(root, manifestPath)) };
}

function verifyVisual(root) {
  const lock = readJSON(root, 'layers.lock.json');
  if (!Array.isArray(lock.layers) || lock.layers.length !== 6 || !validHash(lock.previewHtmlSha256)) {
    throw new Error('The visual lock must describe six layers and the preview HTML hash');
  }
  if (!lock.runtimeHashes || typeof lock.runtimeHashes !== 'object' || Array.isArray(lock.runtimeHashes)) throw new Error('Visual runtimeHashes are missing');
  const runtime = Object.keys(lock.runtimeHashes).sort();
  if (!runtime.length) throw new Error('Visual runtimeHashes are empty');
  const actual = ['visual/engine', 'visual/background'].flatMap(dir => walk(root, dir)).filter(file => file.endsWith('.js')).map(file => file.slice(7)).sort();
  if (JSON.stringify(runtime) !== JSON.stringify(actual)) throw new Error('Visual JavaScript inventory differs from layers.lock.json');
  const hashes = { 'layers.lock.json': hashFile(regularFile(root, 'layers.lock.json')), 'visual/index.html': lock.previewHtmlSha256 };
  for (const file of runtime) {
    relativePath(file);
    hashes['visual/' + file] = lock.runtimeHashes[file];
  }
  for (const layer of lock.layers) {
    if (!Array.isArray(layer.files) || !layer.files.length || layer.files.some(file => !runtime.includes(file))) {
      throw new Error('A layer references an unlocked runtime file');
    }
  }
  checkHashes(root, hashes, 'Visual source lock');
  const html = fs.readFileSync(regularFile(root, 'visual/index.html'), 'utf8');
  const scripts = [...html.matchAll(/<script\b[^>]*\bsrc\s*=\s*["']([^"']+)["'][^>]*>/gi)].map(match => relativePath(match[1]));
  if (new Set(scripts).size !== scripts.length || JSON.stringify([...scripts].sort()) !== JSON.stringify(runtime)) {
    throw new Error('The HTML script list must match every locked runtime script exactly once');
  }
  return { lock, hashes, scripts: runtime.length };
}

function inspectPE(file, expectedSubsystem) {
  const data = fs.readFileSync(file);
  if (data.length < 64 || data.readUInt16LE(0) !== 0x5a4d) throw new Error('Expected a Windows PE image');
  const offset = data.readUInt32LE(0x3c);
  if (offset > data.length - 96 || data.readUInt32LE(offset) !== 0x4550 || data.readUInt16LE(offset + 4) !== 0x8664 || data.readUInt16LE(offset + 24) !== 0x20b) {
    throw new Error('Expected an AMD64 PE32+ image');
  }
  const subsystem = data.readUInt16LE(offset + 24 + 68);
  if (expectedSubsystem !== undefined && subsystem !== expectedSubsystem) throw new Error('Unexpected Windows executable subsystem');
  return { bytes: data.length, sha256: sha256(data), architecture: 'amd64', subsystem };
}

module.exports = { sha256, hashFile, validHash, relativePath, projectPath, regularFile, readJSON, walk, hashFiles, checkHashes, verifyNative, verifyVisual, inspectPE };
