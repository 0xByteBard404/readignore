#!/bin/sh
# install.sh — one-liner installer for readignore
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/0xByteBard404/readignore/main/install.sh | sh
#   or:  wget -qO- https://raw.githubusercontent.com/0xByteBard404/readignore/main/install.sh | sh
#
# What it does:
#   1. Detects OS (linux/darwin) + arch (amd64/arm64).
#   2. Queries the GitHub API for the latest release tag + asset URLs.
#   3. Downloads the matching goreleaser archive + checksums.txt.
#   4. Verifies the archive's SHA256 against checksums.txt.
#   5. Extracts the `readignore` binary into /usr/local/bin (or ~/.local/bin).
#   6. Runs `readignore --version` to confirm.
#
# Windows / MINGW is NOT supported here — use npm, Scoop, or download the .zip
# binary from the release page: https://github.com/0xByteBard404/readignore/releases
#
# Pure POSIX sh, zero deps beyond curl/wget + tar + sha256sum (all standard).

set -eu

OWNER="0xByteBard404"
REPO="readignore"
REPO_URL="https://github.com/${OWNER}/${REPO}"
RELEASES_API="https://api.github.com/repos/${OWNER}/${REPO}/releases/latest"

# --- helpers ---------------------------------------------------------------

err() {
	# Print to stderr, prefixed. Interprets \n escapes so multi-line messages
	# render correctly. Does not exit.
	# shellcheck disable=SC2059  # \n is intentional in caller strings
	printf 'readignore: %b\n' "$*" >&2
}

die() {
	# Print error and exit 1. Multi-arg: join with space (printf %b handles \n).
	err "$*"
	exit 1
}

note() {
	# shellcheck disable=SC2059
	printf 'readignore: %b\n' "$*"
}

have() {
	# True if the given command is on PATH.
	command -v "$1" >/dev/null 2>&1
}

# --- preflight: required tools --------------------------------------------

if ! have curl && ! have wget; then
	die "neither curl nor wget found; install one to proceed."
fi
have tar || die "tar not found; required to extract the archive."

# --- OS + arch detection ---------------------------------------------------

detect_os() {
	os="$(uname -s)"
	case "$os" in
		Linux*)  echo "linux" ;;
		Darwin*) echo "darwin" ;;
		MINGW*|MSYS*|CYGWIN*)
			die "Windows/MINGW detected. This installer targets unix.\n" \
"      Use one of:\n" \
"        - npm:    npm install -g @caixuetang/readignore\n" \
"        - Scoop:  (scoop bucket coming soon)\n" \
"        - binary: download the .zip from ${REPO_URL}/releases/latest"
			;;
		*) die "unsupported OS: '$os'. See ${REPO_URL}/releases/latest for binaries." ;;
	esac
}

detect_arch() {
	arch="$(uname -m)"
	case "$arch" in
		x86_64|amd64)  echo "amd64" ;;
		aarch64|arm64) echo "arm64" ;;
		# 32-bit + other RISC intentionally unsupported (no goreleaser build).
		*) die "unsupported arch: '$arch'. Only amd64 and arm64 are built." ;;
	esac
}

# download <url> <dest>: pick curl or wget, write to file.
download() {
	_url="$1"; _dest="$2"
	if have curl; then
		curl -fsSL "$_url" -o "$_dest"
	else
		wget -qO "$_dest" "$_url"
	fi
}

# fetch_text <url>: print body to stdout (used for the GitHub API JSON).
fetch_text() {
	if have curl; then
		curl -fsSL "$1"
	else
		wget -qO- "$1"
	fi
}

# --- main ------------------------------------------------------------------

GOOS="$(detect_os)"
GOARCH="$(detect_arch)"
note "detected platform: ${GOOS}/${GOARCH}"

# 1. Latest release: tag + asset URL via the GitHub API.
note "fetching latest release from the GitHub API..."
# jq is not assumed present — parse the JSON with sed/grep (single-line enough
# for goreleaser payloads; we look for the archive + checksums.txt assets).
JSON="$(fetch_text "$RELEASES_API")" \
	|| die "failed to query GitHub API for latest release."

TAG_NAME="$(printf '%s\n' "$JSON" | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
[ -n "$TAG_NAME" ] || die "could not parse tag_name from release API response."
note "latest release: ${TAG_NAME}"

# Strip a leading 'v' (tag v0.3.0 -> version 0.3.0) for the asset filename.
VER="${TAG_NAME#v}"
ARCHIVE_NAME="readignore_${VER}_${GOOS}_${GOARCH}.tar.gz"
CHECKSUMS_NAME="checksums.txt"

# Find the browser_download_url for our archive + checksums.txt.
extract_asset_url() {
	_pattern="\"name\":[[:space:]]*\"$1\""
	printf '%s\n' "$JSON" \
		| awk -v want="$1" '
			/"browser_download_url":[[:space:]]*"/ {
				url = $0
				sub(/.*"browser_download_url":[[:space:]]*"/, "", url)
				sub(/".*/, "", url)
				last_url = url
			}
			/"name":[[:space:]]*"/ {
				name = $0
				sub(/.*"name":[[:space:]]*"/, "", name)
				sub(/".*/, "", name)
				if (name == want && last_url != "") { print last_url; exit }
			}
		'
}

ARCHIVE_URL="$(extract_asset_url "$ARCHIVE_NAME")"
CHECKSUMS_URL="$(extract_asset_url "$CHECKSUMS_NAME")"

if [ -z "$ARCHIVE_URL" ]; then
	die "no asset named '${ARCHIVE_NAME}' in release ${TAG_NAME}.\n" \
	    "      Your platform (${GOOS}/${GOARCH}) may have no build yet.\n" \
	    "      See ${REPO_URL}/releases/tag/${TAG_NAME}"
fi
note "archive:    ${ARCHIVE_NAME}"
[ -n "$CHECKSUMS_URL" ] && note "checksums:  ${CHECKSUMS_NAME}"

# 2. Download to a temp dir.
TMPDIR="$(mktemp -d 2>/dev/null || mktemp -d -t readignore)"
trap 'rm -rf "$TMPDIR"' EXIT INT TERM

ARCHIVE_PATH="${TMPDIR}/${ARCHIVE_NAME}"
note "downloading archive..."
download "$ARCHIVE_URL" "$ARCHIVE_PATH" \
	|| die "archive download failed: ${ARCHIVE_URL}"

# 3. SHA256 verification (skip with a warning only if checksums.txt is absent —
#    e.g. draft release). A verification FAILURE, however, is fatal.
verify_sha256() {
	[ -n "$CHECKSUMS_URL" ] || return 2   # 2 = checksums unavailable

	CHECKSUMS_PATH="${TMPDIR}/${CHECKSUMS_NAME}"
	if ! download "$CHECKSUMS_URL" "$CHECKSUMS_PATH" 2>/dev/null; then
		return 2
	fi

	# Expected hash: the line in checksums.txt that ends with our archive name.
	EXPECTED="$(grep -E "[[:space:]]${ARCHIVE_NAME}\$" "$CHECKSUMS_PATH" \
		| awk '{print $1}' | head -n1)"
	[ -n "$EXPECTED" ] || return 2   # not listed -> can't verify

	# Actual hash (portable: sha256sum on Linux, shasum -a 256 on macOS/BSD).
	if have sha256sum; then
		ACTUAL="$(sha256sum "$ARCHIVE_PATH" | awk '{print $1}')"
	elif have shasum; then
		ACTUAL="$(shasum -a 256 "$ARCHIVE_PATH" | awk '{print $1}')"
	else
		err "no sha256sum / shasum found; cannot verify checksum."
		return 1
	fi

	if [ "$ACTUAL" != "$EXPECTED" ]; then
		err "SHA256 mismatch!"
		err "  expected: ${EXPECTED}"
		err "  actual:   ${ACTUAL}"
		return 1
	fi
	return 0
}

note "verifying SHA256..."
verify_sha256
_verify_rc=$?
case $_verify_rc in
	0) note "SHA256 OK." ;;
	2) err "WARNING: checksums.txt not available for ${TAG_NAME};" \
	       "skipping verification (download still proceeded)." ;;
	*) die "checksum verification failed; refusing to install." ;;
esac

# 4. Extract the binary.
note "extracting..."
tar -xzf "$ARCHIVE_PATH" -C "$TMPDIR"
[ -f "${TMPDIR}/readignore" ] \
	|| die "archive did not contain a top-level 'readignore' binary."

# 5. Choose install dir + copy.
INSTALL_BIN="/usr/local/bin/readignore"
if [ -w "/usr/local/bin" ]; then
	DEST="/usr/local/bin"
else
	DEST="${HOME}/.local/bin"
	mkdir -p "$DEST"
	# Already on PATH? If not, tell the user how to add it.
	case ":${PATH}:" in
		*":${DEST}:"*) ;;
		*)
			err "note: ${DEST} is not on your PATH."
			err "      Add it (e.g. to ~/.bashrc / ~/.zshrc):"
			err "        export PATH=\"${DEST}:\$PATH\""
			;;
	esac
fi
cp "${TMPDIR}/readignore" "${DEST}/readignore"
chmod 0755 "${DEST}/readignore"

# 6. Verify.
note "installed -> ${DEST}/readignore"
if "${DEST}/readignore" --version >/dev/null 2>&1; then
	"${DEST}/readignore" --version
else
	# May still fail if PATH deps missing; surface a soft warning, don't die.
	err "warning: 'readignore --version' failed; the binary may not run on this system."
fi

note "done. Try: readignore init"
