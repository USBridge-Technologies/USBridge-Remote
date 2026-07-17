package models

import (
	"strings"
	"testing"
)

func TestFormatChangelogRename(t *testing.T) {
	s := &SnapshotInfo{
		Changelog: "rename          ./data_2026-02-06_00-57-50/o474-191-0 dest=./data_2026-02-06_00-57-50/record.mov",
	}
	result := s.FormatChangelog(nil)
	if !strings.Contains(result, "o474-191-0 → record.mov") {
		t.Errorf("Expected rename to show BTRFS name → dest, got: %s", result)
	}
}

func TestFormatChangelogPathsWithSpaces(t *testing.T) {
	// mkfile and rename with paths containing spaces (escaped with \ )
	s := &SnapshotInfo{
		Changelog: "mkfile          ./data_2026-02-06_00-57-50/My\\ File.mov\n" +
			"rename          ./data_2026-02-06_00-57-50/o474-191-0 dest=./data_2026-02-06_00-57-50/Screen\\ Recording.mov",
	}
	result := s.FormatChangelog(nil)
	if !strings.Contains(result, "My File.mov") {
		t.Errorf("Expected mkfile to show filename with spaces, got: %s", result)
	}
	if !strings.Contains(result, "Screen Recording.mov") {
		t.Errorf("Expected rename dest to show filename with spaces, got: %s", result)
	}
	if !strings.Contains(result, "o474-191-0 → Screen Recording.mov") {
		t.Errorf("Expected rename to show BTRFS name → dest with spaces, got: %s", result)
	}
}

func TestFormatChangelogPathSpansMultipleParts(t *testing.T) {
	// Path with unescaped spaces spans multiple parts
	s := &SnapshotInfo{
		Changelog: "rename          ./data/File with spaces.mov dest=./data/Other name.mov",
	}
	result := s.FormatChangelog(nil)
	if !strings.Contains(result, "File with spaces.mov") {
		t.Errorf("Expected path with spaces, got: %s", result)
	}
	if !strings.Contains(result, "Other name.mov") {
		t.Errorf("Expected dest with spaces, got: %s", result)
	}
}

func TestFormatChangelogWithSpaces(t *testing.T) {
	s := &SnapshotInfo{
		Changelog: "clone           ./data_2026-02-06_00-57-50/record.mov offset=0 len=29944405 from=./data_2026-02-06_00-57-50/Screen\\ Recording_rnm\\ 2026-01-20\\ at\\ 21.10.02.mov clone_offset=0",
	}
	result := s.FormatChangelog(nil)
	if !strings.Contains(result, "Screen Recording_rnm 2026-01-20 at 21.10.02.mov") {
		t.Errorf("Expected changelog to contain source filename with spaces, got: %s", result)
	}
	if !strings.Contains(result, "record.mov") {
		t.Errorf("Expected changelog to contain record.mov, got: %s", result)
	}
	// clone: from → to (source → destination)
	if !strings.Contains(result, "Screen Recording_rnm 2026-01-20 at 21.10.02.mov → record.mov") {
		t.Errorf("Expected clone to show source → dest order, got: %s", result)
	}
}
