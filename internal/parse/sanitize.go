package parse

import (
	"bytes"
	"regexp"
	"strings"
	"unicode/utf8"
)

const escapeByte = 0x1b

var pagerStatusPattern = regexp.MustCompile(`(?i)^(?:lines?[[:space:]]+[0-9]+(?:-[0-9]+)?/[0-9]+[[:space:]]+)?\(END\)$`)

type sanitizedText struct {
	text        string
	hadTerminal bool
	invalidUTF8 bool
}

func sanitizeBytes(input []byte, preserveNewlines bool) sanitizedText {
	output := make([]byte, 0, len(input))
	hadTerminal := false

	for i := 0; i < len(input); {
		current := input[i]
		switch {
		case current == escapeByte:
			hadTerminal = true
			i = consumeEscape(input, i)
		case current == 0xc2 && i+1 < len(input) && input[i+1] >= 0x80 && input[i+1] <= 0x9f:
			hadTerminal = true
			i = consumeC1(input, i+1)
		case current >= 0x80 && current <= 0x9f:
			hadTerminal = true
			i = consumeC1(input, i)
		case current < 0x20:
			switch current {
			case '\t':
				output = append(output, current)
				i++
			case '\n':
				if preserveNewlines {
					output = append(output, '\n')
				}
				i++
			case '\r':
				if preserveNewlines {
					output = append(output, '\n')
					if i+1 < len(input) && input[i+1] == '\n' {
						i++
					}
				}
				i++
			default:
				hadTerminal = true
				i++
			}
		case current == 0x7f:
			hadTerminal = true
			i++
		default:
			output = append(output, current)
			i++
		}
	}

	invalidUTF8 := !utf8.Valid(output)
	output = bytes.ToValidUTF8(output, []byte("\uFFFD"))

	var cleaned strings.Builder
	cleaned.Grow(len(output))
	for _, value := range string(output) {
		if value >= 0x80 && value <= 0x9f {
			hadTerminal = true
			continue
		}
		cleaned.WriteRune(value)
	}
	return sanitizedText{text: cleaned.String(), hadTerminal: hadTerminal, invalidUTF8: invalidUTF8}
}

func consumeEscape(input []byte, start int) int {
	if start+1 >= len(input) {
		return len(input)
	}
	switch input[start+1] {
	case '[':
		return consumeCSI(input, start+2)
	case ']':
		return consumeControlString(input, start+2, true)
	case 'P', 'X', '^', '_':
		return consumeControlString(input, start+2, false)
	default:
		index := start + 1
		for index < len(input) && input[index] >= 0x20 && input[index] <= 0x2f {
			index++
		}
		if index < len(input) {
			return index + 1
		}
		return len(input)
	}
}

func consumeC1(input []byte, start int) int {
	switch input[start] {
	case 0x9b:
		return consumeCSI(input, start+1)
	case 0x9d:
		return consumeControlString(input, start+1, true)
	case 0x90, 0x98, 0x9e, 0x9f:
		return consumeControlString(input, start+1, false)
	default:
		return start + 1
	}
}

func consumeCSI(input []byte, start int) int {
	for index := start; index < len(input); index++ {
		if input[index] >= 0x40 && input[index] <= 0x7e {
			return index + 1
		}
	}
	return len(input)
}

func consumeControlString(input []byte, start int, bellTerminates bool) int {
	for index := start; index < len(input); index++ {
		if bellTerminates && input[index] == '\a' {
			return index + 1
		}
		if input[index] == escapeByte && index+1 < len(input) && input[index+1] == '\\' {
			return index + 2
		}
	}
	return len(input)
}

func isPagerArtifact(cleaned string, hadTerminal bool) bool {
	if !hadTerminal {
		return false
	}
	return cleaned == "~" || pagerStatusPattern.MatchString(cleaned)
}

func hasLessTruncation(raw []byte, cleaned string) bool {
	if !strings.HasSuffix(cleaned, ">") {
		return false
	}
	return bytes.Contains(raw, []byte{escapeByte, '[', '7', 'm', '>'})
}
