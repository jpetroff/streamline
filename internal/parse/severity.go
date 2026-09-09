package parse

import "strings"

func normalizeSeverity(input string) string {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "emerg", "emergency", "alert", "crit", "critical", "fatal", "panic", "dpanic":
		return "fatal"
	case "err", "error":
		return "error"
	case "warn", "warning":
		return "warn"
	case "notice", "info", "information":
		return "info"
	case "debug", "trace":
		return "debug"
	default:
		return ""
	}
}

func syslogSeverity(priority int) string {
	switch {
	case priority >= 0 && priority <= 2:
		return "fatal"
	case priority == 3:
		return "error"
	case priority == 4:
		return "warn"
	case priority == 5 || priority == 6:
		return "info"
	case priority == 7:
		return "debug"
	default:
		return ""
	}
}
