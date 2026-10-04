package syslog

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/Mordevolt/ha-syslog-server/syslog-server/internal/config"
	"github.com/Mordevolt/ha-syslog-server/syslog-server/internal/db"
)

type Broadcaster interface {
	Broadcast(entry *db.LogEntry)
}

type rateLimiter struct {
	tokens     int
	lastRefill time.Time
}

type Server struct {
	cfg         *config.Config
	database    *db.DB
	broadcaster Broadcaster
	conn        *net.UDPConn
	stopChan    chan struct{}
	limiters    map[string]*rateLimiter
	limiterMu   sync.Mutex
	stopOnce    sync.Once
}

func NewServer(cfg *config.Config, database *db.DB, broadcaster Broadcaster) *Server {
	return &Server{
		cfg:         cfg,
		database:    database,
		broadcaster: broadcaster,
		stopChan:    make(chan struct{}),
		limiters:    make(map[string]*rateLimiter),
	}
}

func (s *Server) Start() error {
	addr := net.UDPAddr{
		Port: s.cfg.SyslogPort,
		IP:   net.ParseIP("0.0.0.0"),
	}

	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		return fmt.Errorf("failed to bind UDP port %d: %w", s.cfg.SyslogPort, err)
	}
	s.conn = conn

	log.Printf("[Syslog] Listening on UDP port %d (Allowed hosts: %v)", s.cfg.SyslogPort, s.cfg.AllowedHosts)

	go s.listenLoop()
	go s.cleanupLimiters()

	return nil
}

func (s *Server) listenLoop() {
	buf := make([]byte, 4096) // Max 4KB per UDP packet

	for {
		select {
		case <-s.stopChan:
			return
		default:
		}

		n, remoteAddr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-s.stopChan:
				return
			default:
				log.Printf("[Syslog] UDP read error: %v", err)
				continue
			}
		}

		if n == 0 {
			continue
		}

		remoteIP := remoteAddr.IP.String()

		// 1. IP Allowlist Security Check
		if !s.cfg.IsIPAllowed(remoteIP) {
			// Dropped silently to avoid log spam & CPU overhead
			continue
		}

		// 2. Rate Limiting Check (Token Bucket)
		if !s.allowRate(remoteIP) {
			// Dropped due to rate limit excess
			continue
		}

		// 3. Parse Syslog Packet
		packetCopy := make([]byte, n)
		copy(packetCopy, buf[:n])

		entry := ParseSyslogPacket(packetCopy, remoteIP)

		// 4. Send to DB batch writer
		s.database.Insert(entry)

		// 5. Broadcast to live SSE stream if anyone is watching
		if s.broadcaster != nil {
			s.broadcaster.Broadcast(entry)
		}
	}
}

func (s *Server) allowRate(ip string) bool {
	s.limiterMu.Lock()
	defer s.limiterMu.Unlock()

	now := time.Now()
	rl, exists := s.limiters[ip]
	if !exists {
		s.limiters[ip] = &rateLimiter{
			tokens:     s.cfg.RateLimitPerSec - 1,
			lastRefill: now,
		}
		return true
	}

	// Refill tokens based on elapsed time
	elapsed := now.Sub(rl.lastRefill)
	if elapsed >= time.Second {
		rl.tokens = s.cfg.RateLimitPerSec
		rl.lastRefill = now
	}

	if rl.tokens > 0 {
		rl.tokens--
		return true
	}

	return false
}

func (s *Server) cleanupLimiters() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.limiterMu.Lock()
			now := time.Now()
			for ip, rl := range s.limiters {
				if now.Sub(rl.lastRefill) > 10*time.Minute {
					delete(s.limiters, ip)
				}
			}
			s.limiterMu.Unlock()
		}
	}
}

func (s *Server) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopChan)
		if s.conn != nil {
			s.conn.Close()
		}
	})
}

