package parse

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"streamline/internal/logmodel"
)

var (
	// A local-file record must contain both a hostname and an app[:]/app[pid]: tag.
	// Timestamped prose without this header remains ordinary timestamped text.
	syslogTagPattern   = regexp.MustCompile(`^(-|[A-Za-z0-9_][A-Za-z0-9_.:-]*)[ \t]+([A-Za-z0-9_][A-Za-z0-9_.@/-]*)(?:\[([0-9]+)\])?:(?:[ \t](.*)|$)`)
	rfc5424TimePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,6})?(?:Z|[+-][0-9]{2}:[0-9]{2})$`)
)

// parseSyslog reports candidacy separately from successful recognition. A PRI
// prefix owns its failures; a local file needs an unambiguous timestamp/tag shape.
func parseSyslog(input string, context parseContext, diagnostics []logmodel.Diagnostic) (parseDecision, bool) {
	line := strings.TrimLeft(input, " \t")
	priority := -1
	format := logmodel.FormatSyslogText
	if len(line) > 1 && line[0] == '<' && line[1] >= '0' && line[1] <= '9' {
		end := strings.IndexByte(line, '>')
		if end < 2 || end > 4 || !asciiDigits(line[1:end]) || (end > 2 && line[1] == '0') {
			return malformedSyslog(input, diagnostics), true
		}
		priority, _ = strconv.Atoi(line[1:end])
		if priority > 191 {
			return malformedSyslog(input, diagnostics), true
		}
		line = line[end+1:]
		if len(line) > 0 && line[0] >= '0' && line[0] <= '9' {
			return parseRFC5424(input, line, priority, diagnostics), true
		}
		format = logmodel.FormatSyslogRFC3164
	}

	match := syslogTimestampPattern.FindStringSubmatch(line)
	if match == nil && priority < 0 {
		match = calendarTimestampPattern.FindStringSubmatch(line)
	}
	if match == nil {
		if priority >= 0 {
			return malformedSyslog(input, diagnostics), true
		}
		return parseDecision{}, false
	}
	header := syslogTagPattern.FindStringSubmatch(line[len(match[0]):])
	if header == nil {
		if priority >= 0 {
			return malformedSyslog(input, diagnostics), true
		}
		return parseDecision{}, false
	}
	fields := map[string]any{
		"time": match[1], "hostname": syslogValue(header[1]),
		"app": header[2], "procid": nil,
	}
	if header[3] != "" {
		fields["procid"] = header[3]
	}
	record := logmodel.Record{Fields: fields, Message: header[4], SourceFormat: format, Diagnostics: diagnostics}
	if timestamp, assumed, ok := parseTimestampString(match[1], context); ok {
		record.Timestamp = formatTimestamp(timestamp)
		if assumed {
			addDiagnostic(&record.Diagnostics, "timestamp_context_assumed", "syslog timestamp omitted a timezone or year; configured context was applied")
		}
	} else {
		addDiagnostic(&record.Diagnostics, "invalid_timestamp", "syslog timestamp is not a valid date")
	}
	setSyslogPriority(&record, priority)
	return parseDecision{Record: record, IdentifiesLogs: true}, true
}

func parseRFC5424(input, line string, priority int, diagnostics []logmodel.Diagnostic) parseDecision {
	parts := strings.SplitN(line, " ", 7)
	if len(parts) != 7 || parts[0] != "1" {
		return malformedSyslog(input, diagnostics)
	}
	for i, limit := range []int{32, 255, 48, 128, 32} {
		if !syslogHeaderToken(parts[i+1], limit) {
			return malformedSyslog(input, diagnostics)
		}
	}
	structured, message, ok := scanStructuredData(parts[6])
	if !ok {
		return malformedSyslog(input, diagnostics)
	}
	fields := map[string]any{
		"version": json.Number("1"), "time": syslogValue(parts[1]),
		"hostname": syslogValue(parts[2]), "app": syslogValue(parts[3]),
		"procid": syslogValue(parts[4]), "msgid": syslogValue(parts[5]),
		"structured_data": structured,
	}
	// Structured values are sanitized after decoding, as with JSON and logfmt.
	fields = cleanJSONObject(fields, &diagnostics)
	record := logmodel.Record{
		Fields: fields, Message: strings.TrimPrefix(message, "\ufeff"),
		SourceFormat: logmodel.FormatSyslogRFC5424, Diagnostics: diagnostics,
	}
	if parts[1] != "-" {
		timestamp, err := time.Parse(time.RFC3339Nano, parts[1])
		if err == nil && rfc5424TimePattern.MatchString(parts[1]) && validNumericZone(parts[1]) {
			record.Timestamp = formatTimestamp(timestamp)
		} else {
			addDiagnostic(&record.Diagnostics, "invalid_timestamp", "RFC 5424 timestamp must be an RFC 3339 date with at most six fractional digits")
		}
	}
	setSyslogPriority(&record, priority)
	return parseDecision{Record: record, IdentifiesLogs: true}
}

func malformedSyslog(input string, diagnostics []logmodel.Diagnostic) parseDecision {
	return malformedFormat(input, diagnostics, "malformed_syslog_fallback", "unsupported or malformed syslog header or structured data")
}

func setSyslogPriority(record *logmodel.Record, priority int) {
	if priority < 0 {
		return
	}
	record.Fields["priority"] = json.Number(strconv.Itoa(priority))
	record.Fields["facility"] = json.Number(strconv.Itoa(priority / 8))
	record.Fields["severity_code"] = json.Number(strconv.Itoa(priority % 8))
	record.Severity = syslogSeverity(priority % 8)
}

func syslogValue(value string) any {
	if value == "-" {
		return nil
	}
	return value
}

func syslogHeaderToken(value string, limit int) bool {
	if len(value) == 0 || len(value) > limit {
		return false
	}
	for i := range len(value) {
		if value[i] < 33 || value[i] > 126 {
			return false
		}
	}
	return true
}

func asciiDigits(value string) bool {
	if value == "" {
		return false
	}
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

// Go's time parser accepts offset hours of 24 and minutes of 60; the log
// formats require hours 00..23 and minutes 00..59.
func validNumericZone(value string) bool {
	if strings.HasSuffix(value, "Z") {
		return true
	}
	zone := value[strings.LastIndexAny(value, "+-")+1:]
	zone = strings.ReplaceAll(zone, ":", "")
	return len(zone) == 4 && asciiDigits(zone) && zone[:2] < "24" && zone[2:] < "60"
}

// scanStructuredData consumes exactly STRUCTURED-DATA and its optional SP MSG.
// Repeated parameter names become arrays; duplicate element IDs are malformed.
// Unknown escapes retain the backslash, as required by RFC 5424 section 6.3.3.
func scanStructuredData(input string) (any, string, bool) {
	if strings.HasPrefix(input, "-") {
		message, ok := syslogMessage(input[1:])
		return nil, message, ok
	}
	elements := make(map[string]any)
	position := 0
	for position < len(input) && input[position] == '[' {
		position++
		start := position
		for position < len(input) && sdNameByte(input[position]) {
			position++
		}
		id := input[start:position]
		if len(id) == 0 || len(id) > 32 {
			return nil, "", false
		}
		if _, exists := elements[id]; exists {
			return nil, "", false
		}
		params := make(map[string]any)
		for position < len(input) && input[position] == ' ' {
			position++
			start = position
			for position < len(input) && sdNameByte(input[position]) {
				position++
			}
			name := input[start:position]
			if len(name) == 0 || len(name) > 32 || !strings.HasPrefix(input[position:], "=\"") {
				return nil, "", false
			}
			position += 2
			var value strings.Builder
			closed := false
			for position < len(input) {
				ch := input[position]
				position++
				if ch == '"' {
					closed = true
					break
				}
				if ch == ']' {
					return nil, "", false
				}
				if ch == '\\' && position < len(input) {
					next := input[position]
					if next == '\\' || next == '"' || next == ']' {
						ch = next
						position++
					}
				}
				value.WriteByte(ch)
			}
			if !closed {
				return nil, "", false
			}
			text := value.String()
			switch previous := params[name].(type) {
			case nil:
				params[name] = text
			case string:
				params[name] = []any{previous, text}
			case []any:
				params[name] = append(previous, text)
			}
		}
		if position >= len(input) || input[position] != ']' {
			return nil, "", false
		}
		position++
		elements[id] = params
	}
	if len(elements) == 0 {
		return nil, "", false
	}
	message, ok := syslogMessage(input[position:])
	return elements, message, ok
}

func sdNameByte(ch byte) bool {
	return ch >= 33 && ch <= 126 && ch != '=' && ch != ']' && ch != '"'
}

func syslogMessage(remainder string) (string, bool) {
	if remainder == "" {
		return "", true
	}
	if remainder[0] != ' ' {
		return "", false
	}
	return remainder[1:], true
}
