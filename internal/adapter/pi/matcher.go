package pi

// tsMatcherBody 是 gitignore 匹配引擎的 JS 实现（纯 ES，无 TS 类型注解），
// 被 text/template 拼进生成的 readignore.ts（外层模板提供类型注解，如
// `const PATTERNS: readonly string[] = ...`；本体本身不含注解）。
//
// 之所以特意写成**纯 JS（无注解）**而非带类型注解的 TS：使生成的 .ts 与
// node 测试 harness 共享同一字符串——node 跑 .mjs 不接受 TS 注解，若本体含注解，
// harness 必须额外剥注解才能跑；纯 JS 双向兼容（TS 把它当合法表达式，node 直接跑），
// 无需任何转换。
//
// 匹配语义（与 internal/adapter/shared/hookengine 的 py 引擎对齐，I-1/I-2/M-1 修复同源）：
//   - `**/`  → 任意层级目录前缀（含零层）：编译成 (?:.*/)? ；
//   - `**`   → 跨任意层级（含零层）：.* ；
//   - `*`    → 单层内任意非 / 字符：[^/]* ；
//   - `?`    → 单个非 / 字符：[^/] ；
//   - `[...]`/`[!...]` → 字符类（透传为正则字符类，[!abc] 取反 → [^abc]）；
//   - 尾 `/` → 仅匹配目录（运行时无 stat，候选补尾 / 仍能命中）；
//   - 前导 `/` 或含内部 `/` → 根锚定（^ 开头）；无 `/` → basename（等价 **/<glob>）；
//   - 取反 (`!`)：按文件顺序求值，最后一条命中者决定结果（与 go-git/py 一致）。
//
// 进程契约：isBlocked(path) → true=应拦截（命中 DENY 规则），false=放行。
const tsMatcherBody = `// --- readignore matcher: hand-written gitignore engine, zero npm deps. ---
// globToRegex: convert a single gitignore glob (leading ! and trailing / stripped
// by caller) into { regex, anchored }.
function globToRegex(glob) {
	let anchored = false;
	if (glob.startsWith("/")) {
		anchored = true;
		glob = glob.slice(1);
	} else if (glob.indexOf("/") >= 0) {
		// contains an internal slash -> root-anchored
		anchored = true;
	}
	// basename pattern (no slash after stripping) -> behaves like **/<glob>
	if (!anchored && !glob.startsWith("**/")) {
		glob = "**/" + glob;
	}
	let out = "";
	let i = 0;
	const n = glob.length;
	while (i < n) {
		const c = glob.charAt(i);
		if (c === "*") {
			if (i + 1 < n && glob.charAt(i + 1) === "*") {
				// **/  -> any-depth dir prefix (incl. zero)
				if (i + 2 < n && glob.charAt(i + 2) === "/") {
					out += "(?:.*/)?";
					i += 3;
					continue;
				}
				// bare ** -> cross layers
				out += ".*";
				i += 2;
				continue;
			}
			// single * -> within one segment
			out += "[^/]*";
			i += 1;
			continue;
		}
		if (c === "?") {
			out += "[^/]";
			i += 1;
			continue;
		}
		if (c === "[") {
			// character class: find closing ]. [^...] or [!...] negate.
			let j = i + 1;
			let negate = false;
			if (j < n && (glob.charAt(j) === "!" || glob.charAt(j) === "^")) {
				negate = true;
				j += 1;
			}
			if (j < n && glob.charAt(j) === "]") {
				// literal ] right after [ / [^ (POSIX)
				j += 1;
			}
			const close = glob.indexOf("]", j);
			if (close === -1) {
				// unclosed [ -> literal
				out += "\\[";
				i += 1;
				continue;
			}
			const body = glob.slice(i + 1, close);
			out += "[" + (negate ? "^" : "") + body + "]";
			i = close + 1;
			continue;
		}
		// escape regex metachar
		out += escapeRe(c);
		i += 1;
	}
	let full;
	if (anchored) {
		full = "^" + out + "(?:/|$)";
	} else {
		full = "(?:^|/)" + out + "(?:/|$)";
	}
	return { regex: new RegExp(full), anchored: anchored };
}

function escapeRe(c) {
	// Regex metacharacters that need escaping when they reach here as a literal
	// single char. The chars *, ?, [ are consumed by the switch above (never
	// reach here). The chars ], {, }, | are accepted literally by JS regex when
	// they appear alone (no matching opener), so listing them is harmless but
	// unnecessary; this list keeps them for safety/parity with the py engine.
	const meta = ".\\+()|^$={}?:!";
	if (meta.indexOf(c) >= 0) return "\\" + c;
	return c;
}

// Compile rules once at module load (PATTERNS is the embedded literal array).
const RULES = PATTERNS.map(function (raw) {
	const negated = raw.startsWith("!");
	const pat = negated ? raw.slice(1) : raw;
	const body = pat.endsWith("/") ? pat.replace(/\/+$/, "") : pat;
	const compiled = globToRegex(body);
	return { raw: raw, negated: negated, regex: compiled.regex };
});

// matches: gitignore last-match-wins. Empty PATTERNS -> never block.
function matches(path) {
	if (!path) return false;
	let p = String(path).replace(/\\/g, "/").replace(/^\/+/, "");
	while (p.startsWith("./")) p = p.slice(2);
	if (!p) return false;
	let excluded = false;
	for (const rule of RULES) {
		if (rule.regex.test(p)) {
			excluded = !rule.negated;
		}
	}
	return excluded;
}

// isBlocked: path-relative guard. Tests both the given path and its basename
// (a bare ".env" pattern should match "sub/.env" too — handled by basename
// semantics in globToRegex, but we also strip dir for safety).
function isBlocked(path) {
	if (!path) return false;
	if (matches(path)) return true;
	const slash = path.lastIndexOf("/");
	if (slash >= 0) {
		const base = path.slice(slash + 1);
		if (base && matches(base)) return true;
	}
	return false;
}`
