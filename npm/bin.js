#!/usr/bin/env node
/**
 * readignore npm 壳包 — bin 入口
 *
 * spawn postinstall 下载好的 Go 二进制（npm/bin/readignore[.exe]），
 * 透传 argv / stdin / stdout / stderr / exit code。
 *
 * 若 binary 不存在（postinstall 未跑 / 失败），打印清晰报错并提示重新安装。
 */

'use strict';

const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');

const binName = process.platform === 'win32' ? 'readignore.exe' : 'readignore';
const binPath = path.join(__dirname, 'bin', binName);

if (!fs.existsSync(binPath)) {
  console.error('[readignore] Binary not found at: ' + binPath);
  console.error('');
  console.error(
    '  The platform binary was not installed. This usually means postinstall did not run or failed.',
  );
  console.error('  Re-install the package:');
  console.error('    npm install readignore');
  console.error('  or, if installed globally:');
  console.error('    npm install -g readignore');
  console.error('');
  console.error(
    '  If the GitHub Release is still a DRAFT, ask the maintainer to publish it first.',
  );
  process.exit(127); // 127 = command not found (POSIX 惯例)
}

// stdio: 'inherit' 让子进程直接接管终端（stdin/stdout/stderr 全透传，
// 含 TTY 颜色、Ctrl+C 信号）。child 退出码原样透传给父进程。
const child = spawn(binPath, process.argv.slice(2), { stdio: 'inherit' });

child.on('error', (err) => {
  // 常见：EACCES（无执行权限）/ ENOENT（路径错）/ E2BIG（参数过长）。
  console.error(`[readignore] Failed to spawn binary: ${err.message}`);
  console.error(`  binary: ${binPath}`);
  if (err.code === 'EACCES' && process.platform !== 'win32') {
    console.error('  Try: chmod +x "' + binPath + '"');
  }
  process.exit(126); // 126 = command found but not executable (POSIX 惯例)
});

child.on('exit', (code, signal) => {
  if (signal) {
    // 子进程被信号终止：以 128 + signal 退出（POSIX 惯例）。
    process.exit(128 + (signal === 'SIGINT' ? 2 : 1));
  } else {
    process.exit(code ?? 0);
  }
});
