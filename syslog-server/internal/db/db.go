package db

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type LogEntry struct {
	ID           int64     `json:"id"`
	Timestamp    time.Time `json:"timestamp"`
	Facility     int       `json:"facility"`
	Severity     int       `json:"severity"`
	SeverityName string    `json:"severity_name"`
	SourceIP     string    `json:"source_ip"`
	Hostname     string    `json:"hostname"`
	Tag          string    `json:"tag"`
	Message      string    `json:"message"`
}

type LogFilter struct {
	Search   string
	Severity *int
	SourceIP string
	Hostname string
	Tag      string
	From     *time.Time
	To       *time.Time
	Limit    int
	Offset   int
}

type Stats struct {
	TotalLogs     int64   `json:"total_logs"`
	DBSizeBytes   int64   `json:"db_size_bytes"`
	DBSizeMB      float64 `json:"db_size_mb"`
	RetentionDays int     `json:"retention_days"`
	MaxDBSizeMB   int     `json:"max_db_size_mb"`
	UniqueHosts   int     `json:"unique_hosts"`
	UptimeSec     int64   `json:"uptime_sec"`
}

type DB struct {
	db            *sql.DB
	dbPath        string
	writeChan     chan *LogEntry
	retentionDays int
	maxDBSizeMB   int
	startTime     time.Time
	mu            sync.RWMutex
	stopChan      chan struct{}
	closeOnce     sync.Once
}

func Open(dbPath string, retentionDays, maxDBSizeMB int) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	// Open with pragmas for high performance & durability
	dsn := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)", dbPath)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	sqlDB.SetMaxOpenConns(1) // SQLite works best with 1 writer connection

	schema := `
	CREATE TABLE IF NOT EXISTS syslog_entries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		facility INTEGER,
		severity INTEGER,
		severity_name TEXT,
		source_ip TEXT,
		hostname TEXT,
		tag TEXT,
		message TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_syslog_timestamp ON syslog_entries(timestamp);
	CREATE INDEX IF NOT EXISTS idx_syslog_source_ip ON syslog_entries(source_ip);
	CREATE INDEX IF NOT EXISTS idx_syslog_severity ON syslog_entries(severity);
	CREATE INDEX IF NOT EXISTS idx_syslog_tag ON syslog_entries(tag);
	`
	if _, err := sqlDB.Exec(schema); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to init db schema: %w", err)
	}

	d := &DB{
		db:            sqlDB,
		dbPath:        dbPath,
		writeChan:     make(chan *LogEntry, 5000),
		retentionDays: retentionDays,
		maxDBSizeMB:   maxDBSizeMB,
		startTime:     time.Now(),
		stopChan:      make(chan struct{}),
	}

	go d.batchWriter()
	go d.retentionWorker()

	return d, nil
}

func (d *DB) Insert(entry *LogEntry) {
	select {
	case d.writeChan <- entry:
	default:
		log.Printf("[DB] Warning: Write buffer full, dropping log entry")
	}
}

func (d *DB) batchWriter() {
	const batchSize = 100
	const flushInterval = 2 * time.Second

	batch := make([]*LogEntry, 0, batchSize)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := d.writeBatch(batch); err != nil {
			log.Printf("[DB] Batch insert error: %v", err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-d.stopChan:
			flush()
			return
		case entry := <-d.writeChan:
			batch = append(batch, entry)
			if len(batch) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (d *DB) writeBatch(entries []*LogEntry) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO syslog_entries (timestamp, facility, severity, severity_name, source_ip, hostname, tag, message)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range entries {
		_, err := stmt.ExecContext(ctx,
			e.Timestamp.Format("2006-01-02 15:04:05"),
			e.Facility,
			e.Severity,
			e.SeverityName,
			e.SourceIP,
			e.Hostname,
			e.Tag,
			e.Message,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (d *DB) retentionWorker() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	// Initial cleanup on startup
	d.cleanOldLogs()

	for {
		select {
		case <-d.stopChan:
			return
		case <-ticker.C:
			d.cleanOldLogs()
		}
	}
}

func (d *DB) cleanOldLogs() {
	d.mu.Lock()
	defer d.mu.Unlock()

	// 1. Time-based retention
	cutoff := fmt.Sprintf("-%d days", d.retentionDays)
	res, err := d.db.Exec(`DELETE FROM syslog_entries WHERE timestamp < datetime('now', ?)`, cutoff)
	if err == nil {
		if rows, _ := res.RowsAffected(); rows > 0 {
			log.Printf("[Retention] Cleaned up %d entries older than %d days", rows, d.retentionDays)
		}
	} else {
		log.Printf("[Retention] Error cleaning old logs: %v", err)
	}

	// 2. Max DB Size check
	if fi, err := os.Stat(d.dbPath); err == nil {
		sizeMB := float64(fi.Size()) / (1024 * 1024)
		if sizeMB > float64(d.maxDBSizeMB) {
			log.Printf("[Retention] DB size %.2f MB exceeds limit of %d MB. Pruning oldest 10000 entries...", sizeMB, d.maxDBSizeMB)
			d.db.Exec(`DELETE FROM syslog_entries WHERE id IN (SELECT id FROM syslog_entries ORDER BY timestamp ASC LIMIT 10000)`)
		}
	}

	// Lightweight SQLite optimize
	d.db.Exec(`PRAGMA optimize`)
}

func (d *DB) QueryLogs(filter LogFilter) ([]*LogEntry, int64, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var conditions []string
	var args []interface{}

	if filter.Search != "" {
		conditions = append(conditions, "(message LIKE ? OR tag LIKE ? OR hostname LIKE ?)")
		searchTerm := "%" + filter.Search + "%"
		args = append(args, searchTerm, searchTerm, searchTerm)
	}
	if filter.Severity != nil {
		conditions = append(conditions, "severity <= ?")
		args = append(args, *filter.Severity)
	}
	if filter.SourceIP != "" {
		conditions = append(conditions, "source_ip = ?")
		args = append(args, filter.SourceIP)
	}
	if filter.Hostname != "" {
		conditions = append(conditions, "hostname = ?")
		args = append(args, filter.Hostname)
	}
	if filter.Tag != "" {
		conditions = append(conditions, "tag = ?")
		args = append(args, filter.Tag)
	}
	if filter.From != nil {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, filter.From.Format("2006-01-02 15:04:05"))
	}
	if filter.To != nil {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, filter.To.Format("2006-01-02 15:04:05"))
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Count total matching
	var total int64
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM syslog_entries %s", whereClause)
	if err := d.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Select rows
	limit := filter.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	queryArgs := append(args, limit, offset)
	dataQuery := fmt.Sprintf(`
		SELECT id, timestamp, facility, severity, severity_name, source_ip, hostname, tag, message
		FROM syslog_entries
		%s
		ORDER BY id DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	rows, err := d.db.Query(dataQuery, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []*LogEntry
	for rows.Next() {
		var e LogEntry
		var tsStr string
		if err := rows.Scan(&e.ID, &tsStr, &e.Facility, &e.Severity, &e.SeverityName, &e.SourceIP, &e.Hostname, &e.Tag, &e.Message); err != nil {
			continue
		}
		if t, err := time.Parse("2006-01-02 15:04:05", tsStr); err == nil {
			e.Timestamp = t
		}
		entries = append(entries, &e)
	}

	return entries, total, nil
}

func (d *DB) GetStats() (*Stats, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var total int64
	_ = d.db.QueryRow("SELECT COUNT(*) FROM syslog_entries").Scan(&total)

	var uniqueHosts int
	_ = d.db.QueryRow("SELECT COUNT(DISTINCT source_ip) FROM syslog_entries").Scan(&uniqueHosts)

	var sizeBytes int64
	if fi, err := os.Stat(d.dbPath); err == nil {
		sizeBytes = fi.Size()
	}

	return &Stats{
		TotalLogs:     total,
		DBSizeBytes:   sizeBytes,
		DBSizeMB:      float64(sizeBytes) / (1024 * 1024),
		RetentionDays: d.retentionDays,
		MaxDBSizeMB:   d.maxDBSizeMB,
		UniqueHosts:   uniqueHosts,
		UptimeSec:     int64(time.Since(d.startTime).Seconds()),
	}, nil
}

func (d *DB) GetUniqueHosts() ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query("SELECT DISTINCT source_ip FROM syslog_entries WHERE source_ip != '' ORDER BY source_ip LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hosts []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err == nil {
			hosts = append(hosts, h)
		}
	}
	return hosts, nil
}

func (d *DB) ExportLogs(filter LogFilter, w io.Writer, format string) error {
	filter.Limit = 50000 // Upper safety limit for single export
	filter.Offset = 0
	entries, _, err := d.QueryLogs(filter)
	if err != nil {
		return err
	}

	if format == "csv" {
		cw := csv.NewWriter(w)
		defer cw.Flush()
		_ = cw.Write([]string{"ID", "Timestamp", "Severity", "Facility", "Source IP", "Hostname", "Tag", "Message"})
		for _, e := range entries {
			_ = cw.Write([]string{
				fmt.Sprintf("%d", e.ID),
				e.Timestamp.Format("2006-01-02 15:04:05"),
				e.SeverityName,
				fmt.Sprintf("%d", e.Facility),
				e.SourceIP,
				e.Hostname,
				e.Tag,
				e.Message,
			})
		}
		return nil
	}

	// RAW format
	for _, e := range entries {
		line := fmt.Sprintf("%s [%s] %s %s: %s\n",
			e.Timestamp.Format("2006-01-02 15:04:05"),
			strings.ToUpper(e.SeverityName),
			e.SourceIP,
			e.Tag,
			e.Message,
		)
		if _, err := io.WriteString(w, line); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ClearAll() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Drain any pending items currently in the buffer channel
	for {
		select {
		case <-d.writeChan:
		default:
			goto drained
		}
	}
drained:
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := d.db.ExecContext(ctx, "DELETE FROM syslog_entries;"); err != nil {
		return fmt.Errorf("failed to clear syslog table: %w", err)
	}

	// Vacuum to reclaim disk space immediately
	_, _ = d.db.ExecContext(ctx, "VACUUM;")
	log.Printf("[DB] All syslog records cleared and database vacuumed manually")
	return nil
}

func (d *DB) Close() error {
	var err error
	d.closeOnce.Do(func() {
		close(d.stopChan)
		err = d.db.Close()
	})
	return err
}

