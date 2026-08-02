package streamhost

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// upsertConfigKey and readConfigKey implement the plain "key = value" line
// config file format shared by every backend this package supports
// (Sunshine's sunshine.conf and rust-shine's positionally-specified config
// file use the identical format).

// upsertConfigKey upserts a single "key = value" line in the file at path.
// An empty value removes the key (falling back to the backend's own
// default/auto behavior). The file is created if missing; other
// keys/values are preserved verbatim.
func upsertConfigKey(path, key, value string) error {
	if path == "" {
		return os.ErrNotExist
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	var lines []string
	if data, err := os.ReadFile(path); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, key) && strings.Contains(trimmed, "=") {
				parts := strings.SplitN(trimmed, "=", 2)
				if strings.TrimSpace(parts[0]) == key {
					continue // drop existing line, re-added below if needed
				}
			}
			lines = append(lines, line)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	value = strings.TrimSpace(value)
	if value != "" {
		lines = append(lines, key+" = "+value)
	}

	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// readConfigKey reads the current value of a "key = value" line from the
// file at path, or "" if unset or the file doesn't exist.
func readConfigKey(path, key string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, key) {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}
