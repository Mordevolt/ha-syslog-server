# Changelog

## 1.0.4
- **Fixed Timestamp Discrepancy & Timezones:** Unified database storage and live SSE streaming to canonical UTC packet arrival timestamps, preventing double-offset addition (+3h skew) in local browser rendering.
- **Configurable Page Size:** Added a dropdown selector to display 50, 100, 250, 500, or 1000 logs per page, persisted in local storage.
- **Extended Query Limit:** Increased backend query limit up to 5000 records for larger batched views and exports.

## 1.0.3
- **Interactive Column Sorting:** Click any column header (`Timestamp`, `Level`, `Router / IP`, `Tag / Process`) to sort ascending or descending (▲/▼). Allows sorting logs from oldest to newest and vice versa.
- **SQL Sorting Engine:** Added dynamic, SQL-injection safe `order_by` and `order_dir` parameters to backend query and export APIs.

## 1.0.2
- **Fixed Timestamp Parsing & Display:** Resolved SQLite timestamp scanning where dates displayed as `01.01.1` (year 1) due to RFC3339 layout mismatch; added multi-format driver parser (`parseDBTime`) and local timezone preservation (`time.Local`).
- **Enhanced Syslog RFC Parser:** Added full RFC 5424 ISO timestamp support, Russian month abbreviations, and single-digit day handling.
- **Router / IP Column Display:** The router IP address is now displayed prominently by default (`source_ip`), with the device hostname shown alongside if available.

## 1.0.1
- **Fixed shutdown panic:** Added `sync.Once` guards to ensure graceful and idempotent termination of DB and UDP listeners when stopping the add-on.
- **Bilingual Interface:** English is now the default language for the UI and documentation, with an instant toggle switch for Russian (persisted in local storage).
- **Time Range Filtering:** Added time range presets (Last 15m, 1h, 6h, 24h, 7d) and custom datetime picker for filtering and exporting logs.
- **Manual Database Clear:** Added a button in the Web UI to manually purge and vacuum the SQLite database with confirmation.
- **Export by Time Range:** Extended CSV and RAW exports to respect selected time filters.

## 1.0.0
- Initial release of the ultra-lightweight Go Syslog Server for Home Assistant.
- UDP (port 514) receiver supporting RFC 3164 and RFC 5424 formats.
- High-performance SQLite WAL storage with in-memory buffering to protect flash storage.
- Automated retention by retention days and max database size limit.
- Real-time Ingress web dashboard with live SSE streaming, search, and CSV/RAW export.
- Security controls: IP allowlist, token-bucket rate limiting, and 4KB packet size limits.
- Home Assistant graphical configuration schema.
