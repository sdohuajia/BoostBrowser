# Boost Browser

A Wails/Go-based fingerprint browser with Chromium 146 kernel and built-in proxy tooling.

## Features

- Multi-profile management with isolated fingerprints
- Chromium 146 kernel + Google Chrome 148 fallback
- Built-in xray / sing-box proxy tooling
- Automatic update via GitHub Releases
- Multi-window operation sync (master/follower)

## Download

Get the latest installer from [Releases](https://github.com/sdohuajia/BoostBrowser/releases/latest).

## Build from source

Prerequisites: Go 1.22+, Node 20+, Wails CLI v2.

```bash
# Backend + frontend
wails build

# Full installer (requires <app-root>\ with chromium kernel)
powershell -ExecutionPolicy Bypass -File scripts\build_release.ps1
powershell -ExecutionPolicy Bypass -File scripts\build_installer.ps1
```

## Project layout

- `main.go` / `backend/` — Go backend (Wails)
- `frontend/src/` — React + TypeScript UI
- `scripts/` — PowerShell build / release scripts
- `build/windows/` — Windows installer assets

## License

See LICENSE.
