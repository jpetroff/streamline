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
	// The bracketed access timestamp must follow exactly three unquoted fields.
	// This excludes virtual-host prefixes and arbitrary prose or quoted JSON.
	httpAccessPrefixPattern = regexp.MustCompile(`^([^ \t]+)[ \t]+([^ \t]+)[ \t]+([^ \t]+)[ \t]+\[([0-9]{2}/[A-Za-z]{3}/[0-9]{4}:[^\]]*)\][ \t]+`)
	httpAccessTimePattern   = regexp.MustCompile(`^[0-9]{2}/[A-Za-z]{3}/[0-9]{4}:[0-9]{2}:[0-9]{2}:[0-9]{2} [+-][0-9]{4}$`)
	httpProtocolPattern     = regexp.MustCompile(`^HTTP/[0-9]+(?:\.[0-9]+)?$`)
)

// parseHTTPAccess accepts only complete Common or Combined layouts. Numeric
// fields are typed here; generic JSON/logfmt normalization is deliberately unused.
func parseHTTPAccess(input string, diagnostics []logmodel.Diagnostic) (parseDecision, bool) {
	line := strings.TrimLeft(input, " \t")
	prefix := httpAccessPrefixPattern.FindStringSubmatch(line)
	if prefix == nil {
		return parseDecision{}, false
	}
	fail := func() (parseDecision, bool) {
		return malformedFormat(input, diagnostics, "malformed_http_access_fallback", "unsupported or malformed Common/Combined access layout"), true
	}
	request, rest, ok := scanAccessQuoted(line[len(prefix[0]):])
	if !ok {
		return fail()
	}
	status, rest, ok := accessToken(rest)
	if !ok {
		return fail()
	}
	size, rest, ok := accessToken(rest)
	if !ok {
		return fail()
	}
	// Apache can hex-escape unquoted host, ident, and user fields too.
	for i := 1; i <= 3; i++ {
		decoded, remainder, valid := scanAccessQuoted("\"" + prefix[i] + "\"")
		if !valid || remainder != "" {
			return fail()
		}
		prefix[i] = decoded
	}
	fields := map[string]any{
		"client": accessValue(prefix[1]), "ident": accessValue(prefix[2]),
		"user": accessValue(prefix[3]), "time": prefix[4], "request": request,
	}
	if status == "-" {
		fields["status"] = nil
	} else {
		if len(status) != 3 || !asciiDigits(status) || status < "100" || status > "599" {
			return fail()
		}
		fields["status"] = json.Number(status)
	}
	if size == "-" {
		fields["bytes"] = nil
	} else {
		if !asciiDigits(size) {
			return fail()
		}
		number, err := strconv.ParseUint(size, 10, 64)
		if err != nil {
			return fail()
		}
		fields["bytes"] = json.Number(strconv.FormatUint(number, 10))
	}
	if strings.Trim(rest, " \t") != "" {
		if rest[0] != ' ' && rest[0] != '\t' {
			return fail()
		}
		referer, afterReferer, ok := scanAccessQuoted(strings.TrimLeft(rest, " \t"))
		if !ok || len(afterReferer) == 0 || (afterReferer[0] != ' ' && afterReferer[0] != '\t') {
			return fail()
		}
		agent, afterAgent, ok := scanAccessQuoted(strings.TrimLeft(afterReferer, " \t"))
		if !ok || strings.Trim(afterAgent, " \t") != "" {
			return fail()
		}
		fields["referer"] = accessValue(referer)
		fields["user_agent"] = accessValue(agent)
	}
	// Decode request components only when the original request has a valid
	// three-part shape. Bad client requests are still valid access log records.
	parts := strings.Split(request, " ")
	if len(parts) == 3 && httpMethodToken(parts[0]) && httpRequestTarget(parts[1]) && httpProtocolPattern.MatchString(parts[2]) {
		fields["method"], fields["target"], fields["protocol"] = parts[0], parts[1], parts[2]
	}
	fields = cleanJSONObject(fields, &diagnostics)
	record := logmodel.Record{
		Fields: fields, Message: fields["request"].(string),
		SourceFormat: logmodel.FormatHTTPAccess, Diagnostics: diagnostics,
	}
	timestamp, err := time.Parse("02/Jan/2006:15:04:05 -0700", prefix[4])
	if err == nil && httpAccessTimePattern.MatchString(prefix[4]) && validNumericZone(prefix[4]) {
		record.Timestamp = formatTimestamp(timestamp)
	} else {
		addDiagnostic(&record.Diagnostics, "invalid_timestamp", "access timestamp is not a valid date with a numeric timezone")
	}
	return parseDecision{Record: record, IdentifiesLogs: true}, true
}

func accessValue(value string) any {
	if value == "-" {
		return nil
	}
	return value
}

// accessToken requires whitespace before an unquoted field and leaves the next
// separator in the remainder, so adjacent fields cannot accidentally be accepted.
func accessToken(input string) (string, string, bool) {
	if input == "" || (input[0] != ' ' && input[0] != '\t') {
		return "", input, false
	}
	input = strings.TrimLeft(input, " \t")
	end := strings.IndexAny(input, " \t")
	if end < 0 {
		end = len(input)
	}
	return input[:end], input[end:], end > 0
}

// scanAccessQuoted handles Apache C-style escapes and Nginx/Apache hex-byte
// escapes. It does not URL-decode requests or guess the meaning of unknown escapes.
func scanAccessQuoted(input string) (string, string, bool) {
	if input == "" || input[0] != '"' {
		return "", input, false
	}
	var value strings.Builder
	for position := 1; position < len(input); position++ {
		ch := input[position]
		if ch == '"' {
			return value.String(), input[position+1:], true
		}
		if ch == '\\' {
			position++
			if position >= len(input) {
				return "", input, false
			}
			ch = input[position]
			switch ch {
			case '"', '\\':
			case 'n':
				ch = '\n'
			case 'r':
				ch = '\r'
			case 't':
				ch = '\t'
			case 'b':
				ch = '\b'
			case 'v':
				ch = '\v'
			case 'f':
				ch = '\f'
			case 'a':
				ch = '\a'
			case 'x':
				if position+2 >= len(input) {
					return "", input, false
				}
				decoded, err := strconv.ParseUint(input[position+1:position+3], 16, 8)
				if err != nil {
					return "", input, false
				}
				ch = byte(decoded)
				position += 2
			default:
				return "", input, false
			}
		}
		value.WriteByte(ch)
	}
	return "", input, false
}

func httpMethodToken(value string) bool {
	if value == "" {
		return false
	}
	for i := range len(value) {
		ch := value[i]
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			continue
		}
		if !strings.ContainsRune("!#$%&'*+-.^_|~", rune(ch)) && ch != 0x60 {
			return false
		}
	}
	return true
}

func httpRequestTarget(value string) bool {
	if value == "" {
		return false
	}
	for i := range len(value) {
		if value[i] <= 32 || value[i] == 127 {
			return false
		}
	}
	return true
}
