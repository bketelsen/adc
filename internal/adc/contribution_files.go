package adc

import (
	"fmt"
	"path"
	"strings"
	"unicode/utf8"
)

// Version one accepts a small, explicit text source snapshot and replacement
// files. No archive extraction, URL fetching, Git configuration or symlinks.
func validateContributionFiles(files map[string]string) error {
	if len(files) == 0 || len(files) > 256 {
		return fmt.Errorf("source must contain 1–256 text files")
	}
	total := 0
	for name, body := range files {
		if name == "" || name == "." || len(name) > 240 || path.Clean(name) != name || strings.HasPrefix(name, "/") || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\\x00\r\n\t") {
			return fmt.Errorf("invalid source path")
		}
		for _, part := range strings.Split(name, "/") {
			if strings.EqualFold(part, ".git") {
				return fmt.Errorf("Git metadata cannot be submitted")
			}
		}
		if !utf8.ValidString(body) || strings.ContainsRune(body, 0) || len(body) > 1<<20 {
			return fmt.Errorf("files must be UTF-8 text up to 1 MiB")
		}
		total += len(body) + len(name)
		if total > 4<<20 {
			return fmt.Errorf("source snapshot exceeds 4 MiB")
		}
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if _, ok := files[parent]; ok {
				return fmt.Errorf("source paths overlap")
			}
		}
	}
	return nil
}
