# Syslog Server for Home Assistant

[English](#english) | [Русский](#русский)

---

<a name="english"></a>
## English

### Description
**Syslog Server** is an ultra-lightweight, high-performance syslog receiver and log analyzer designed specifically for Home Assistant. It collects, parses, and visualizes system logs from Wi-Fi routers (Keenetic, MikroTik, OpenWrt, ASUS, TP-Link, UniFi, etc.) and local network devices.

### Key Features
- **Ultra-low resource usage:** Consumes only ~5–12 MB of RAM and ~0% CPU (written in Go).
- **Home Assistant DB Isolation:** Router logs are stored in a dedicated, optimized SQLite WAL database with in-memory batch writes, preventing flash drive wear and avoiding bloating `home-assistant_v2.db`.
- **Integrated Ingress Dashboard:** Seamlessly embedded into the Home Assistant sidebar with native authentication and SSL. No exposed external web ports required.
- **Real-Time Live Streaming:** Monitor Wi-Fi events (802.11k/v/r roaming, client handovers, DHCP leases, errors) instantly via Server-Sent Events (SSE).
- **Time Range & Search Filters:** Filter by presets (Last 15m, 1h, 6h, 24h, 7d), custom date ranges, hostname/IP, log level, and full text.
- **Multi-Level Security:**
  - IP Allowlist / CIDR subnet filtering.
  - Per-IP Token-Bucket Rate Limiter (anti-flood).
  - Packet size validation (up to 4 KB).
- **Automated Retention & Storage Safety:** Automatically purges records older than `retention_days` and enforces a hard database size cap (`max_db_size_mb`).
- **Manual Database Clear:** One-click database purging and vacuuming directly from the web interface.
- **Export Data:** Download filtered logs as **CSV** or **RAW text** files.
- **Bilingual Interface:** English by default with instant toggle to Russian.

---

### Router Configuration

In your router's web administration panel, navigate to the System Log / Syslog settings and configure:
* **Syslog Server IP:** IP address of your Home Assistant instance (e.g., `192.168.1.100`).
* **Port:** `514` (UDP protocol).
* **Facility / Level:** `Info` or `Debug`.

#### Popular Router Guides:
* **Keenetic:** Management -> Log -> Send to Syslog Server -> Enable -> IP: `Home_Assistant_IP`, Port: `514`.
* **MikroTik RouterOS:** System -> Logging -> Actions -> Add action with type: `remote`, remote-address: `Home_Assistant_IP`, remote-port: `514`. Then add a matching rule in Rules.
* **OpenWrt:** System -> System -> Logging -> External system log server: `Home_Assistant_IP`, External system log server port: `514`.
* **ASUS / TP-Link:** Administration -> System Log -> Remote Log Server -> IP: `Home_Assistant_IP`.

---

### Add-on Configuration Parameters

| Option | Default | Description |
| :--- | :--- | :--- |
| `syslog_port` | `514` | UDP port for incoming syslog messages. |
| `retention_days` | `14` | Number of days to keep logs before automatic pruning. |
| `max_db_size_mb` | `500` | Maximum SQLite database size in megabytes. |
| `rate_limit_per_sec` | `100` | Max messages per second accepted per IP address. |
| `allowed_hosts` | `[]` | List of trusted IP addresses or CIDR subnets (e.g., `192.168.1.1`, `192.168.1.0/24`). If empty, all local network IPs are allowed. |

---

<a name="русский"></a>
## Русский

### Описание
**Syslog Server** — это сверхлегковесный сервер приема и анализа системных логов с Wi-Fi роутеров, точек доступа и сетевых устройств, работающий внутри Home Assistant как изолированное дополнение (Add-on).

### Особенности
- **Минимальное потребление ресурсов:** занимает всего ~5–12 МБ ОЗУ и 0% CPU.
- **Полная изоляция от базы Home Assistant:** логи хранятся в отдельной базе SQLite (режим WAL с батчингом), не изнашивая диск и не раздувая базу умного дома.
- **Встроенный веб-интерфейс (Ingress):** открывается прямо в боковом меню Home Assistant под единой авторизацией.
- **Live Stream в реальном времени:** отслеживание событий Wi-Fi через Server-Sent Events (SSE).
- **Фильтрация по времени:** готовые пресеты (15 минут, 1 час, 6 часов, 24 часа, 7 дней) и произвольный выбор диапазона дат.
- **Ручная очистка базы:** возможность очистить базу данных в один клик из веб-интерфейса.
- **Многоуровневая безопасность:** белый список IP/CIDR, ограничение частоты запросов (Rate Limiting), лимит на размер пакета.
- **Двуязычный интерфейс:** английский по умолчанию с возможностью переключения на русский в заголовке панели.
