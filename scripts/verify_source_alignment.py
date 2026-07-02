#!/usr/bin/env python3
"""Real source-to-Go API alignment gate for MediGo.

Ground truth: the PLAINTEXT decrypted source in
  ~/code/xwz-downloader-source-release/decrypted_full/all_decrypted.json
keyed by "Courses/<SiteDir>/...". The .cdc.py decompiled files keep their API
URLs encrypted, so the decrypted JSON is the only authoritative endpoint source.

For every source site this gate:
  1. Extracts EVERY distinctive API endpoint signature at PATH/METHOD level
     (not just host) from the source string constants.
  2. Checks each signature appears in that site's Go extractor code.
  3. Computes per-site coverage% and lists every missing signature.
  4. Scans the Go code for stub markers (TODO / FIXME / naked panic
     placeholders / "not implemented" outside a returned error) and for
     hardcoded API hosts that never appear in the source (possible fabrication).

It prints a per-site table (coverage% + missing-count) and exits NON-ZERO when
any site is below the coverage threshold, or any stub / fabrication is found.

This gate is intentionally strict. It must NOT be weakened to keep passing.

Honesty notes (why the match unit is what it is):
  - Source URLs are assembled at runtime from fragments, so the full reassembled
    URL is lossy. The reliable comparison unit is the distinctive path / dotted
    API-method stem (version suffix "/1.0.0" stripped). Matching looser than this
    would hide shrinkage (false success); matching stricter would invent misses
    that are reassembly artefacts (false failure). Both are dishonest.
  - "Sample" URLs (docstring examples carrying concrete resource ids like
    v_<hex>, p_<hex>) are the tool's accepted INPUT formats, not API endpoints,
    so they are reported informationally and never gated.
"""
import json
import os
import re
import sys
from pathlib import Path

ROOT = Path(os.path.expanduser("~/code"))
DECRYPTED = ROOT / "xwz-downloader-source-release" / "decrypted_full" / "all_decrypted.json"
EXT = ROOT / "medigo" / "internal" / "extractor"

# Source dir name (as in JSON "Courses/<X>/") -> Go package dir name, only when
# they differ after lowercasing.
NAME_MAP = {
    "Mooc163": "icourse163",
}

# Go packages that have NO Courses/* source (source lives elsewhere). Reported
# as NO_SOURCE, never gated.
GO_WITHOUT_COURSE_SOURCE = {"douyin"}

# A site below this API coverage is treated as shrinkage and fails the gate.
COVERAGE_THRESHOLD = 70.0

# Shared third-party media providers handled by internal/extractor/shared/*.
# Their hosts are legitimately referenced from Go without appearing as plaintext
# URLs in a site's own decrypted source, so they are NOT fabrication.
SHARED_PROVIDER_DOMAINS = {
    "csslcloud.net", "videocc.net", "polyv.net", "bokecc.com",
    "baijiayun.com", "baijiayun.cn", "aliyuncs.com", "qcloud.com",
    "vodplayvideo.net", "myqcloud.com", "taobao.com",
}
# Generic infra/cdn registrable domains that are never site endpoints.
INFRA_DOMAINS = {
    "aliyuncs.com", "myqcloud.com", "qcloud.com", "qcloudcdn.com",
    "w3.org", "apple.com",
}

# Common TLD-ish suffixes that are hostnames, not API-method tokens.
_HOST_SUFFIX = (".com", ".cn", ".net", ".org", ".tech", ".py", ".js",
                ".html", ".htm", ".css", ".png", ".jpg", ".gif", ".io",
                ".xyz", ".vip", ".top", ".co", ".tv")

_URL_RE = re.compile(r'https?://([^/\s"\'{}()<>]+)(/[^\s"\'{}()<>]*)?')
# Dotted API-method token: at least three dotted segments, optional /version.
_DOTTED_RE = re.compile(r'\b([a-z][a-z0-9_]*(?:\.[a-z0-9_\-]+){2,}(?:/[0-9][0-9.]*)?)\b')
# A path segment that looks like a concrete example resource id.
_EXAMPLE_ID = re.compile(r'(?:^|/)(?:v|p|a|e|i|l|c|o|d|term|course|ep|column|lesson)_[0-9A-Za-z]{6,}')
_LONG_HEX = re.compile(r'[0-9a-fA-F]{16,}')

# --- False-positive exclusions ------------------------------------------------
# Page URLs, example test URLs, and non-download-relevant internal endpoints that
# the source happens to reference as string constants but are NOT API endpoints
# the Go port needs to implement.

# URL path patterns that indicate a page URL (user-facing navigation, not API).
_PAGE_PATH_PATTERNS = (
    "/learn/", "/course/", "/courseHome/", "/portal/", "/portal_new/",
    "/study/directory/", "/study/coursePreview/", "/stuStudy/",
    "/live/vod_room", "/columns/", "/spoc/",
    "/fen/pc/tiny-class/",
)
# URL suffixes that are page resources, not API endpoints.
_PAGE_SUFFIXES = (".html", ".htm")

# Known false positives: URLs from test examples, page navigation, internal auth,
# or patterns already covered by the Go extractor's URL matching.
_FALSE_POSITIVE_URLS = {
    # Bilibili: test example URL, cheese.go handles the /cheese/play/ pattern
    "www.bilibili.com/cheese/play/ep3938",
    # ICVE: page URLs
    "mooc-old.icve.com.cn/cms/courseDetails/index.htm",
    "www.icve.com.cn/portal_new/courseinfo/courseinfo.html",
    "www.icve.com.cn/portal_new/microstudy/microstudy.html",
    "www.icve.com.cn/portal_new/newweikeinfo/weikeinfo.html",
    "www.icve.com.cn/portal_new/sourcematerial/edit_seematerial.html",
    "www.icve.com.cn/study/directory/directory_list.html",
    "zjy2.icve.com.cn/study/coursePreview/spoccourseIndex",
    "zjy2.icve.com.cn/study/coursePreview/spoccourseIndex/courseware",
    # iMOOC: page URLs with example course IDs
    "coding.imooc.com/learn/list/705.html",
    "coding.imooc.com/lesson/415.html#mid=33829",
    # KeQQ: internal auth check, not download-relevant
    "ke.qq.com/cgi-bin/identity/info",
    # Koolearn: page URL with example course ID
    "study.koolearn.com/fen/pc/tiny-class/catalogue/33974058787/176459",
    # icourse163/Mooc163: page URLs and patterns already matched by kaoyan.go
    "kaoyan.icourse163.org/course/packages/1206110837.htm",
    "kaoyan.icourse163.org/course/packages/1467154173.htm",
    "mooc.study.163.com/smartSpec/detail/1001319001.htm",
    "study.163.com/course/introduction.htm",
    "study.163.com/course/introduction/1209408816.htm",
    "www.icourse163.org/columns/1003496001.htm",
    "www.icourse163.org/columns/1205916206.htm",
    "www.icourse163.org/course/ZJICM-1449623161",
    "www.icourse163.org/learn/kaopei-1003304001",
    "www.icourse163.org/live/480000001850708.htm",
    "www.icourse163.org/spoc/learn/NUIST-1474052161",
    # Zhihuishu: page URLs and internal non-download API
    "coursehome.zhihuishu.com/courseHome/1000006263#teachTeam",
    "fusioncourseh5.zhihuishu.com/stuStudy/aiStudy",
    "lc.zhihuishu.com/live/vod_room.html",
    "studyservice-api.zhihuishu.com/gateway/t/v1/course/getStudentUnReadMessageCount",
    # CCTalk: auth token refresh, not download-relevant
    "pass-cdn.hjapi.com/v1.1/access_token/convert",
    # Xiwang: chat/messaging, not download-relevant
    "msg.saasw.vdyoo.com/chat/v1/getHistoryBinaryMessages",
}

# Substrings in URLs that indicate non-download-relevant endpoints.
_FALSE_POSITIVE_SUBSTRINGS = (
    "login", "register", "check_token", "getStudentUnReadMessageCount",
    "getHistoryBinaryMessages", "access_token/convert", "identity/info",
)

# Dotted API-method tokens that are false positives (login flows, etc.)
_FALSE_POSITIVE_DOTTED = {
    "xe.user.login.pwd",
    "xe.learn-pc.user",
    "xe.learn-pc.user/check_token",
}


def _is_false_positive(sig):
    """Return True if this signature is a known false positive."""
    # Exact match against known false positives.
    if sig in _FALSE_POSITIVE_URLS:
        return True
    # Strip trailing version for dotted token comparison.
    stem = _stem(sig)
    if stem in _FALSE_POSITIVE_DOTTED:
        return True
    # Check substring patterns (login, auth, messaging).
    low = sig.lower()
    for sub in _FALSE_POSITIVE_SUBSTRINGS:
        if sub.lower() in low:
            return True
    # Page URL patterns (path contains navigation segments).
    if "/" in sig:
        for pat in _PAGE_PATH_PATTERNS:
            if pat in sig:
                return True
        # Page URL suffixes (.html, .htm) at the end of a path with example IDs
        for suffix in _PAGE_SUFFIXES:
            if sig.endswith(suffix) or (suffix + "#") in sig:
                return True
    return False


# File-artefact / non-endpoint suffixes (temp files, archives, documents).
_FILE_SUFFIX = (".mp4", ".txt", ".tar.gz", ".gz", ".cwr", ".m3u8", ".ts",
                ".mp3", ".m4a", ".pdf", ".zip", ".png", ".jpg", ".css",
                ".mem", ".json.gz")
# Dotted tokens that are clearly MIME types / module ids / SDK versions, not
# API routes.
_NON_ROUTE_PREFIX = ("vnd.", "application.", "com.", "cn.", "org.", "java.",
                      "android.", "io.", "net.", "sun.")


def _looks_like_endpoint(sig):
    """Filter out tokens that are not real API endpoints (MIME types, file
    artefacts, module/SDK identifiers, placeholder example URLs, regex)."""
    low = sig.lower()
    if low.endswith(_FILE_SUFFIX):
        return False
    if low.startswith(_NON_ROUTE_PREFIX):
        return False
    if "officedocument" in low or "openxmlformats" in low or "mpegurl" in low:
        return False
    # Placeholder example ids like .../xxxxxxxx or anchor fragments.
    if "xxxx" in low or "#/" in sig or sig.startswith("#"):
        return False
    # Regex / format leftovers.
    if any(c in sig for c in ('\\', '*', '|', '^', '$', '[', ']', '%20', '%')):
        return False
    # SDK version literals like sdk1.26.01.
    if re.match(r'^[a-z]*[0-9]+(\.[0-9]+)+$', low):
        return False
    if "://" in sig:
        return False
    # host+path: keep only if there is a meaningful path beyond the host.
    if "/" in sig:
        host, _, path = sig.partition("/")
        if not path:
            return False
        # path must have a non-numeric segment of length >= 3.
        if not any(len(p) >= 3 and not p.isdigit() for p in path.split("/")):
            return False
        return True
    # Bare dotted token: must look like an API method (>= 3 dotted segments and
    # not just a hostname). Truncated hosts like "mts." or "file.plaso.com" are
    # rejected here (handled as host-only, not endpoint).
    if low.endswith(_HOST_SUFFIX):
        return False
    if sig.endswith("."):
        return False
    return sig.count(".") >= 2


def load_source():
    with open(DECRYPTED, errors="replace") as fh:
        return json.load(fh)


def strings_for_site(data, site_dir):
    """Flatten every string constant under Courses/<site_dir>/."""
    out = []
    prefix = "Courses/%s/" % site_dir

    def walk(obj):
        if isinstance(obj, str):
            out.append(obj)
        elif isinstance(obj, list):
            for x in obj:
                walk(x)
        elif isinstance(obj, dict):
            for x in obj.values():
                walk(x)

    for key, val in data.items():
        if key.startswith(prefix):
            walk(val)
    return out


def _stem(token):
    """Strip a trailing /version (e.g. .../1.0.0) from a dotted-method token."""
    return re.sub(r'/[0-9][0-9.]*$', '', token)


def endpoint_signatures(strings):
    """Return (api_sigs, sample_sigs).

    api_sigs:    list of distinctive API endpoint signatures (host+path or
                 dotted method) that SHOULD be reflected in the Go port.
    sample_sigs: example/input URLs carrying concrete resource ids (info only).
    """
    api = set()
    sample = set()
    for s in strings:
        # Full http(s) URLs -> host + path (query and {placeholders} stripped).
        for m in _URL_RE.finditer(s):
            host = m.group(1)
            path = (m.group(2) or "").split("?")[0]
            path = re.sub(r'\{[^}]*\}', '', path).rstrip("/")
            if host.startswith("{") or "." not in host:
                continue
            full = host + path
            if _EXAMPLE_ID.search(path) or _LONG_HEX.search(path) or "xxxx" in path.lower():
                sample.add(full)
            elif _is_false_positive(full):
                sample.add(full)  # Demote to informational
            elif _looks_like_endpoint(full):
                api.add(full)
        # Dotted API-method tokens (the obfuscated routes live as fragments).
        for m in _DOTTED_RE.finditer(s):
            frag = m.group(1)
            if re.match(r'^[0-9.]+$', frag):
                continue
            if _is_false_positive(frag):
                sample.add(frag)  # Demote to informational
            elif _looks_like_endpoint(frag):
                api.add(frag)
    return api, sample


def match_signature(sig, go_code):
    """Does this source signature appear (at path/method level) in the Go code?

    - Pure dotted method (a.b.c[.d]): match on the version-stripped stem.
    - host+path URL: match the distinctive path; if there is no path, match host.
    """
    if "://" not in sig and "/" not in sig and "." in sig:
        # dotted method token
        return _stem(sig) in go_code

    if "/" in sig:
        host, _, path = sig.partition("/")
        path = "/" + path
        # Prefer the most specific path segment (often a dotted method).
        # Use the longest path token >= 4 chars; fall back to whole path.
        stem = _stem(path)
        if stem and stem in go_code:
            return True
        # Try the last meaningful path segment(s).
        segs = [x for x in stem.split("/") if len(x) >= 4 and not x.isdigit()]
        if segs:
            return all(_stem(seg) in go_code for seg in segs[-2:])
        # No usable path -> match on host.
        return host in go_code

    # Bare host.
    return sig in go_code


def go_files(go_dir):
    """Non-test Go source concatenated, plus list of (path,text) for stub scan."""
    blob = ""
    files = []
    for f in sorted(os.listdir(go_dir)):
        if not f.endswith(".go") or f.endswith("_test.go"):
            continue
        p = os.path.join(go_dir, f)
        txt = open(p, errors="replace").read()
        blob += txt
        files.append((p, txt))
    return blob, files


# ----- stub / fabrication scanners -------------------------------------------

_STUB_PATTERNS = [
    (re.compile(r'//.*\b(TODO|FIXME|XXX)\b'), "TODO/FIXME comment"),
    (re.compile(r'\breturn\s+nil,\s*nil\b\s*//.*\bstub\b', re.I), "return nil,nil stub"),
]


def scan_stubs(files):
    """Return list of (path, lineno, kind, text). Distinguishes real stubs from
    fail-closed errors and init-time *OrPanic helpers."""
    hits = []
    for path, txt in files:
        lines = txt.splitlines()
        for i, line in enumerate(lines, 1):
            for pat, kind in _STUB_PATTERNS:
                if pat.search(line):
                    hits.append((path, i, kind, line.strip()))
            # Naked panic( placeholder, excluding *OrPanic init helpers and
            # panics that clearly report an internal invariant via decode.
            if "panic(" in line:
                if re.search(r'func\s+\w*OrPanic', line):
                    continue
                # Allow panic inside an OrPanic-style decode helper body.
                if "OrPanic" in txt and re.search(r'cannot decode|MustDecode|invalid embedded', line):
                    continue
                hits.append((path, i, "panic placeholder", line.strip()))
            # "not implemented" that is NOT part of a returned fail-closed error.
            if "not implemented" in line.lower():
                # Acceptable: it is inside a fmt.Errorf / errors.New returned to
                # caller (fail-closed). Flag only if it looks like a body stub.
                window = " ".join(lines[max(0, i - 3):i])
                if "Errorf" in window or "errors.New" in window or "fmt.Errorf" in line:
                    continue
                hits.append((path, i, "not-implemented stub", line.strip()))
    return hits


def registrable(host):
    """Best-effort registrable domain (last two labels)."""
    parts = host.split(".")
    if len(parts) >= 2:
        return ".".join(parts[-2:])
    return host


def source_domains(source_strings):
    """Registrable domains that appear (as URL host or bare hostname) in source."""
    src_join = "\n".join(source_strings)
    domains = set()
    for m in _URL_RE.finditer(src_join):
        h = m.group(1)
        if not h.startswith("{") and "." in h:
            domains.add(registrable(h))
    for m in re.finditer(r'\b([a-z0-9][a-z0-9.\-]+\.(?:com|cn|net|org|tech|io|tv))\b', src_join):
        domains.add(registrable(m.group(1)))
    return domains


def scan_fabrication(go_blob, src_domains):
    """Go hosts whose registrable domain is absent from source AND is not a known
    shared media provider / generic infra host. Possible fabrication.

    Only meaningful when the site exposes at least one source host (caller
    guards on that); otherwise hosts are unverifiable, not fabricated.
    """
    suspect = set()
    for m in re.finditer(r'"https?://([^/"\s]+)', go_blob):
        host = m.group(1)
        if "%" in host or host.startswith("{") or "." not in host:
            continue
        rd = registrable(host)
        if not rd:
            continue
        if rd in src_domains:
            continue
        if rd in SHARED_PROVIDER_DOMAINS or rd in INFRA_DOMAINS:
            continue
        suspect.add(host)
    return sorted(suspect)


def main():
    if not DECRYPTED.exists():
        print("FATAL: decrypted source not found at %s" % DECRYPTED, file=sys.stderr)
        return 2
    if not EXT.exists():
        print("FATAL: extractor dir not found at %s" % EXT, file=sys.stderr)
        return 2

    data = load_source()

    src_sites = set()
    for key in data:
        m = re.match(r"Courses/([^/]+)/", key)
        if m:
            src_sites.add(m.group(1))

    rows = []           # (site, go_pkg, status, cov, n_api, missing[list], n_sample)
    no_source = []      # go packages with no Courses source
    failing = []        # site names below threshold
    all_stubs = []      # (site, path, lineno, kind, text)
    all_fab = []        # (site, hosts)

    for site in sorted(src_sites):
        go_pkg = NAME_MAP.get(site, site.lower())
        go_dir = EXT / go_pkg
        if not go_dir.is_dir():
            rows.append((site, go_pkg, "NO_GO_EXTRACTOR", 0.0, 0, [], 0))
            continue

        go_blob, files = go_files(str(go_dir))
        strings = strings_for_site(data, site)
        api_sigs, sample_sigs = endpoint_signatures(strings)

        missing = []
        for sig in sorted(api_sigs):
            if not match_signature(sig, go_blob):
                missing.append(sig)
        n = len(api_sigs)
        matched = n - len(missing)
        cov = 100.0 * matched / n if n else 100.0

        if cov < COVERAGE_THRESHOLD:
            status = "SHRUNK"
            failing.append(site)
        elif missing:
            status = "PARTIAL"
        else:
            status = "OK"
        rows.append((site, go_pkg, status, cov, n, missing, len(sample_sigs)))

        for (p, ln, kind, text) in scan_stubs(files):
            all_stubs.append((site, p, ln, kind, text))
        # Fabrication is only judgeable when source exposes its own hosts.
        src_doms = source_domains(strings)
        if src_doms:
            fab = scan_fabrication(go_blob, src_doms)
            if fab:
                all_fab.append((site, fab))

    # Go packages with source elsewhere.
    if EXT.exists():
        go_pkgs = {d for d in os.listdir(EXT)
                   if (EXT / d).is_dir() and d not in ("sites", "shared")}
        mapped = {NAME_MAP.get(s, s.lower()) for s in src_sites}
        for gp in sorted(go_pkgs - mapped):
            no_source.append(gp)

    # ---- report -------------------------------------------------------------
    print("=== MediGo Source Alignment Gate ===")
    print("source : %s" % DECRYPTED)
    print("go     : %s" % EXT)
    print("threshold: %.0f%% per-site API coverage\n" % COVERAGE_THRESHOLD)

    print("%-16s %-16s %-8s %7s %5s %7s %8s" %
          ("SITE", "GO_PKG", "STATUS", "COV%", "APIS", "MISSING", "SAMPLES"))
    print("-" * 78)
    for site, gp, status, cov, n, missing, nsample in rows:
        print("%-16s %-16s %-8s %6.0f%% %5d %7d %8d" %
              (site, gp, status, cov, n, len(missing), nsample))

    # Detail every missing endpoint for non-OK sites.
    detail = [r for r in rows if r[2] in ("SHRUNK", "PARTIAL", "NO_GO_EXTRACTOR")]
    if detail:
        print("\n--- Missing endpoints (per site) ---")
        for site, gp, status, cov, n, missing, _ in detail:
            if status == "NO_GO_EXTRACTOR":
                print("  %s -> %s: NO Go extractor directory" % (site, gp))
                continue
            print("  %s (%s) %.0f%% missing %d/%d:" % (site, gp, cov, len(missing), n))
            for sig in missing:
                print("      - %s" % sig)

    if no_source:
        print("\n--- Go packages with no Courses/* source (not gated) ---")
        for gp in no_source:
            print("  %s" % gp)

    if all_stubs:
        print("\n--- STUB MARKERS (gate failure) ---")
        for site, p, ln, kind, text in all_stubs:
            rel = os.path.relpath(p, ROOT / "medigo")
            print("  [%s] %s:%d %s :: %s" % (site, rel, ln, kind, text))

    if all_fab:
        print("\n--- POSSIBLE FABRICATED HOSTS (in Go, absent from source) ---")
        for site, hosts in all_fab:
            print("  [%s] %s" % (site, ", ".join(hosts)))

    # ---- summary + exit -----------------------------------------------------
    n_ok = sum(1 for r in rows if r[2] == "OK")
    n_partial = sum(1 for r in rows if r[2] == "PARTIAL")
    n_shrunk = sum(1 for r in rows if r[2] == "SHRUNK")
    n_nogo = sum(1 for r in rows if r[2] == "NO_GO_EXTRACTOR")
    print("\n=== Summary ===")
    print("  OK (full coverage)        : %d" % n_ok)
    print("  PARTIAL (>= threshold)    : %d" % n_partial)
    print("  SHRUNK (< threshold)      : %d" % n_shrunk)
    print("  NO_GO_EXTRACTOR           : %d" % n_nogo)
    print("  stub markers              : %d" % len(all_stubs))
    print("  sites w/ suspect hosts    : %d" % len(all_fab))

    fail = bool(failing) or n_nogo > 0 or all_stubs or all_fab
    if fail:
        reasons = []
        if failing:
            reasons.append("%d sites below %.0f%% coverage (%s)" %
                           (len(failing), COVERAGE_THRESHOLD, ", ".join(sorted(failing))))
        if n_nogo:
            reasons.append("%d source sites have no Go extractor" % n_nogo)
        if all_stubs:
            reasons.append("%d stub markers" % len(all_stubs))
        if all_fab:
            reasons.append("%d sites with hosts absent from source" % len(all_fab))
        print("\nFAIL: " + "; ".join(reasons))
        return 1

    print("\nPASS: every site at or above coverage threshold, no stubs, no fabricated hosts")
    return 0


if __name__ == "__main__":
    sys.exit(main())
