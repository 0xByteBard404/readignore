#!/usr/bin/env node
/**
 * readignore npm 壳包 — postinstall
 *
 * 从 GitHub Release 下载对应平台的 Go 二进制（goreleaser 产物），解压提取
 * `readignore` 可执行文件到包内 `bin/`，并赋予执行权限（非 windows）。
 *
 * 设计原则：
 *   - 零 npm 运行时依赖（仅用 Node 内置模块：fs/path/https/crypto/child_process）。
 *   - 解压复用系统 `tar`（linux / darwin / windows 10 1803+ 均自带，
 *     且 windows 的 tar 同时支持 .zip 与 .tar.gz —— bsdtar）。
 *   - 失败清晰报错（不静默），打印用户可操作的修复指引。
 *   - 可选 SHA256 校验（下载 checksums.txt 比对）。
 *
 * goreleaser archive 命名（见 .goreleaser.yml）：
 *   readignore_<version>_<os>_<arch>.tar.gz   (linux / darwin)
 *   readignore_<version>_<os>_<arch>.zip      (windows)
 *   archive 内含 binary `readignore`（windows 为 readignore.exe）+ README/LICENSE/CHANGELOG
 */

'use strict';

const fs = require('fs');
const path = require('path');
const https = require('https');
const crypto = require('crypto');
const { execFileSync } = require('child_process');

// --- 配置 -------------------------------------------------------------

const OWNER = '0xByteBard404';
const REPO = 'readignore';
// version 与 Go release 同步（与 package.json version 一致）。
const VERSION = '0.2.0';
const TAG = `v${VERSION}`;
const RELEASE_BASE = `https://github.com/${OWNER}/${REPO}/releases/download/${TAG}`;
const CHECKSUMS_URL = `${RELEASE_BASE}/checksums.txt`;

// 下载失败时的最大重试次数（指数退避）。
const MAX_RETRIES = 2;
const DOWNLOAD_TIMEOUT_MS = 60_000;
const MAX_REDIRECTS = 5;

// --- 平台映射 ---------------------------------------------------------

/**
 * 把 Node 的 process.platform / process.arch 映射到 goreleaser 的 os / arch 命名。
 * @returns {{goos: string, goarch: string, ext: string, binExt: string}}
 * @throws {Error} 平台不支持时抛出（含可操作指引）。
 */
function mapPlatform() {
  const platform = process.platform;
  const arch = process.arch;

  // os 映射：win32 -> windows，其余原样。
  const goos = platform === 'win32' ? 'windows' : platform; // linux | darwin | windows

  // arch 映射：x64 -> amd64，arm64 -> arm64。
  let goarch;
  if (arch === 'x64') goarch = 'amd64';
  else if (arch === 'arm64') goarch = 'arm64';
  else {
    throw new Error(
      `[readignore] Unsupported arch: ${arch}. ` +
        `Only x64 (amd64) and arm64 are supported. ` +
        `Available builds: ${RELEASE_BASE}`,
    );
  }

  // windows arm64 goreleaser 已 ignore（无主流工具链需求）。
  if (goos === 'windows' && goarch === 'arm64') {
    throw new Error(
      '[readignore] Windows arm64 build is not provided by goreleaser. ' +
        'Please use a Windows x64 (amd64) environment. ' +
        `See release assets: https://github.com/${OWNER}/${REPO}/releases/tag/${TAG}`,
    );
  }

  // archive 扩展：linux/darwin tar.gz，windows zip。
  const ext = goos === 'windows' ? 'zip' : 'tar.gz';
  // binary 扩展：仅 windows 加 .exe。
  const binExt = goos === 'windows' ? '.exe' : '';

  return { goos, goarch, ext, binExt };
}

// --- 下载（含手动重定向跟随）-----------------------------------------

/**
 * GET 一个 URL，返回其响应流。手动跟随 3xx 重定向（Node https.get 不自动跟随）。
 * @returns {Promise<import('http').IncomingMessage>}
 */
function fetchFollowingRedirects(url, redirectsLeft = MAX_REDIRECTS) {
  return new Promise((resolve, reject) => {
    const req = https.get(
      url,
      { headers: { 'User-Agent': 'readignore-npm-postinstall' } },
      (res) => {
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          res.resume(); // 释放当前响应体
          if (redirectsLeft <= 0) {
            reject(
              new Error(
                `[readignore] Too many redirects while downloading ${url}`,
              ),
            );
            return;
          }
          // relative redirect → resolve against current url
          const next = new URL(res.headers.location, url).href;
          resolve(fetchFollowingRedirects(next, redirectsLeft - 1));
          return;
        }
        resolve(res);
      },
    );
    req.setTimeout(DOWNLOAD_TIMEOUT_MS, () => {
      req.destroy(new Error(`[readignore] Download timed out: ${url}`));
    });
    req.on('error', reject);
  });
}

/**
 * 下载 URL 到本地文件（带重试 + 进度提示）。失败抛出含 HTTP 状态码与 URL 的错误。
 * @returns {Promise<void>}
 */
async function downloadToFile(url, dest, attempt = 0) {
  const tmp = `${dest}.tmp`;
  try {
    const res = await fetchFollowingRedirects(url);
    if (res.statusCode !== 200) {
      res.resume();
      throw new Error(
        `[readignore] Download failed: HTTP ${res.statusCode} for ${url}` +
          (res.statusCode === 404
            ? '\n  Release may be a DRAFT (not yet published) or the version tag does not exist. ' +
              'Ask the maintainer to publish the GitHub Release, then re-run `npm install readignore`.'
            : ''),
      );
    }
    const total = parseInt(res.headers['content-length'] || '0', 10);
    let received = 0;
    await new Promise((resolve, reject) => {
      const out = fs.createWriteStream(tmp);
      res.on('data', (chunk) => {
        received += chunk.length;
        if (total && received % (1024 * 256) < chunk.length) {
          process.stdout.write(
            `\r[readignore] downloading... ${Math.round((received / total) * 100)}%`,
          );
        }
      });
      res.pipe(out);
      out.on('finish', () => out.close(() => {
        fs.renameSync(tmp, dest);
        process.stdout.write('\n');
        resolve();
      }));
      out.on('error', (err) => {
        try { fs.existsSync(tmp) && fs.unlinkSync(tmp); } catch { /* noop */ }
        reject(err);
      });
      res.on('error', (err) => {
        try { out.destroy(); fs.existsSync(tmp) && fs.unlinkSync(tmp); } catch { /* noop */ }
        reject(err);
      });
    });
  } catch (err) {
    if (fs.existsSync(tmp)) { try { fs.unlinkSync(tmp); } catch { /* noop */ } }
    if (attempt < MAX_RETRIES) {
      const wait = 500 * Math.pow(2, attempt);
      console.warn(
        `\n[readignore] download error (${err.message}), retrying in ${wait}ms...`,
      );
      await new Promise((r) => setTimeout(r, wait));
      return downloadToFile(url, dest, attempt + 1);
    }
    throw err;
  }
}

// --- 解压 --------------------------------------------------------------

/**
 * 用系统 `tar` 解压 archive 到目标目录。
 * tar 在 linux / darwin / windows 10 1803+ 均自带，且 windows 的 tar
 * 同时支持 .tar.gz 与 .zip（bsdtar 自动识别格式）。
 *
 * @param {string} archive  archive 文件路径
 * @param {string} ext      'tar.gz' | 'zip'
 * @param {string} destDir  解压目标目录
 */
function extractArchive(archive, ext, destDir) {
  const tarBin = process.platform === 'win32' ? 'tar.exe' : 'tar';
  // tar -xzf <archive> -C <destDir>   (tar.gz)
  // tar -xf  <archive> -C <destDir>   (zip, bsdtar 自动识别)
  const args = ext === 'tar.gz'
    ? ['-xzf', archive, '-C', destDir]
    : ['-xf', archive, '-C', destDir];
  try {
    execFileSync(tarBin, args, { stdio: ['ignore', 'pipe', 'pipe'] });
  } catch (err) {
    const stderr = err.stderr ? err.stderr.toString().trim() : err.message;
    throw new Error(
      `[readignore] Failed to extract ${archive} via system \`tar\`.\n` +
        `  tar stderr: ${stderr}\n` +
        `  Ensure \`tar\` is installed (Windows 10 1803+ ships tar.exe; ` +
        `linux/darwin ship tar by default).`,
    );
  }
}

// --- checksum 校验（可选，提升安全性）---------------------------------

/**
 * 下载 checksums.txt，提取本平台 archive 的期望 SHA256，与本地文件比对。
 * - checksums.txt 不可用（draft release / 网络问题）→ 仅警告，返回 false，不抛。
 * - 校验不匹配 → 抛错（中止安装）。
 * @returns {Promise<boolean>} 校验是否通过
 */
async function verifyChecksum(archivePath, archiveBasename) {
  let checksumsText;
  const tmpChecksum = path.join(path.dirname(archivePath), 'checksums.txt.tmp');
  try {
    await downloadToFile(CHECKSUMS_URL, tmpChecksum);
    checksumsText = fs.readFileSync(tmpChecksum, 'utf8');
  } catch (err) {
    console.warn(
      `[readignore] checksums.txt unavailable, skipping SHA256 verification: ${err.message}`,
    );
    return false;
  } finally {
    if (fs.existsSync(tmpChecksum)) { try { fs.unlinkSync(tmpChecksum); } catch { /* noop */ } }
  }

  const lines = checksumsText.split(/\r?\n/);
  let expected = null;
  for (const line of lines) {
    const m = line.trim().match(/^([0-9a-fA-F]{64})\s+\*?(.+)$/);
    if (m && path.basename(m[2].trim()) === archiveBasename) {
      expected = m[1].toLowerCase();
      break;
    }
  }
  if (!expected) {
    console.warn(
      `[readignore] ${archiveBasename} not found in checksums.txt, skipping verification.`,
    );
    return false;
  }

  const actual = sha256File(archivePath);
  if (actual !== expected) {
    throw new Error(
      `[readignore] SHA256 mismatch for ${archiveBasename}.\n` +
        `  expected: ${expected}\n` +
        `  actual:   ${actual}\n` +
        `  The downloaded archive may be corrupted or tampered. ` +
        `Remove ${archivePath} and re-run \`npm install\`.`,
    );
  }
  return true;
}

function sha256File(filePath) {
  const h = crypto.createHash('sha256');
  h.update(fs.readFileSync(filePath));
  return h.digest('hex');
}

// --- 主流程 ------------------------------------------------------------

async function main() {
  const { goos, goarch, ext, binExt } = mapPlatform();

  const archiveBasename = `readignore_${VERSION}_${goos}_${goarch}.${ext}`;
  const archiveUrl = `${RELEASE_BASE}/${archiveBasename}`;

  // 包内目录布局：npm/bin/readignore[.exe]
  const pkgDir = __dirname;
  const binDir = path.join(pkgDir, 'bin');
  const binName = `readignore${binExt}`;
  const binPath = path.join(binDir, binName);

  // 解压工作目录（用临时目录，避免旧文件残留干扰）。
  const workDir = path.join(binDir, '_extract');
  const archivePath = path.join(workDir, archiveBasename);

  console.log(`[readignore] installing readignore ${VERSION} for ${goos}/${goarch}`);

  fs.mkdirSync(workDir, { recursive: true });

  try {
    // 1. 下载 archive
    console.log(`[readignore] downloading: ${archiveUrl}`);
    await downloadToFile(archiveUrl, archivePath);

    // 2. SHA256 校验（可选；draft release 无 checksums.txt 时跳过）
    const ok = await verifyChecksum(archivePath, archiveBasename);
    if (ok) console.log('[readignore] SHA256 verified');

    // 3. 解压
    console.log(`[readignore] extracting ${archiveBasename} ...`);
    extractArchive(archivePath, ext, workDir);

    // 4. 定位解压出的 binary 并移动到 binDir/readignore[.exe]
    const extractedBin = path.join(workDir, binName);
    if (!fs.existsSync(extractedBin)) {
      const contents = fs.readdirSync(workDir);
      throw new Error(
        `[readignore] binary "${binName}" not found in extracted archive.\n` +
          `  archive contents: ${contents.join(', ')}\n` +
          `  This may indicate the archive layout changed; please report.`,
      );
    }

    fs.mkdirSync(binDir, { recursive: true });
    if (fs.existsSync(binPath)) fs.unlinkSync(binPath);
    fs.renameSync(extractedBin, binPath);

    // 5. chmod +x（非 windows）
    if (goos !== 'windows') {
      fs.chmodSync(binPath, 0o755);
    }
  } finally {
    // 6. 无论成功/失败，都清理解压工作目录（含 archive、checksums.tmp、解压残留）。
    try {
      fs.rmSync(workDir, { recursive: true, force: true });
    } catch {
      /* 清理失败不影响安装结果 */
    }
  }

  console.log(`[readignore] installed to ${binPath}`);
  console.log('[readignore] run: npx readignore --version');
}

main().catch((err) => {
  console.error(err && err.message ? err.message : String(err));
  console.error('');
  console.error('[readignore] postinstall failed.');
  console.error(
    '  If the GitHub Release is a DRAFT, ask the maintainer to publish it, then re-run `npm install`.',
  );
  console.error(`  Release: https://github.com/${OWNER}/${REPO}/releases/tag/${TAG}`);
  process.exit(1);
});
