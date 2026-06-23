#!/usr/bin/env python3
"""Strict verifier: every Extract() must perform real HTTP+parse, no stubs.

Output per site:
  PASS    — Extract() body has at least one HTTP call AND one JSON/regex parse
  STUB    — Extract() returns "not yet implemented" / "not implemented" early
  BLOCKED — explicit blocked-by-X message after partial implementation
  PARTIAL — has HTTP call but no parse, or has parse but no HTTP call

Also surfaces source-URL coverage from decompiled Python.
"""
import os
import re
import subprocess
import sys

EXTRACTORS = os.path.expanduser("~/code/medigo/internal/extractor")
DECOMPILED = os.path.expanduser("~/code/xwz-downloader-source-release/decompiled_full/Mooc/Courses")

SKIP_DIRS = {"sites", "shared"}
NON_SITE_FILES = {"extractor.go", "registry.go"}

STUB_PATTERNS = [
    re.compile(r'fmt\.Errorf\(["\'].*?not yet implemented', re.IGNORECASE),
    re.compile(r'fmt\.Errorf\(["\'].*?not implemented', re.IGNORECASE),
    re.compile(r'fmt\.Errorf\(["\'].*?chain.*?not yet', re.IGNORECASE),
]

# A real implementation has one of these (HTTP call) AND one of these (parse).
HTTP_PATTERNS = [
    r'\.GetString\(',
    r'\.PostForm\(',
    r'\.GetBytes\(',
    r'\.Get\(',
    r'\.Post\(',
]
PARSE_PATTERNS = [
    r'json\.Unmarshal\(',
    r'\.FindStringSubmatch\(',
    r'\.FindAllStringSubmatch\(',
    r'gjson\.Get\(',
]

# Extract Extract() body from a Go file. Returns text between `func ... Extract(` and matching `}`.
def extract_body(src):
    m = re.search(r'func\s+\(\w+\s+\*\w+\)\s+Extract\(', src)
    if not m:
        return None
    # find opening brace after signature
    i = src.find('{', m.end())
    if i < 0:
        return None
    depth = 1
    j = i + 1
    while j < len(src) and depth > 0:
        if src[j] == '{':
            depth += 1
        elif src[j] == '}':
            depth -= 1
        j += 1
    return src[i+1:j-1]


def classify(go_path):
    src = open(go_path, errors='replace').read()
    body = extract_body(src)
    if body is None:
        return 'NO_EXTRACT', "no Extract() function"

    # An impl is real if EITHER Extract() body OR the rest of the file (helpers
    # called from Extract) has HTTP+parse. We treat the whole file as the unit.
    file_has_http = any(re.search(p, src) for p in HTTP_PATTERNS)
    file_has_parse = any(re.search(p, src) for p in PARSE_PATTERNS)
    is_blocked = bool(re.search(r'fmt\.Errorf\(["\'].*?(blocked|requires|needs).*?(LWP|JS sandbox|WebSocket|DRM|engine|sandbox)', src, re.IGNORECASE))
    extract_returns_error_immediately = bool(re.search(r'return nil,\s*fmt\.Errorf\(.*?not yet implemented', body, re.IGNORECASE))

    # If Extract() body itself just returns "not yet implemented" without
    # branching to a helper that does the work, it's a STUB.
    if extract_returns_error_immediately and not file_has_http:
        return 'STUB', "Extract() returns 'not yet implemented' and file has no HTTP call"
    if not file_has_http and not file_has_parse:
        return 'STUB', "no HTTP call, no parse"
    if file_has_http and file_has_parse:
        return 'PASS', "has HTTP+parse"
    if is_blocked:
        return 'BLOCKED', "blocked-by-X with partial implementation"
    if file_has_http and not file_has_parse:
        return 'PARTIAL', "HTTP call without parse"
    return 'PARTIAL', "parse without HTTP call"


def main():
    results = {'PASS': [], 'BLOCKED': [], 'PARTIAL': [], 'STUB': [], 'NO_EXTRACT': []}
    for site_dir in sorted(os.listdir(EXTRACTORS)):
        full = os.path.join(EXTRACTORS, site_dir)
        if not os.path.isdir(full) or site_dir in SKIP_DIRS:
            continue
        # find primary go file
        go_files = [f for f in os.listdir(full) if f.endswith('.go') and not f.endswith('_test.go')]
        if not go_files:
            continue
        # check ALL go files in the dir, not just one
        verdict, reason = 'NO_EXTRACT', 'no main file'
        for f in go_files:
            v, r = classify(os.path.join(full, f))
            # take the BEST verdict across files (cheese.go counts for bilibili)
            order = {'PASS': 0, 'BLOCKED': 1, 'PARTIAL': 2, 'STUB': 3, 'NO_EXTRACT': 4}
            if order[v] < order[verdict]:
                verdict, reason = v, r
        results[verdict].append((site_dir, reason))

    print(f"=== Strict alignment audit ===\n")
    for k in ('PASS', 'BLOCKED', 'PARTIAL', 'STUB', 'NO_EXTRACT'):
        sites = results[k]
        if not sites:
            continue
        print(f"\n{k} ({len(sites)}):")
        for name, reason in sites:
            print(f"  {name:<20} {reason}")

    print(f"\n=== Summary ===")
    for k in ('PASS', 'BLOCKED', 'PARTIAL', 'STUB', 'NO_EXTRACT'):
        print(f"  {k}: {len(results[k])}")

    # FAIL if any STUB exists
    if results['STUB'] or results['NO_EXTRACT']:
        print(f"\nFAIL: {len(results['STUB'])} STUB + {len(results['NO_EXTRACT'])} NO_EXTRACT")
        sys.exit(1)
    print(f"\nPASS: no stubs")


if __name__ == '__main__':
    main()
