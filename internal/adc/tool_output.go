package adc

import (
	"encoding/json"
	"strings"
)

const inlineToolBytes = 12 << 10

const truncatedToolGuidance = "Output was shortened by ADC to stay inline. Do not treat this preview as complete evidence. Use a focused status view or narrower read (grep/sed). Redirect long command output to a file in /workspace and inspect selected lines; provider-private /tmp files are not available in the protected workspace."

func textPrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "�")
}

// Bound the encoded result, not just raw stdout: JSON escaping can multiply its
// size. Preserve exit status, but truncated output is never complete evidence.
func inlineWorkspaceResult(result workspaceResult) workspaceResult {
	raw, _ := json.Marshal(result)
	if len(raw) <= inlineToolBytes {
		return result
	}
	result.Truncated = true
	prefix := result.Output
	for {
		prefix = textPrefix(prefix, len(prefix)/2)
		result.Output = prefix + "\n\n" + truncatedToolGuidance
		raw, _ = json.Marshal(result)
		if len(raw) <= inlineToolBytes {
			return result
		}
	}
}

func inlineToolText(value string) string {
	if len(value) <= inlineToolBytes {
		return value
	}
	preview := value
	for {
		preview = textPrefix(preview, len(preview)/2)
		raw, _ := json.Marshal(map[string]any{"Truncated": true, "Preview": strings.TrimSpace(preview), "Guidance": truncatedToolGuidance})
		if len(raw) <= inlineToolBytes {
			return string(raw)
		}
	}
}
