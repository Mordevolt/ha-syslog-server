package syslog

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Mordevolt/ha-syslog-server/syslog-server/internal/db"
)

var severityNames = []string{
	"emergency",
	"alert",
	"critical",
	"error",
	"warning",
	"notice",
	"info",
	"debug",
}

func SeverityName(sev int) string {
	if sev >= 0 && sev < len(severityNames) {
		return severityNames[sev]
	}
	return "unknown"
}

// Regex for standard RFC 3164 Syslog: <PRI>MMM DD HH:MM:SS HOSTNAME TAG[PID]: MESSAGE
var rfc3164Regex = regexp.MustCompile(`^<([0-9]{1,3})>([A-Za-z]{3}\s+[0-9\s]{1,2}\s+[0-9]{2}:[0-9]{2}:[0-9]{2})\s+([^\s:]+)\s+([^:\[\s]+)(?:\[([0-9]+)\])?:\s*(.*)$`)

// Regex for simplified BSD format: <PRI>TAG[PID]: MESSAGE or <PRI>HOSTNAME TAG: MESSAGE
var rfc3164Simple = regexp.MustCompile(`^<([0-9]{1,3})>(?:([^\s:]+)\s+)?([^:\[\s]+)(?:\[([0-9]+)\])?:\s*(.*)$`)

func ParseSyslogPacket(raw []byte, remoteIP string) *db.LogEntry {
	msgStr := strings.TrimSpace(string(raw))
	now := time.Now()

	entry := &db.LogEntry{
		Timestamp:    now,
		Facility:     1, // user-level
		Severity:     6, // info
		SeverityName: "info",
		SourceIP:     remoteIP,
		Hostname:     remoteIP,
		Tag:          "syslog",
		Message:      msgStr,
	}

	if len(msgStr) == 0 {
		return entry
	}

	// 1. Try RFC 3164 standard with timestamp
	if matches := rfc3164Regex.FindStringSubmatch(msgStr); len(matches) >= 7 {
		pri, _ := strconv.Atoi(matches[1])
		entry.Facility = pri / 8
		entry.Severity = pri % 8
		entry.SeverityName = SeverityName(entry.Severity)

		// Parse BSD timestamp: "Oct 04 22:15:30" (adds current year)
		timeStr := matches[2]
		currentYear := now.Year()
		dateStr := strconv.Itoa(currentYear) + " " + strings.Join(strings.Fields(timeStr), " ")
		if t, err := time.Parse("2006 Jan 02 15:04:05", dateStr); err == nil {
			entry.Timestamp = t
		} else if t, err := time.Parse("2006 Jan _2 15:04:05", dateStr); err == nil {
			entry.Timestamp = t
		} else {
			entry.Timestamp = now
		}

		entry.Hostname = matches[3]
		entry.Tag = matches[4]
		entry.Message = matches[6]
		return entry
	}

	// 2. Try RFC 3164 simple / without timestamp
	if matches := rfc3164Simple.FindStringSubmatch(msgStr); len(matches) >= 6 {
		pri, _ := strconv.Atoi(matches[1])
		entry.Facility = pri / 8
		entry.Severity = pri % 8
		entry.SeverityName = SeverityName(entry.Severity)

		if matches[2] != "" {
			entry.Hostname = matches[2]
		}
		if matches[3] != "" {
			entry.Tag = matches[3]
		}
		entry.Message = matches[5]
		return entry
	}

	// 3. Fallback: extract PRI if starts with <NUM>
	if strings.HasPrefix(msgStr, "<") {
		endIdx := strings.Index(msgStr, ">")
		if endIdx > 1 && endIdx <= 4 {
			priStr := msgStr[1:endIdx]
			if pri, err := strconv.Atoi(priStr); err == nil {
				entry.Facility = pri / 8
				entry.Severity = pri % 8
				entry.SeverityName = SeverityName(entry.Severity)
				entry.Message = strings.TrimSpace(msgStr[endIdx+1:])
			}
		}
	}

	return entry
}
