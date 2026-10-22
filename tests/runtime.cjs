'use strict';
const fs = require('fs'), path = require('path'), os = require('os');
const candidates = [
  process.env.OBSERVATORY_PLAYWRIGHT,
  'playwright',
  path.join(os.homedir(), '.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright')
].filter(Boolean);
let playwright;
for (const location of candidates) {
  try { playwright = require(location); break; } catch (error) {
    if (error.code !== 'MODULE_NOT_FOUND') throw error;
  }
}
if (!playwright) throw Error('Set OBSERVATORY_PLAYWRIGHT to an installed Playwright package, or install Playwright for this Node runtime.');
const chromePath = [
  process.env.OBSERVATORY_CHROME,
  'C:/Program Files/Google/Chrome/Application/chrome.exe',
  'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
  '/usr/bin/google-chrome', '/usr/bin/chromium'
].find(file => file && fs.existsSync(file));
// Undefined executablePath lets Playwright use its own installed browser.
module.exports = {...playwright, chromePath};
