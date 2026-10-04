# Changelog

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
