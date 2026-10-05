package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Mordevolt/ha-syslog-server/syslog-server/internal/db"
)

// MCP JSON-RPC 2.0 Types
type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *mcpError   `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// handleAgentSummary provides a concise, high-signal health summary for LLM context windows
func (s *Server) handleAgentSummary(w http.ResponseWriter, r *http.Request) {
	minutes := 60
	if mStr := r.URL.Query().Get("minutes"); mStr != "" {
		if m, err := strconv.Atoi(mStr); err == nil && m > 0 {
			minutes = m
		}
	}

	summary, err := s.database.GetAgentSummary(minutes)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "Failed to generate summary: %v"}`, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(summary)
}

// handleAgentTools returns OpenAI / Hermes compatible function calling tool schemas
func (s *Server) handleAgentTools(w http.ResponseWriter, r *http.Request) {
	tools := []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "get_syslog_health",
				"description": "Get high-level syslog summary, health anomalies, error counts, and top failing components for local network equipment (routers, APs, switches).",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"minutes": map[string]interface{}{
							"type":        "integer",
							"description": "Lookback time window in minutes (default: 60)",
						},
					},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "search_syslog",
				"description": "Search detailed syslog entries with filters by query text, severity, router IP, and limit.",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "Text or tag to search for (e.g. pppoe, dhcp, link)",
						},
						"severity": map[string]interface{}{
							"type":        "integer",
							"description": "Maximum severity: 3 (Error), 4 (Warning), 6 (Info), 7 (Debug)",
						},
						"host": map[string]interface{}{
							"type":        "string",
							"description": "Filter by router IP address",
						},
						"limit": map[string]interface{}{
							"type":        "integer",
							"description": "Number of logs to return (default: 20, max: 100)",
						},
					},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "get_monitored_hosts",
				"description": "List all active router and network device IP addresses sending logs to the syslog server.",
				"parameters": map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "get_server_stats",
				"description": "Get total log count, database size, and retention settings.",
				"parameters": map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"tools": tools,
	})
}

// handleMCP handles Model Context Protocol (MCP) JSON-RPC 2.0 requests
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if r.Method == http.MethodGet {
		// Server Info on GET
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"name":        "ha-syslog-server",
			"version":     "1.0.5",
			"mcp_version": "2024-11-05",
			"endpoints": map[string]string{
				"rpc":     "/mcp",
				"summary": "/api/agent/summary",
				"tools":   "/api/agent/tools",
			},
		})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
	if err != nil {
		_ = json.NewEncoder(w).Encode(mcpResponse{
			JSONRPC: "2.0",
			Error:   &mcpError{Code: -32700, Message: "Parse error"},
		})
		return
	}

	var req mcpRequest
	if err := json.Unmarshal(body, &req); err != nil {
		_ = json.NewEncoder(w).Encode(mcpResponse{
			JSONRPC: "2.0",
			Error:   &mcpError{Code: -32700, Message: "Invalid JSON-RPC request"},
		})
		return
	}

	switch req.Method {
	case "initialize":
		_ = json.NewEncoder(w).Encode(mcpResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
				"serverInfo": map[string]interface{}{
					"name":    "ha-syslog-server",
					"version": "1.0.5",
				},
			},
		})

	case "notifications/initialized":
		// Notification, no content required
		w.WriteHeader(http.StatusOK)

	case "tools/list":
		_ = json.NewEncoder(w).Encode(mcpResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"tools": []map[string]interface{}{
					{
						"name":        "get_syslog_health",
						"description": "Get high-level syslog summary, health anomalies, error counts, and top failing components for local network equipment (routers, APs, switches).",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"minutes": map[string]interface{}{
									"type":        "integer",
									"description": "Lookback time window in minutes (default: 60)",
								},
							},
						},
					},
					{
						"name":        "search_syslog",
						"description": "Search detailed syslog entries with filters by query text, severity, router IP, and limit.",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"query": map[string]interface{}{
									"type":        "string",
									"description": "Text or tag to search for (e.g. pppoe, dhcp, link)",
								},
								"severity": map[string]interface{}{
									"type":        "integer",
									"description": "Maximum severity: 3 (Error), 4 (Warning), 6 (Info), 7 (Debug)",
								},
								"host": map[string]interface{}{
									"type":        "string",
									"description": "Filter by router IP address",
								},
								"limit": map[string]interface{}{
									"type":        "integer",
									"description": "Number of logs to return (default: 20, max: 100)",
								},
							},
						},
					},
					{
						"name":        "get_monitored_hosts",
						"description": "List all active router and network device IP addresses sending logs to the syslog server.",
						"inputSchema": map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{},
						},
					},
					{
						"name":        "get_server_stats",
						"description": "Get total log count, database size, and retention settings.",
						"inputSchema": map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{},
						},
					},
				},
			},
		})

	case "tools/call":
		var callParams mcpToolCallParams
		if err := json.Unmarshal(req.Params, &callParams); err != nil {
			_ = json.NewEncoder(w).Encode(mcpResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &mcpError{Code: -32602, Message: "Invalid parameters"},
			})
			return
		}

		resultText, err := s.executeAgentTool(callParams.Name, callParams.Arguments)
		if err != nil {
			_ = json.NewEncoder(w).Encode(mcpResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]interface{}{
					"isError": true,
					"content": []map[string]interface{}{
						{
							"type": "text",
							"text": fmt.Sprintf("Error executing %s: %v", callParams.Name, err),
						},
					},
				},
			})
			return
		}

		_ = json.NewEncoder(w).Encode(mcpResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": resultText,
					},
				},
			},
		})

	default:
		_ = json.NewEncoder(w).Encode(mcpResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &mcpError{Code: -32601, Message: fmt.Sprintf("Method not found: %s", req.Method)},
		})
	}
}

func (s *Server) executeAgentTool(name string, args map[string]interface{}) (string, error) {
	switch name {
	case "get_syslog_health":
		minutes := 60
		if mVal, ok := args["minutes"]; ok {
			if mFloat, ok := mVal.(float64); ok {
				minutes = int(mFloat)
			}
		}
		summary, err := s.database.GetAgentSummary(minutes)
		if err != nil {
			return "", err
		}
		data, _ := json.MarshalIndent(summary, "", "  ")
		return string(data), nil

	case "search_syslog":
		filter := db.LogFilter{
			Limit:    20,
			OrderBy:  "timestamp",
			OrderDir: "desc",
		}
		if q, ok := args["query"].(string); ok {
			filter.Search = strings.TrimSpace(q)
		}
		if h, ok := args["host"].(string); ok {
			filter.SourceIP = strings.TrimSpace(h)
		}
		if sev, ok := args["severity"].(float64); ok {
			sInt := int(sev)
			filter.Severity = &sInt
		}
		if lim, ok := args["limit"].(float64); ok && lim > 0 {
			filter.Limit = int(lim)
			if filter.Limit > 100 {
				filter.Limit = 100
			}
		}

		entries, total, err := s.database.QueryLogs(filter)
		if err != nil {
			return "", err
		}

		type resultPayload struct {
			TotalFound int64          `json:"total_found"`
			Returned   int            `json:"returned"`
			Logs       []*db.LogEntry `json:"logs"`
		}
		res := resultPayload{
			TotalFound: total,
			Returned:   len(entries),
			Logs:       entries,
		}
		data, _ := json.MarshalIndent(res, "", "  ")
		return string(data), nil

	case "get_monitored_hosts":
		hosts, err := s.database.GetUniqueHosts()
		if err != nil {
			return "", err
		}
		data, _ := json.MarshalIndent(map[string]interface{}{"hosts": hosts}, "", "  ")
		return string(data), nil

	case "get_server_stats":
		stats, err := s.database.GetStats()
		if err != nil {
			return "", err
		}
		data, _ := json.MarshalIndent(stats, "", "  ")
		return string(data), nil

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}
