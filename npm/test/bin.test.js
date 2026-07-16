const test = require('node:test');
const assert = require('node:assert');
const path = require('path');
const { resolveBinaryPath } = require('../bin.js');

test('resolveBinaryPath: 5 supported platforms', () => {
  assert.strictEqual(
    resolveBinaryPath('linux', 'x64'),
    path.join(__dirname, '..', 'bin', 'linux-x64', 'readignore'),
  );
  assert.strictEqual(
    resolveBinaryPath('linux', 'arm64'),
    path.join(__dirname, '..', 'bin', 'linux-arm64', 'readignore'),
  );
  assert.strictEqual(
    resolveBinaryPath('darwin', 'x64'),
    path.join(__dirname, '..', 'bin', 'darwin-x64', 'readignore'),
  );
  assert.strictEqual(
    resolveBinaryPath('darwin', 'arm64'),
    path.join(__dirname, '..', 'bin', 'darwin-arm64', 'readignore'),
  );
  assert.strictEqual(
    resolveBinaryPath('win32', 'x64'),
    path.join(__dirname, '..', 'bin', 'windows-x64', 'readignore.exe'),
  );
});

test('resolveBinaryPath: unsupported throws', () => {
  assert.throws(() => resolveBinaryPath('win32', 'arm64'), /Unsupported platform: win32\/arm64/);
  assert.throws(() => resolveBinaryPath('freebsd', 'x64'), /Unsupported platform: freebsd\/x64/);
});
