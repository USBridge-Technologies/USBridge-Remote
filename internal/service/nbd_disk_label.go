// Package service: reading volume label (disk name inside image) for use as export_name.

package service

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/sirupsen/logrus"
)

const (
	mbrPart1StartLBA = 0x1C6   // offset in MBR: start LBA of the first partition (4 bytes LE)
	ext4MagicOffset  = 0x38    // in ext4 superblock: signature 0xEF53
	ext4LabelOffset  = 0x278   // in ext4 superblock: volume name, 16 bytes
	gptNameOffset    = 0x48    // in GPT header (LBA 1): disk name, 72 bytes UTF-16LE
	superblockSize   = 1024   // ext4 superblock starts at offset 1024 from the beginning of the partition
	readSectors      = 32768  // 16MB - covers partition alignment up to 8MB (16384 sectors)
)

// GetVolumeLabelFromImage reads the volume label from the image (ext4 - from the superblock of the first partition).
// Requires qemu-img. Returns the label (without trailing spaces) or "disk" on error/empty label.
func GetVolumeLabelFromImage(imagePath string) string {
	qemuImg := qemuImgPath()
	if qemuImg == "" {
		return "disk"
	}
	ext := strings.ToLower(filepath.Ext(imagePath))
	format := backingFormat(ext)

	// qemu-img dd -f vdi -O raw if=image of=temp count=N
	tmpFile, err := os.CreateTemp("", "usbridge-nbd-label-*.bin")
	if err != nil {
		logrus.Debugf("[NBD-LABEL] temp file: %v", err)
		return "disk"
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	tmpFile.Close()

	cmd := exec.Command(qemuImg, "dd", "-f", format, "-O", "raw", "if="+imagePath, "of="+tmpPath,
		"bs=512", fmt.Sprintf("count=%d", readSectors))
	maybeHideWindow(cmd)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		logrus.Debugf("[NBD-LABEL] qemu-img dd: %s: %v", string(out), err)
		return "disk"
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil || len(data) < 1024 {
		return "disk"
	}

	// First GPT: disk name in header (LBA 1)
	if len(data) >= 512+72+72 && bytes.Equal(data[512:520], []byte("EFI PART")) {
		nameUTF16 := data[512+gptNameOffset : 512+gptNameOffset+72]
		if s := decodeUTF16LE(nameUTF16); s != "" {
			return sanitizeExportName(s)
		}
	}

	// MBR: partition 1 - start LBA (4 bytes LE) at offset 0x1C6
	if len(data) < mbrPart1StartLBA+4 {
		return "disk"
	}
	part1LBA := binary.LittleEndian.Uint32(data[mbrPart1StartLBA : mbrPart1StartLBA+4])
	if part1LBA == 0 {
		return "disk"
	}

	// ext4 superblock: offset = part1LBA*512 + 1024
	superOff := int(part1LBA)*512 + superblockSize
	if superOff+1024 > len(data) {
		return "disk"
	}
	super := data[superOff:]
	if len(super) < ext4MagicOffset+2 {
		return "disk"
	}
	// ext4 magic 0xEF53 in little-endian
	if super[ext4MagicOffset] != 0x53 || super[ext4MagicOffset+1] != 0xEF {
		return "disk"
	}
	if len(super) < ext4LabelOffset+16 {
		return "disk"
	}
	label := strings.TrimSpace(string(bytes.TrimRight(super[ext4LabelOffset:ext4LabelOffset+16], "\x00")))
	if label == "" {
		return "disk"
	}
	return sanitizeExportName(label)
}

func decodeUTF16LE(b []byte) string {
	var u16 []uint16
	for i := 0; i+1 < len(b); i += 2 {
		u16 = append(u16, binary.LittleEndian.Uint16(b[i:i+2]))
	}
	return string(utf16.Decode(u16))
}

// sanitizeExportName removes characters not allowed in export name (spaces -> underscores, etc.).
func sanitizeExportName(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), "._ ")
	if out == "" {
		return "disk"
	}
	// Limit length (NBD/API may have a limit)
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}
