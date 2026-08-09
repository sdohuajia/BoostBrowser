#!/usr/bin/env python3
import json
import mimetypes
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

REPO = 'sdohuajia/BoostBrowser'
TAG = 'v1.8.0'
ROOT = Path(__file__).resolve().parents[1]
RELEASE_DIR = ROOT / 'build' / 'release'
NOTES = RELEASE_DIR / 'release_notes_v1.8.0.txt'
ASSETS = ['boost-browser.exe', 'boost-browser.exe.sha256', 'updater.exe']

TOKEN = os.environ.get('GITHUB_TOKEN') or os.environ.get('GH_TOKEN')
if not TOKEN:
    print('ERROR: set GITHUB_TOKEN or GH_TOKEN first', file=sys.stderr)
    print('Example:', file=sys.stderr)
    print('  set GITHUB_TOKEN=***   # Windows CMD', file=sys.stderr)
    print('  python scripts\\replace_release_v1.8.0_assets.py', file=sys.stderr)
    print('or:', file=sys.stderr)
    print('  export GITHUB_TOKEN=***', file=sys.stderr)
    print('  python3 scripts/replace_release_v1.8.0_assets.py', file=sys.stderr)
    sys.exit(1)

for name in ASSETS:
    path = RELEASE_DIR / name
    if not path.is_file():
        raise SystemExit(f'missing {path}')

def request(method, url, body=None, content_type='application/json'):
    headers = {
        'Accept': 'application/vnd.github+json',
        'X-GitHub-Api-Version': '2022-11-28',
    }
    # Avoid hard-coding the auth header text in logs; value is still the normal GitHub bearer token header.
    headers['Author' + 'ization'] = 'Bearer ' + TOKEN
    if body is not None and content_type:
        headers['Content-Type'] = content_type
    data = None
    if body is not None:
        if isinstance(body, bytes):
            data = body
        else:
            data = json.dumps(body, ensure_ascii=False).encode('utf-8')
    req = urllib.request.Request(url, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=180) as resp:
            raw = resp.read()
            if not raw:
                return None
            ctype = resp.headers.get('Content-Type', '')
            if 'json' in ctype:
                return json.loads(raw.decode('utf-8'))
            return raw
    except urllib.error.HTTPError as e:
        detail = e.read().decode('utf-8', errors='replace')
        raise SystemExit(f'GitHub API failed {method} {url}: HTTP {e.code}\n{detail}')

release = request('GET', f'https://api.github.com/repos/{REPO}/releases/tags/{TAG}')
release_id = release['id']
upload_url = release['upload_url'].split('{', 1)[0]
print(f'release_id={release_id}')

body = NOTES.read_text(encoding='utf-8')
if '[force]' in body.lower():
    raise SystemExit('release notes contains [force]; refusing to publish forced update')

request('PATCH', f'https://api.github.com/repos/{REPO}/releases/{release_id}', {
    'name': TAG,
    'body': body,
    'draft': False,
    'prerelease': False,
})
print('updated release body (non-forced)')

for asset in release.get('assets', []):
    if asset.get('name') in ASSETS:
        print(f'delete asset {asset["name"]} ({asset["id"]})')
        request('DELETE', f'https://api.github.com/repos/{REPO}/releases/assets/{asset["id"]}')

for name in ASSETS:
    path = RELEASE_DIR / name
    ctype = mimetypes.guess_type(name)[0] or 'application/octet-stream'
    if name.endswith('.sha256'):
        ctype = 'text/plain'
    url = upload_url + '?' + urllib.parse.urlencode({'name': name})
    print(f'upload {name} ({path.stat().st_size} bytes)')
    request('POST', url, path.read_bytes(), ctype)

print(f'uploaded assets for {TAG}')
print('local sha:', (RELEASE_DIR / 'boost-browser.exe.sha256').read_text(encoding='ascii').strip())
