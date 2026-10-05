package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Mordevolt/ha-syslog-server/syslog-server/internal/config"
	"github.com/Mordevolt/ha-syslog-server/syslog-server/internal/db"
)

//go:embed ui/*
var uiFS embed.FS

type SSEBroker struct {
	clients   map[chan *db.LogEntry]bool
	mu        sync.Mutex
	broadcast chan *db.LogEntry
}

func NewSSEBroker() *SSEBroker {
	b := &SSEBroker{
		clients:   make(map[chan *db.LogEntry]bool),
		broadcast: make(chan *db.LogEntry, 500),
	}
	go b.run()
	return b
}

func (b *SSEBroker) run() {
	for entry := range b.broadcast {
		b.mu.Lock()
		for ch := range b.clients {
			select {
			case ch <- entry:
			default:
				// Client buffer full, skip to avoid blocking others
			}
		}
		b.mu.Unlock()
	}
}

func (b *SSEBroker) Broadcast(entry *db.LogEntry) {
	select {
	case b.broadcast <- entry:
	default:
	}
}

func (b *SSEBroker) Register() chan *db.LogEntry {
	ch := make(chan *db.LogEntry, 100)
	b.mu.Lock()
	b.clients[ch] = true
	b.mu.Unlock()
	return ch
}

func (b *SSEBroker) Unregister(ch chan *db.LogEntry) {
	b.mu.Lock()
	delete(b.clients, ch)
	close(ch)
	b.mu.Unlock()
}

type Server struct {
	cfg      *config.Config
	database *db.DB
	broker   *SSEBroker
	httpSrv  *http.Server
}

func NewServer(cfg *config.Config, database *db.DB, broker *SSEBroker) *Server {
	return &Server{
		cfg:      cfg,
		database: database,
		broker:   broker,
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Ingress Web Dashboard & standard API
	mux.HandleFunc("GET /", s.handleUI)
	mux.HandleFunc("GET /api/logs", s.wrapAPIEndpoint(s.handleGetLogs))
	mux.HandleFunc("GET /api/stream", s.wrapAPIEndpoint(s.handleStream))
	mux.HandleFunc("GET /api/stats", s.wrapAPIEndpoint(s.handleStats))
	mux.HandleFunc("GET /api/hosts", s.wrapAPIEndpoint(s.handleHosts))
	mux.HandleFunc("GET /api/export", s.wrapAPIEndpoint(s.handleExport))
	mux.HandleFunc("POST /api/clear", s.handleClearLogs)

	// AI Agent Endpoints (REST & MCP)
	mux.HandleFunc("GET /api/agent/summary", s.wrapAgentEndpoint(s.handleAgentSummary))
	mux.HandleFunc("OPTIONS /api/agent/summary", s.wrapAgentEndpoint(s.handleAgentSummary))
	mux.HandleFunc("GET /api/agent/tools", s.wrapAgentEndpoint(s.handleAgentTools))
	mux.HandleFunc("OPTIONS /api/agent/tools", s.wrapAgentEndpoint(s.handleAgentTools))
	mux.HandleFunc("GET /mcp", s.wrapAgentEndpoint(s.handleMCP))
	mux.HandleFunc("POST /mcp", s.wrapAgentEndpoint(s.handleMCP))
	mux.HandleFunc("OPTIONS /mcp", s.wrapAgentEndpoint(s.handleMCP))

	addr := fmt.Sprintf("0.0.0.0:%d", s.cfg.HTTPPort)
	s.httpSrv = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // 0 for streaming SSE
	}

	if s.cfg.EnableAgentAPI {
		log.Printf("[Web] AI Agent API is ENABLED (REST: /api/agent/summary, MCP: /mcp)")
	}
	log.Printf("[Web] HTTP dashboard listening on http://%s", addr)
	return s.httpSrv.ListenAndServe()
}

func (s *Server) checkAuth(r *http.Request) bool {
	// If accessed through Home Assistant Ingress, trust Supervisor proxy
	if r.Header.Get("X-Ingress-Path") != "" {
		return true
	}

	// If no token is configured, allow
	if s.cfg.APIToken == "" {
		return true
	}

	// Check Authorization header: "Bearer <token>"
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && parts[1] == s.cfg.APIToken {
			return true
		}
	}

	// Check X-API-Key header
	if r.Header.Get("X-API-Key") == s.cfg.APIToken {
		return true
	}

	// Check query param "?token=<token>"
	if r.URL.Query().Get("token") == s.cfg.APIToken {
		return true
	}

	return false
}

func (s *Server) wrapAPIEndpoint(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		if s.cfg.APIToken != "" && !s.checkAuth(r) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "Unauthorized: Invalid or missing API token"}`))
			return
		}

		handler(w, r)
	}
}

func (s *Server) wrapAgentEndpoint(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		if !s.cfg.EnableAgentAPI {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error": "AI Agent API is disabled in add-on configuration (enable_agent_api: false)"}`))
			return
		}

		if !s.checkAuth(r) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "Unauthorized: Invalid or missing API token"}`))
			return
		}

		handler(w, r)
	}
}

func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	// Serve index.html for root and any subpaths (SPA style)
	content, err := uiFS.ReadFile("ui/index.html")
	if err != nil {
		http.Error(w, "UI template not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(content)
}

func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := db.LogFilter{
		Search:   q.Get("search"),
		SourceIP: q.Get("ip"),
		Hostname: q.Get("host"),
		Tag:      q.Get("tag"),
		OrderBy:  q.Get("order_by"),
		OrderDir: q.Get("order_dir"),
		Limit:    100,
		Offset:   0,
	}

	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 {
		filter.Limit = l
	}
	if o, err := strconv.Atoi(q.Get("offset")); err == nil && o >= 0 {
		filter.Offset = o
	}
	if sVal, err := strconv.Atoi(q.Get("severity")); err == nil && sVal >= 0 && sVal <= 7 {
		filter.Severity = &sVal
	}
	if fromStr := q.Get("from"); fromStr != "" {
		if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
			filter.From = &t
		}
	}
	if toStr := q.Get("to"); toStr != "" {
		if t, err := time.Parse(time.RFC3339, toStr); err == nil {
			filter.To = &t
		}
	}

	entries, total, err := s.database.QueryLogs(filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"entries": entries,
		"total":   total,
		"limit":   filter.Limit,
		"offset":  filter.Offset,
	})
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	clientChan := s.broker.Register()
	defer s.broker.Unregister(clientChan)

	// Send initial ping
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case entry, ok := <-clientChan:
			if !ok {
				return
			}
			data, err := json.Marshal(entry)
			if err == nil {
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.database.GetStats()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

func (s *Server) handleHosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := s.database.GetUniqueHosts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(hosts)
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	format := q.Get("format")
	if format != "csv" {
		format = "raw"
	}

	filter := db.LogFilter{
		Search:   q.Get("search"),
		SourceIP: q.Get("ip"),
		Hostname: q.Get("host"),
		Tag:      q.Get("tag"),
		OrderBy:  q.Get("order_by"),
		OrderDir: q.Get("order_dir"),
	}
	if sVal, err := strconv.Atoi(q.Get("severity")); err == nil {
		filter.Severity = &sVal
	}
	if fromStr := q.Get("from"); fromStr != "" {
		if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
			filter.From = &t
		}
	}
	if toStr := q.Get("to"); toStr != "" {
		if t, err := time.Parse(time.RFC3339, toStr); err == nil {
			filter.To = &t
		}
	}

	filename := fmt.Sprintf("syslog_%s.%s", time.Now().Format("2006-01-02_150405"), format)
	if format == "raw" {
		filename = fmt.Sprintf("syslog_%s.txt", time.Now().Format("2006-01-02_150405"))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	_ = s.database.ExportLogs(filter, w, format)
}

func (s *Server) handleClearLogs(w http.ResponseWriter, r *http.Request) {
	// If outside Ingress, require configured APIToken (prevent unauthenticated LAN wipe)
	if r.Header.Get("X-Ingress-Path") == "" && s.cfg.APIToken == "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error": "Direct LAN database purge requires api_token to be configured"}`))
		return
	}
	if !s.checkAuth(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Unauthorized: Invalid or missing API token"}`))
		return
	}

	if err := s.database.ClearAll(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "All logs cleared successfully",
	})
}


func (s *Server) Stop() error {
	if s.httpSrv != nil {
		return s.httpSrv.Close()
	}
	return nil
}
