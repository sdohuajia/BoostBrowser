#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TARGET="${1:-}"
MANIFEST_PATH="${2:-$ROOT_DIR/publish/runtime-manifest.json}"

if [[ -z "$TARGET" ]]; then
  echo "Usage: $0 <target> [manifest-path]" >&2
  echo "Example: $0 linux-amd64" >&2
  exit 1
fi

if [[ ! -f "$MANIFEST_PATH" ]]; then
  echo "[ERROR] runtime manifest not found: $MANIFEST_PATH" >&2
  exit 1
fi

PYTHON_BIN=""
for candidate in python3 python py; do
  if ! command -v "$candidate" >/dev/null 2>&1; then
    continue
  fi
  if "$candidate" -c "import sys" >/dev/null 2>&1; then
    PYTHON_BIN="$candidate"
    break
  fi
done

if [[ -z "$PYTHON_BIN" ]]; then
  echo "[ERROR] python3 (or python) is required for runtime verification" >&2
  exit 1
fi

"$PYTHON_BIN" - "$ROOT_DIR" "$TARGET" "$MANIFEST_PATH" <<'PY'
import hashlib
import json
import os
import sys

root_dir = sys.argv[1]
target = sys.argv[2]
manifest_path = sys.argv[3]

with open(manifest_path, "r", encoding="utf-8") as f:
    manifest = json.load(f)

files = manifest.get("files", [])
if not isinstance(files, list):
    print("[ERROR] invalid manifest: files must be an array", file=sys.stderr)
    sys.exit(1)

targets = []
for item in files:
    if not isinstance(item, dict):
        continue
    item_targets = item.get("targets") or []
    if isinstance(item_targets, list) and target in item_targets:
        targets.append(item)

if not targets:
    print(f"[ERROR] no runtime entries found for target: {target}", file=sys.stderr)
    sys.exit(1)

errors = []
for item in targets:
    rel_path = str(item.get("path", "")).strip()
    expected = str(item.get("sha256", "")).strip().lower()
    if not rel_path:
        errors.append("manifest entry has empty path")
        continue
    if not expected or "todo_replace_with_sha256" in expected:
        errors.append(f"{rel_path}: sha256 is not initialized")
        continue

    abs_path = os.path.join(root_dir, rel_path.replace("/", os.sep))
    if not os.path.isfile(abs_path):
        errors.append(f"{rel_path}: file not found")
        continue

    h = hashlib.sha256()
    with open(abs_path, "rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    actual = h.hexdigest().lower()
    if actual != expected:
        errors.append(f"{rel_path}: sha256 mismatch (expected {expected}, got {actual})")

if errors:
    print("[ERROR] runtime verification failed:", file=sys.stderr)
    for e in errors:
        print(f"  - {e}", file=sys.stderr)
    sys.exit(1)

print(f"[OK] runtime verification passed for target: {target}")
PY
