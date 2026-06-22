#!/usr/bin/env python3
import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import time
from urllib.parse import urlparse
from urllib.request import Request, urlopen
import zipfile


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest().lower()


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Download runtime archives from pinned sources, verify SHA256, "
            "extract binaries to repo paths, and refresh runtime manifest hashes."
        )
    )
    parser.add_argument(
        "--target",
        help="Only sync one target. Default: sync all targets in sources file.",
    )
    parser.add_argument(
        "--sources",
        default="publish/runtime-sources.json",
        help="Pinned sources file path.",
    )
    parser.add_argument(
        "--manifest",
        default="publish/runtime-manifest.json",
        help="Runtime manifest path to update.",
    )
    parser.add_argument(
        "--cache-dir",
        default=".tmp/runtime-cache",
        help="Archive cache directory.",
    )
    parser.add_argument(
        "--force-download",
        action="store_true",
        help="Always re-download archives even if cache file exists.",
    )
    return parser.parse_args()


def load_sources(path: Path) -> list[dict]:
    if not path.is_file():
        raise RuntimeError(f"sources file not found: {path}")
    data = json.loads(path.read_text(encoding="utf-8"))
    items = data.get("sources", [])
    if not isinstance(items, list):
        raise RuntimeError("invalid sources file: sources must be an array")
    return items


def validate_source(item: dict) -> None:
    required = [
        "id",
        "target",
        "archiveType",
        "url",
        "archiveSha256",
        "archiveBinaryPath",
        "destPath",
    ]
    missing = [k for k in required if not str(item.get(k, "")).strip()]
    if missing:
        raise RuntimeError(f"source entry missing required fields: {', '.join(missing)}")


def choose_archive_path(cache_dir: Path, url: str) -> Path:
    parsed = urlparse(url)
    name = Path(parsed.path).name
    if not name:
        raise RuntimeError(f"invalid archive url: {url}")
    return cache_dir / name


def download_archive(url: str, dest: Path) -> None:
    dest.parent.mkdir(parents=True, exist_ok=True)
    last_error = None
    for attempt in range(1, 6):
        tmp = dest.with_suffix(dest.suffix + ".part")
        try:
            if tmp.exists():
                tmp.unlink()
            req = Request(url, headers={"User-Agent": "ant-chrome-runtime-sync/1.0"})
            with urlopen(req, timeout=120) as resp:
                if getattr(resp, "status", 200) >= 400:
                    raise RuntimeError(f"download failed ({resp.status}): {url}")
                with tmp.open("wb") as out:
                    shutil.copyfileobj(resp, out)
            tmp.replace(dest)
            return
        except Exception as exc:  # noqa: BLE001
            last_error = exc
            if tmp.exists():
                tmp.unlink()
            if attempt == 5:
                break
            wait_seconds = attempt * 2
            print(
                f"[WARN] download attempt {attempt}/5 failed for {url}: {exc}. "
                f"retrying in {wait_seconds}s...",
                flush=True,
            )
            time.sleep(wait_seconds)
    raise RuntimeError(f"download failed after retries: {url}: {last_error}")


def extract_binary(archive: Path, archive_type: str, inner_path: str, dest: Path) -> None:
    dest.parent.mkdir(parents=True, exist_ok=True)

    if archive_type == "zip":
        with zipfile.ZipFile(archive, "r") as zf:
            member = inner_path.replace("\\", "/")
            try:
                with zf.open(member, "r") as src, dest.open("wb") as out:
                    shutil.copyfileobj(src, out)
            except KeyError as exc:
                raise RuntimeError(f"file not found in zip archive: {member}") from exc
    elif archive_type == "tar.gz":
        with tarfile.open(archive, "r:gz") as tf:
            member = inner_path.replace("\\", "/")
            info = tf.getmember(member)
            fobj = tf.extractfile(info)
            if fobj is None:
                raise RuntimeError(f"file not found in tar.gz archive: {member}")
            with fobj, dest.open("wb") as out:
                shutil.copyfileobj(fobj, out)
    else:
        raise RuntimeError(f"unsupported archiveType: {archive_type}")

    # Keep runtime binaries executable when used on Linux.
    os.chmod(dest, 0o755)


def main() -> int:
    args = parse_args()
    root = Path(__file__).resolve().parents[2]

    sources_path = (root / args.sources).resolve()
    manifest_path = (root / args.manifest).resolve()
    cache_dir = (root / args.cache_dir).resolve()
    update_script = root / "tools/runtime/update-runtime-manifest.py"

    sources = load_sources(sources_path)
    if args.target:
        sources = [s for s in sources if s.get("target") == args.target]
    if not sources:
        raise RuntimeError("no sources matched target filter")

    touched_targets: set[str] = set()
    for src in sources:
        validate_source(src)
        source_id = src["id"]
        target = src["target"]
        archive_type = src["archiveType"]
        url = src["url"]
        expected_sha = src["archiveSha256"].lower()
        archive_binary_path = src["archiveBinaryPath"]
        dest_path = (root / src["destPath"]).resolve()

        archive_path = choose_archive_path(cache_dir, url)
        if args.force_download or not archive_path.is_file():
            print(f"[INFO] downloading {source_id}: {url}", flush=True)
            download_archive(url, archive_path)
        else:
            print(f"[INFO] using cached archive for {source_id}: {archive_path}", flush=True)

        actual_sha = sha256_file(archive_path)
        if actual_sha != expected_sha:
            raise RuntimeError(
                f"archive sha256 mismatch for {source_id}: "
                f"expected {expected_sha}, got {actual_sha}"
            )
        print(f"[OK] archive checksum verified: {source_id}", flush=True)

        extract_binary(archive_path, archive_type, archive_binary_path, dest_path)
        print(f"[OK] installed runtime: {dest_path.relative_to(root)}", flush=True)
        touched_targets.add(target)

    for target in sorted(touched_targets):
        cmd = [
            sys.executable,
            str(update_script),
            "--target",
            target,
            "--manifest",
            str(manifest_path),
        ]
        subprocess.run(cmd, cwd=root, check=True)

    print("[OK] runtime sync complete", flush=True)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except RuntimeError as exc:
        print(f"[ERROR] {exc}", file=sys.stderr, flush=True)
        raise SystemExit(1)
