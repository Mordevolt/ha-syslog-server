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
var rfc3164Regex = regexp.MustCompile(`^<([0-9]{1,3})>([A-Za-zА-Яа-я]{3,4}\s+[0-9\s]{1,2}\s+[0-9]{2}:[0-9]{2}:[0-9]{2})\s+([^\s:]+)\s+([^:\[\s]+)(?:\[([0-9]+)\])?:\s*(.*)$`)

// Regex for RFC 5424 Syslog: <PRI>1 TIMESTAMP HOSTNAME APP-NAME PROCID MSGID [SD] MESSAGE
var rfc5424Regex = regexp.MustCompile(`^<([0-9]{1,3})>1\s+([0-9T:\-\.Z\+]+)\s+([^\s]+)\s+([^\s]+)(?:\s+[^\s]+)?(?:\s+[^\s]+)?(?:\s+\[.*?\])?\s*(.*)$`)

// Regex for simplified BSD format: <PRI>TAG[PID]: MESSAGE or <PRI>HOSTNAME TAG: MESSAGE
var rfc3164Simple = regexp.MustCompile(`^<([0-9]{1,3})>(?:([^\s:]+)\s+)?([^:\[\s]+)(?:\[([0-9]+)\])?:\s*(.*)$`)

func parseBSDTimestamp(timeStr string, now time.Time) time.Time {
	// Support Russian month abbreviations if router sends localized names
	ruMonths := map[string]string{
		"янв": "Jan", "фев": "Feb", "мар": "Mar", "апр": "Apr",
		"май": "May", "июн": "Jun", "июл": "Jul", "авг": "Aug",
		"сен": "Sep", "окт": "Oct", "ноя": "Nov", "дек": "Dec",
	}
	lower := strings.ToLower(timeStr)
	for ru, en := range ruMonths {
		if strings.HasPrefix(lower, ru) {
			timeStr = en + timeStr[len(ru):]
			break
		}
	}

	fields := strings.Fields(timeStr)
	if len(fields) < 3 {
		return now
	}
	currentYear := strconv.Itoa(now.Year())
	normalized := currentYear + " " + strings.Join(fields, " ")

	layouts := []string{
		"2006 Jan 2 15:04:05",
		"2006 Jan 02 15:04:05",
		"2006 Jan _2 15:04:05",
	}
	loc := now.Location()
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, normalized, loc); err == nil {
			// If router clock is drastically skewed (> 7 days off), fallback to current server time
			diff := now.Sub(t)
			if diff > 7*24*time.Hour || diff < -24*time.Hour {
				return now
			}
			return t
		}
	}
	return now
}

func parseISO8601Timestamp(timeStr string, now time.Time) time.Time {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, timeStr); err == nil {
			return t.UTC()
		}
	}
	return now.UTC()
}

func ParseSyslogPacket(raw []byte, remoteIP string) *db.LogEntry {
	msgStr := strings.TrimSpace(string(raw))
	now := time.Now().UTC()

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

	// 1. Try RFC 5424 standard (starts with <PRI>1 )
	if matches := rfc5424Regex.FindStringSubmatch(msgStr); len(matches) >= 6 {
		pri, _ := strconv.Atoi(matches[1])
		entry.Facility = pri / 8
		entry.Severity = pri % 8
		entry.SeverityName = SeverityName(entry.Severity)

		entry.Timestamp = parseISO8601Timestamp(matches[2], now)
		if matches[3] != "-" && matches[3] != "" {
			entry.Hostname = matches[3]
		}
		if matches[4] != "-" && matches[4] != "" {
			entry.Tag = matches[4]
		}
		if len(matches) > 5 && matches[5] != "" {
			entry.Message = matches[5]
		}
		return entry
	}

	// 2. Try RFC 3164 standard with timestamp
	if matches := rfc3164Regex.FindStringSubmatch(msgStr); len(matches) >= 7 {
		pri, _ := strconv.Atoi(matches[1])
		entry.Facility = pri / 8
		entry.Severity = pri % 8
		entry.SeverityName = SeverityName(entry.Severity)

		// For RFC 3164 (BSD), packets are received in real-time over UDP without timezone.
		// Using the NTP-synchronized receipt timestamp now.UTC() prevents double-offset skew.
		entry.Timestamp = now.UTC()
		entry.Hostname = matches[3]
		entry.Tag = matches[4]
		entry.Message = matches[6]
		return entry
	}

	// 3. Try RFC 3164 simple / without timestamp
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

	// 4. Fallback: extract PRI if starts with <NUM>
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
