package config

import (
	"encoding/json"
	"log"
	"net"
	"os"
	"strings"
)

type Config struct {
	SyslogPort      int      `json:"syslog_port"`
	RetentionDays   int      `json:"retention_days"`
	MaxDBSizeMB     int      `json:"max_db_size_mb"`
	RateLimitPerSec int      `json:"rate_limit_per_sec"`
	AllowedHosts    []string `json:"allowed_hosts"`
	DBPath          string   `json:"db_path"`
	HTTPPort        int      `json:"http_port"`

	allowedIPs []*net.IPNet
}

func LoadConfig() *Config {
	cfg := &Config{
		SyslogPort:      514,
		RetentionDays:   14,
		MaxDBSizeMB:     500,
		RateLimitPerSec: 100,
		AllowedHosts:    []string{},
		DBPath:          "/data/syslog.db",
		HTTPPort:        8099,
	}

	// Home Assistant options file
	const haOptionsPath = "/data/options.json"
	if data, err := os.ReadFile(haOptionsPath); err == nil {
		if err := json.Unmarshal(data, cfg); err != nil {
			log.Printf("[Config] Warning: Failed to parse %s: %v. Using defaults.", haOptionsPath, err)
		} else {
			log.Printf("[Config] Loaded configuration from %s", haOptionsPath)
		}
	} else {
		// Fallback for local testing if /data doesn't exist
		if _, err := os.Stat("/data"); os.IsNotExist(err) {
			cfg.DBPath = "./data/syslog.db"
		}
	}

	if cfg.SyslogPort <= 0 {
		cfg.SyslogPort = 514
	}
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = 14
	}
	if cfg.MaxDBSizeMB <= 0 {
		cfg.MaxDBSizeMB = 500
	}
	if cfg.RateLimitPerSec <= 0 {
		cfg.RateLimitPerSec = 100
	}
	if cfg.HTTPPort <= 0 {
		cfg.HTTPPort = 8099
	}

	cfg.compileAllowedHosts()
	return cfg
}

func (c *Config) compileAllowedHosts() {
	c.allowedIPs = make([]*net.IPNet, 0, len(c.AllowedHosts))
	for _, entry := range c.AllowedHosts {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			_, ipNet, err := net.ParseCIDR(entry)
			if err == nil {
				c.allowedIPs = append(c.allowedIPs, ipNet)
			} else {
				log.Printf("[Config] Invalid CIDR in allowed_hosts: %s", entry)
			}
		} else {
			ip := net.ParseIP(entry)
			if ip != nil {
				mask := net.CIDRMask(32, 32)
				if ip.To4() == nil {
					mask = net.CIDRMask(128, 128)
				}
				c.allowedIPs = append(c.allowedIPs, &net.IPNet{IP: ip, Mask: mask})
			} else {
				log.Printf("[Config] Invalid IP in allowed_hosts: %s", entry)
			}
		}
	}
}

func (c *Config) IsIPAllowed(ipStr string) bool {
	if len(c.allowedIPs) == 0 {
		return true // Empty allowlist allows all
	}

	// Strip port if present
	host := ipStr
	if h, _, err := net.SplitHostPort(ipStr); err == nil {
		host = h
	}

	parsedIP := net.ParseIP(host)
	if parsedIP == nil {
		return false
	}

	for _, ipNet := range c.allowedIPs {
		if ipNet.Contains(parsedIP) {
			return true
		}
	}
	return false
}
