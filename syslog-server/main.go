package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Mordevolt/ha-syslog-server/syslog-server/internal/config"
	"github.com/Mordevolt/ha-syslog-server/syslog-server/internal/db"
	"github.com/Mordevolt/ha-syslog-server/syslog-server/internal/syslog"
	"github.com/Mordevolt/ha-syslog-server/syslog-server/internal/web"
)

func main() {
	log.Println("==================================================")
	log.Println("   Home Assistant Syslog Server (Go) v1.0.5      ")
	log.Println("==================================================")

	// 1. Load Configuration
	cfg := config.LoadConfig()
	log.Printf("[Main] Config: SyslogPort=%d, Retention=%d days, MaxDBSize=%d MB, RateLimit=%d/s, AgentAPI=%t",
		cfg.SyslogPort, cfg.RetentionDays, cfg.MaxDBSizeMB, cfg.RateLimitPerSec, cfg.EnableAgentAPI)

	// 2. Initialize Database with WAL & Batching
	database, err := db.Open(cfg.DBPath, cfg.RetentionDays, cfg.MaxDBSizeMB)
	if err != nil {
		log.Fatalf("[Main] Fatal: Failed to initialize database: %v", err)
	}
	log.Printf("[Main] SQLite database initialized at %s", cfg.DBPath)

	// 3. Initialize SSE Broker for live tailing
	broker := web.NewSSEBroker()

	// 4. Start Syslog UDP Server
	syslogServer := syslog.NewServer(cfg, database, broker)
	if err := syslogServer.Start(); err != nil {
		log.Fatalf("[Main] Fatal: Failed to start Syslog UDP server: %v", err)
	}

	// 5. Start HTTP Ingress Server (Dashboard & API)
	webServer := web.NewServer(cfg, database, broker)
	go func() {
		if err := webServer.Start(); err != nil {
			log.Printf("[Main] Web server stopped: %v", err)
		}
	}()

	// 6. Graceful Shutdown Handler
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	sig := <-sigChan
	log.Printf("[Main] Received signal %v, shutting down gracefully...", sig)

	_ = webServer.Stop()
	syslogServer.Stop()
	_ = database.Close()

	log.Println("[Main] Server stopped cleanly. Goodbye.")
}
