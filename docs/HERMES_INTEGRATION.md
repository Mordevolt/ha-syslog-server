# AI Agent (Hermes) Integration Guide

This guide explains how to connect local AI agents (such as [Hermes Agent](https://github.com/NousResearch), Claude, Cursor, LangChain, or custom Python scripts) to **Home Assistant Syslog Server** to inspect router logs, detect network drops, and report anomalies.

---

## 1. Quick Setup in Home Assistant

The AI Agent API is **completely disabled by default** for maximum security. To enable it:

1. In Home Assistant, navigate to **Settings** → **Add-ons** → **Syslog Server**.
2. Open the **Configuration** tab:
   - Toggle **`enable_agent_api`** to `true`.
   - Set an **`api_token`** (e.g. `secret-hermes-token-xyz`).
   - Click **Save**.
3. Scroll down to the **Network** card on the add-on page:
   - Set the port mapping for `8099/tcp` to `8099` (or another host port of your choice).
   - Click **Save**.
4. Restart the add-on.

> [!TIP]
> If you do not map port `8099/tcp` in the Network tab, the server will **not** be accessible from the LAN, remaining strictly confined to Home Assistant Ingress.

---

## 2. Available Endpoints

| Endpoint | Method | Description |
| :--- | :--- | :--- |
| `/api/agent/summary` | `GET` | **Recommended for LLMs:** High-signal health digest (errors, top failing tags, affected hosts) in < 500 tokens. |
| `/api/agent/tools` | `GET` | Function-calling tool schemas formatted for Hermes / OpenAI function-calling models. |
| `/mcp` | `POST` / `GET` | **Model Context Protocol (MCP)** JSON-RPC 2.0 endpoint (supports `tools/list` and `tools/call`). |
| `/api/logs` | `GET` | Full log search with filters (`search`, `ip`, `severity`, `from`, `to`, `limit`, `order_by`). |
| `/api/hosts` | `GET` | List of all monitored router / device IP addresses. |
| `/api/stats` | `GET` | High-level statistics (total logs, database size, retention). |
| `/api/stream` | `GET` | Server-Sent Events (SSE) live streaming log feed. |

### Authentication

All external requests require the Bearer token configured in `api_token`:

```bash
# Header authentication (Standard)
curl -H "Authorization: Bearer <api_token>" http://<HOME_ASSISTANT_IP>:8099/api/agent/summary?minutes=60

# Or via X-API-Key header
curl -H "X-API-Key: <api_token>" http://<HOME_ASSISTANT_IP>:8099/api/agent/summary?minutes=60

# Or via query parameter
curl "http://<HOME_ASSISTANT_IP>:8099/api/agent/summary?minutes=60&token=<api_token>"
```

---

## 3. High-Signal Health Digest (`/api/agent/summary`)

Raw syslog streams can easily consume 50,000+ tokens and cause context overflow in LLMs. The `/api/agent/summary` endpoint pre-aggregates errors and anomalies directly inside SQLite:

```json
{
  "window_minutes": 60,
  "total_records": 1842,
  "error_count": 4,
  "warning_count": 18,
  "top_error_tags": [
    { "tag": "pppoe", "count": 3 },
    { "tag": "dhcpd", "count": 1 }
  ],
  "affected_hosts": [
    "192.168.1.1"
  ],
  "recent_critical_samples": [
    {
      "id": 1840,
      "timestamp": "2026-10-05T21:40:12Z",
      "severity": 3,
      "severity_name": "error",
      "source_ip": "192.168.1.1",
      "hostname": "Keenetic-Ultra",
      "tag": "pppoe",
      "message": "Connection terminated: Peer not responding"
    }
  ]
}
```

---

## 4. Hermes Agent Function Calling Definition

If your Hermes instance uses OpenAI-compatible function calling, add this tool definition:

```json
{
  "name": "get_syslog_health",
  "description": "Checks the health of network routers and devices via Syslog Server. Returns total errors, top failing tags (e.g., pppoe, dhcp), affected router IPs, and recent critical error messages.",
  "parameters": {
    "type": "object",
    "properties": {
      "minutes": {
        "type": "integer",
        "description": "Time window in minutes to look back (default: 60)",
        "default": 60
      }
    }
  }
}
```

### Python Implementation for Hermes Tool:

```python
import os
import requests

HA_HOST = os.getenv("HA_HOST", "http://192.168.1.100:8099")
API_TOKEN = os.getenv("SYSLOG_API_TOKEN", "secret-hermes-token-xyz")

def get_syslog_health(minutes: int = 60) -> dict:
    """Queries the Home Assistant Syslog Server for network errors and anomalies."""
    headers = {"Authorization": f"Bearer {API_TOKEN}"}
    response = requests.get(
        f"{HA_HOST}/api/agent/summary",
        params={"minutes": minutes},
        headers=headers,
        timeout=10
    )
    response.raise_for_status()
    return response.json()

def search_syslog(query: str = "", severity: int = 4, host: str = "", limit: int = 20) -> list:
    """Searches detailed log entries from the Syslog Server."""
    headers = {"Authorization": f"Bearer {API_TOKEN}"}
    params = {"limit": limit, "order_by": "timestamp", "order_dir": "desc"}
    if query:
        params["search"] = query
    if host:
        params["ip"] = host
    if severity:
        params["severity"] = severity
    
    response = requests.get(
        f"{HA_HOST}/api/logs",
        params=params,
        headers=headers,
        timeout=10
    )
    response.raise_for_status()
    return response.json().get("entries", [])
```

---

## 5. Model Context Protocol (MCP) Setup

If your Hermes setup or desktop client connects via MCP:

```json
{
  "mcpServers": {
    "syslog": {
      "url": "http://192.168.1.100:8099/mcp",
      "headers": {
        "Authorization": "Bearer secret-hermes-token-xyz"
      }
    }
  }
}
```

The server automatically registers the following tools:
* `get_syslog_health(minutes)`
* `search_syslog(query, severity, host, limit)`
* `get_monitored_hosts()`
* `get_server_stats()`

---

## 6. Security Guarantees

* **Completely Isolated:** The add-on runs in its own lightweight container. It has no access to Home Assistant tokens, database files, or other add-ons.
* **Default Off:** The feature is disabled (`enable_agent_api: false`) until explicitly enabled in settings.
* **Port Isolation:** Port `8099` is closed to the LAN unless explicitly assigned a port number in the Home Assistant network settings.
* **Destructive Action Protection:** Database purge (`/api/clear`) is strictly forbidden for LAN requests unless authenticated with the configured `api_token`.
