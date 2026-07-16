#!/usr/bin/env node
/**
 * readignore npm 包 bin 入口。
 *
 * 按当前平台选**包内**的 prebuilt Go binary（bin/<platform>/readignore[.exe]），
 * spawn 并透传 argv / stdin / stdout / stderr / exit code。
 *
 * binary 随包发布（npm publish 时含），**无 postinstall、无下载** —— allow-scripts
 * 完全无关，默认配置 100% 可靠。
 */
'use strict';

const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');

// platform/arch → 包内 binary 目录名。pure，可单测。
function resolveBinaryPath(platform, arch) {
  const map = {
    'linux-x64': 'linux-x64',
    'linux-arm64': 'linux-arm64',
    'darwin-x64': 'darwin-x64',
    'darwin-arm64': 'darwin-arm64',
    'win32-x64': 'windows-x64',
  };
  const dir = map[`${platform}-${arch}`];
  if (!dir) {
    throw new Error(
      `[readignore] Unsupported platform: ${platform}/${arch}. ` +
        `Supported: linux (x64, arm64), darwin (x64, arm64), windows (x64).`,
    );
  }
  const binName = platform === 'win32' ? 'readignore.exe' : 'readignore';
  return path.join(__dirname, 'bin', dir, binName);
}

function main() {
  let binPath;
  try {
    binPath = resolveBinaryPath(process.platform, process.arch);
  } catch (err) {
    console.error(err.message);
    process.exit(127);
    return;
  }
  if (!fs.existsSync(binPath)) {
    console.error(`[readignore] Binary not found at: ${binPath}`);
    console.error('  The package may be corrupted or incompletely installed. Re-install:');
    console.error('    npm install readignore');
    process.exit(127);
    return;
  }
  const child = spawn(binPath, process.argv.slice(2), { stdio: 'inherit' });
  child.on('error', (err) => {
    console.error(`[readignore] Failed to spawn binary: ${err.message}`);
    if (err.code === 'EACCES' && process.platform !== 'win32') {
      console.error(`  Try: chmod +x "${binPath}"`);
    }
    process.exit(126);
  });
  child.on('exit', (code, signal) => {
    if (signal) process.exit(128 + (signal === 'SIGINT' ? 2 : 1));
    else process.exit(code ?? 0);
  });
}

if (require.main === module) main();

module.exports = { resolveBinaryPath };
