#!/usr/bin/env python3
import argparse
import hashlib
import json
from pathlib import Path
import sys


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def main() -> int:
    parser = argparse.ArgumentParser(description="Update runtime sha256 entries in publish/runtime-manifest.json")
    parser.add_argument("--target", required=True, help="Target label, e.g. linux-amd64")
    parser.add_argument("--manifest", default="publish/runtime-manifest.json", help="Manifest path")
    args = parser.parse_args()

    script_root = Path(__file__).resolve().parents[2]
    manifest_path = Path(args.manifest)
    if not manifest_path.is_absolute():
        manifest_path = (script_root / manifest_path).resolve()
    if not manifest_path.is_file():
        print(f"[ERROR] manifest not found: {manifest_path}", file=sys.stderr)
        return 1

    data = json.loads(manifest_path.read_text(encoding="utf-8"))
    files = data.get("files", [])
    if not isinstance(files, list):
        print("[ERROR] invalid manifest: files must be a list", file=sys.stderr)
        return 1

    updated = 0
    for item in files:
        if not isinstance(item, dict):
            continue
        targets = item.get("targets") or []
        if args.target not in targets:
            continue

        rel = str(item.get("path", "")).strip()
        if not rel:
            continue
        fpath = Path(rel)
        if not fpath.is_absolute():
            fpath = (script_root / fpath).resolve()
        if not fpath.is_file():
            print(f"[ERROR] required file missing for target {args.target}: {rel}", file=sys.stderr)
            return 1
        item["sha256"] = sha256_file(fpath)
        updated += 1

    if updated == 0:
        print(f"[ERROR] no manifest entries matched target: {args.target}", file=sys.stderr)
        return 1

    manifest_path.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"[OK] updated {updated} entries for target: {args.target}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
