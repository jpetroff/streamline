package parse

import "testing"

func TestSanitizerPreservesUTF8WhileRemovingC1Controls(t *testing.T) {
	const visible = "世界 ☺ 🙂 café"
	for _, input := range []string{visible, "\x1b[31m" + visible + "\x1b[0m", "\x9b31m" + visible + "\x9b0m", "\u009b31m" + visible + "\u009b0m"} {
		cleaned := sanitizeBytes([]byte(input), true)
		if cleaned.text != visible || cleaned.invalidUTF8 || cleaned.hadTerminal != (input != visible) {
			t.Fatalf("%q sanitized to %#v", input, cleaned)
		}
	}
}
