# AGENTS.md — AI Agent & Contributor Guide

Welcome! This document provides operational context, architectural invariants, code conventions, and development guidelines for AI coding agents (such as Google Antigravity, Claude, Codex, Cursor, Copilot) and human contributors working on this repository.

---

## 1. Project Context & Purpose

* **Repository:** `Mordevolt/ha-syslog-server`
* **Target Environment:** [Home Assistant](https://www.home-assistant.io/) Add-on (Supervised / OS).
* **Language & Runtime:** Go (Golang) 1.23+, pure Go (no CGo).
* **Primary Role:** Ultra-lightweight, standalone Syslog server designed to receive, parse, store, filter, and tail system logs from Wi-Fi mesh routers (Keenetic, MikroTik, OpenWrt, ASUS, TP-Link, UniFi, etc.) and local network equipment.
* **Storage Location:** `/data/syslog.db` (persistent Docker data volume provided by Home Assistant Supervisor).

---

## 2. Inviolable Architectural Principles

When writing or refactoring code in this project, you **MUST** strictly adhere to the following rules:

### A. Zero Resource Overhead
* The entire service must consume **< 15 MB of RAM** and virtually **0% CPU** during idle and steady-state ingestion.
* Do **NOT** introduce heavy frameworks (no Gin, Echo, Fiber, ORMs, or GORM). Use Go standard library `net/http` and `database/sql`.

### B. Flash Storage & Database Isolation
* **Never write to or touch `home-assistant_v2.db`.** All logs are strictly confined to `/data/syslog.db`.
* **Zero disk thrashing:** Syslog messages must never trigger an immediate `fsync` or individual SQLite transaction per message. All incoming records go into an in-memory batch buffer (`writeChan`) and are committed in batches (100 items or every 2 seconds).
* SQLite **must** run in WAL mode with `synchronous = NORMAL` to avoid wearing out SD cards/eMMC flash on Raspberry Pi and home servers.

### C. No CGo (Pure Go Cross-Compilation)
* Use `modernc.org/sqlite` instead of `mattn/go-sqlite3`.
* Home Assistant users run on `amd64`, `aarch64` (Raspberry Pi 4/5), `armv7` (Raspberry Pi 3/32-bit), and `armhf`. The Go binary must compile cleanly across all architectures without native C cross-compilers.

### D. Home Assistant Ingress URL Compatibility
* **CRITICAL:** The Web UI is reverse-proxied by Home Assistant Ingress behind dynamic paths (e.g., `/api/hassio_ingress/<session-token>/`).
* **NEVER use absolute URL paths** in `index.html` or client-side JavaScript (e.g. do **NOT** use `/api/logs`, `/api/stream`, `/icon.png`).
* **ALWAYS use relative paths** (e.g. `api/logs`, `api/stream`, `api/export`, `api/clear`).

### E. Idempotent & Safe Shutdown
* All server and database shutdown methods (`DB.Close()`, `Server.Stop()`) **must be guarded with `sync.Once`**.
* Never close Go channels (`stopChan`) more than once to prevent runtime panics on add-on stop/restart.

### F. Bilingual Support (EN Default + RU)
* English (`en`) is the default language for UI, documentation, and configuration metadata.
* Russian (`ru`) must always be supported as an instant toggle in the Web UI and as a localized section in docs.

---

## 3. Directory Layout

```
.
├── .gitignore
├── AGENTS.md                          # This file (Agent instructions & guidelines)
├── ARCHITECTURE.md                    # Deep architectural specification & design
├── LICENSE                            # MIT License
├── README.md                          # Main repository documentation (EN & RU)
├── repository.yaml                    # Home Assistant Add-on repository manifest
└── syslog-server/                     # Add-on root folder
    ├── build.yaml                     # Supervisor multi-arch build matrix
    ├── config.yaml                    # Add-on metadata, schema, ports, and options
    ├── CHANGELOG.md                   # Release history
    ├── DOCS.md                        # Documentation shown inside Home Assistant UI
    ├── Dockerfile                     # Multi-stage Alpine container build
    ├── go.mod                         # Go module dependencies
    ├── go.sum                         # Checksums
    ├── icon.png / icon.svg            # Store icon (128x128)
    ├── logo.png / logo.svg            # Store header logo
    ├── main.go                        # Entry point & signal handling
    ├── translations/
    │   ├── en.yaml                    # HA Configuration schema labels (English)
    │   └── ru.yaml                    # HA Configuration schema labels (Russian)
    └── internal/
        ├── config/                    # Configuration loader & CIDR matcher
        │   └── config.go
        ├── db/                        # SQLite WAL, in-memory batching, retention, queries
        │   └── db.go
        ├── syslog/                    # UDP 514 receiver, RFC parsers, rate limiter
        │   ├── parser.go
        │   └── server.go
        └── web/                       # Ingress HTTP server & live stream broker
            ├── server.go              # REST API & SSE handler
            └── ui/
                └── index.html         # Embedded SPA dashboard (HTML/CSS/JS)
```

---

## 4. Key Workflows & Recipes

### 1. Modifying Configuration Options
* If you add an option:
  1. Add it to `syslog-server/internal/config/config.go` (`Config` struct & default fallback).
  2. Add the schema definition to `syslog-server/config.yaml` under `schema:` and `options:`.
  3. Add localized descriptions in `syslog-server/translations/en.yaml` and `ru.yaml`.
  4. Document the option in `README.md` and `syslog-server/DOCS.md`.

### 2. Modifying Database Schema or Queries
* The SQLite schema is initialized in `syslog-server/internal/db/db.go` (`Open()`).
* If changing tables, ensure index coverage (`idx_syslog_timestamp`, `idx_syslog_source_ip`, etc.).
* SQLite queries must always use prepared statements or parameter placeholders (`?`) to prevent SQL injection.

### 3. Modifying the Web Dashboard
* The UI is a single embedded HTML file located at `syslog-server/internal/web/ui/index.html`.
* It is embedded into the Go binary at compile-time using `//go:embed ui/index.html`.
* It does not require Node.js, Webpack, or npm. Keep it in pure vanilla JS and CSS.
* When adding UI strings, add entries to both `i18n.en` and `i18n.ru` in the JavaScript object and update `applyLanguage()`.

### 4. Release & Version Bump Checklist
When releasing a new version (e.g. `v1.0.2`):
1. Update version in `syslog-server/config.yaml` (`version: "1.0.2"`).
2. Update version print in `syslog-server/main.go`.
3. Update version badge in `syslog-server/internal/web/ui/index.html`.
4. Add release notes to `syslog-server/CHANGELOG.md`.
5. Stage and commit: `git commit -m "v1.0.2: <summary>"`.
6. Push to remote: `git push origin main`.

---

## 5. Security Checklist for Agents

* **Network Exposure:** The add-on only exposes UDP port `514` to the host network (`ports: 514/udp: 514`). The HTTP server (port `8099`) **MUST NEVER** be mapped directly in `ports:`; it is strictly accessed through Home Assistant Ingress via Docker bridge network.
* **Input Sanitization:** All text rendered into the DOM must pass through `escapeHtml()` to eliminate XSS risks.
* **Rate Limiting:** Protect UDP ingestion with token-bucket limits to avoid CPU/memory starvation if a malfunctioning device floods port 514.
* **Allowed Hosts:** IP filtering in `syslog-server/internal/config/config.go` supports both exact IPs and CIDR masks (e.g. `192.168.1.0/24`). Always validate against `s.cfg.IsIPAllowed(clientIP)`.
