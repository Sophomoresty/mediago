#!/usr/bin/env python3
"""Verify each Go extractor's API URLs match the decompiled Python source."""
import os, re, sys

DECOMPILED = os.path.expanduser("~/code/xwz-downloader-source-release/decompiled_full/Mooc/Courses")
EXTRACTORS = os.path.expanduser("~/code/medigo/internal/extractor")
GENERIC_DIR = os.path.join(EXTRACTORS, "sites")

def extract_api_urls(py_dir):
    """Extract https API URLs from .cdc.py files in a directory."""
    urls = set()
    for f in os.listdir(py_dir):
        if not f.endswith('.cdc.py') or 'Config' in f:
            continue
        try:
            content = open(os.path.join(py_dir, f), errors='replace').read()
            for m in re.finditer(r"'(https?://[^']{15,})'", content):
                url = m.group(1)
                # Filter to API-like URLs
                if any(k in url.lower() for k in ['api', 'ajax', 'json', 'play', 'video', 'course',
                    'info', 'list', 'detail', 'status', 'm3u8', 'token', 'sign', 'login',
                    'replay', 'live', 'download', 'resource', 'chapter', 'lesson']):
                    # Normalize: strip format params
                    base = re.sub(r'\{[^}]+\}', '{}', url)
                    base = re.sub(r'\?.*', '', base)
                    if len(base) > 20:
                        urls.add(base[:80])
        except:
            pass
    return urls

def check_go_extractor(site_name, api_urls):
    """Check if Go extractor contains at least one matching API domain/path."""
    # Find Go extractor directory
    site_lower = site_name.lower()
    go_dir = None
    for d in os.listdir(EXTRACTORS):
        if d == site_lower or d == site_lower.replace('_', '') or d == site_lower.replace('163', '163'):
            go_dir = os.path.join(EXTRACTORS, d)
            break

    if not go_dir or not os.path.isdir(go_dir):
        return "NO_EXTRACTOR", []

    # Read all .go files in the directory
    go_content = ""
    for f in os.listdir(go_dir):
        if f.endswith('.go'):
            go_content += open(os.path.join(go_dir, f), errors='replace').read()

    # Check if it's in the generic registry instead
    if not go_content:
        return "NO_EXTRACTOR", []

    # Check each API URL's domain+path against Go code
    matched = []
    unmatched = []
    for url in api_urls:
        # Extract domain + first path segment
        m = re.match(r'https?://([^/]+)(/[^/]+)?', url)
        if not m:
            continue
        domain = m.group(1).replace('.', r'\.')
        path = m.group(2) or ''

        # Check if domain appears in Go code
        if re.search(re.escape(m.group(1)), go_content):
            matched.append(url)
        else:
            unmatched.append(url)

    if not matched and unmatched:
        return "MISMATCH", unmatched
    elif matched and unmatched:
        return "PARTIAL", unmatched
    elif matched:
        return "MATCH", []
    else:
        return "EMPTY", []

# Map Python site dirs to Go extractor names
name_map = {
    'Mooc163': 'icourse163',
    'Cctv': 'cctv',
}

results = {"MATCH": [], "PARTIAL": [], "MISMATCH": [], "NO_EXTRACTOR": [], "EMPTY": []}

for site_dir in sorted(os.listdir(DECOMPILED)):
    full_path = os.path.join(DECOMPILED, site_dir)
    if not os.path.isdir(full_path):
        continue
    if site_dir in ('Course_Base', 'Course_Config', 'Course_Others'):
        continue

    api_urls = extract_api_urls(full_path)
    if not api_urls:
        continue

    go_name = name_map.get(site_dir, site_dir)
    status, missing = check_go_extractor(go_name, api_urls)
    results[status].append((site_dir, missing))

# Report
total = sum(len(v) for v in results.values())
print(f"=== API Alignment Report ({total} sites with APIs) ===\n")

print(f"✅ MATCH ({len(results['MATCH'])}): API domains found in Go code")
for name, _ in results['MATCH']:
    print(f"   {name}")

print(f"\n⚠️  PARTIAL ({len(results['PARTIAL'])}): Some APIs match, some missing")
for name, missing in results['PARTIAL']:
    print(f"   {name}: missing {len(missing)} URLs")

print(f"\n❌ MISMATCH ({len(results['MISMATCH'])}): No source APIs found in Go code")
for name, missing in results['MISMATCH']:
    print(f"   {name}: {missing[0][:60] if missing else ''}")

print(f"\n🔲 NO_EXTRACTOR ({len(results['NO_EXTRACTOR'])}): No dedicated Go extractor")
for name, _ in results['NO_EXTRACTOR']:
    print(f"   {name}")

print(f"\n=== Summary ===")
print(f"  MATCH: {len(results['MATCH'])}")
print(f"  PARTIAL: {len(results['PARTIAL'])}")
print(f"  MISMATCH: {len(results['MISMATCH'])}")
print(f"  NO_EXTRACTOR: {len(results['NO_EXTRACTOR'])}")

# Exit code: fail if any MISMATCH
if results['MISMATCH'] or results['NO_EXTRACTOR']:
    print(f"\nFAIL: {len(results['MISMATCH'])} mismatched + {len(results['NO_EXTRACTOR'])} missing extractors")
    sys.exit(1)
else:
    print(f"\nPASS: All extractors aligned")
    sys.exit(0)
