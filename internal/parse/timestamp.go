package parse

import (
	"math/big"
	"regexp"
	"strings"
	"time"
)

var (
	calendarTimestampPattern = regexp.MustCompile(`^([0-9]{4}-[0-9]{2}-[0-9]{2}[T ][0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,9})?(?:Z|[+-][0-9]{2}:?[0-9]{2})?)(?:[ \t]+|$)`)
	syslogTimestampPattern   = regexp.MustCompile(`^(((?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec))[ \t]+([0-9]{1,2})[ \t]+([0-9]{2}:[0-9]{2}:[0-9]{2}))(?:[ \t]+|$)`)
	zoneSuffixPattern        = regexp.MustCompile(`(?:Z|[+-][0-9]{2}:?[0-9]{2})$`)
)

func parseLeadingTimestamp(input string, context parseContext) (time.Time, string, bool, bool) {
	if match := calendarTimestampPattern.FindStringSubmatch(input); match != nil {
		value, assumed, ok := parseCalendarTimestamp(match[1], context.location)
		if !ok {
			return time.Time{}, input, false, false
		}
		return value, input[len(match[0]):], assumed, true
	}

	if match := syslogTimestampPattern.FindStringSubmatch(input); match != nil {
		month, err := time.Parse("Jan", match[2])
		if err != nil {
			return time.Time{}, input, false, false
		}
		clock, err := time.Parse("15:04:05", match[4])
		if err != nil {
			return time.Time{}, input, false, false
		}

		day := 0
		for _, character := range match[3] {
			day = day*10 + int(character-'0')
		}
		reference := context.reference.In(context.location)
		var selected time.Time
		var selectedDistance time.Duration
		for _, year := range []int{reference.Year() - 1, reference.Year(), reference.Year() + 1} {
			candidate := time.Date(year, month.Month(), day, clock.Hour(), clock.Minute(), clock.Second(), 0, context.location)
			if candidate.Day() != day || candidate.Month() != month.Month() {
				continue
			}
			distance := candidate.Sub(reference)
			if distance < 0 {
				distance = -distance
			}
			if selected.IsZero() || distance < selectedDistance {
				selected, selectedDistance = candidate, distance
			}
		}
		if selected.IsZero() {
			return time.Time{}, input, false, false
		}
		return selected, input[len(match[0]):], true, true
	}

	return time.Time{}, input, false, false
}

func parseTimestampString(input string, context parseContext) (time.Time, bool, bool) {
	trimmed := strings.TrimSpace(input)
	value, remainder, assumed, ok := parseLeadingTimestamp(trimmed, context)
	if !ok || strings.TrimSpace(remainder) != "" {
		return time.Time{}, false, false
	}
	return value, assumed, true
}

func parseCalendarTimestamp(input string, location *time.Location) (time.Time, bool, bool) {
	normalized := strings.Replace(input, " ", "T", 1)
	if zoneSuffixPattern.MatchString(normalized) {
		if len(normalized) >= 5 {
			suffix := normalized[len(normalized)-5:]
			if (suffix[0] == '+' || suffix[0] == '-') && suffix[3] != ':' {
				normalized = normalized[:len(normalized)-5] + suffix[:3] + ":" + suffix[3:]
			}
		}
		value, err := time.Parse(time.RFC3339Nano, normalized)
		return value, false, err == nil
	}
	value, err := time.ParseInLocation("2006-01-02T15:04:05.999999999", normalized, location)
	return value, true, err == nil
}

func parseUnixSeconds(input string) (time.Time, bool) {
	value, ok := new(big.Rat).SetString(input)
	if !ok {
		return time.Time{}, false
	}
	value.Mul(value, big.NewRat(int64(time.Second), 1))
	totalNanoseconds := new(big.Int).Quo(value.Num(), value.Denom())
	if !totalNanoseconds.IsInt64() {
		return time.Time{}, false
	}
	total := totalNanoseconds.Int64()
	return time.Unix(total/int64(time.Second), total%int64(time.Second)), true
}

func formatTimestamp(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
