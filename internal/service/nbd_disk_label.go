// Package service: чтение метки тома (имя диска внутри образа) для использования как export_name.

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
	mbrPart1StartLBA = 0x1C6   // смещение в MBR: LBA начала первой партиции (4 байта LE)
	ext4MagicOffset  = 0x38    // в суперблоке ext4: сигнатура 0xEF53
	ext4LabelOffset  = 0x278   // в суперблоке ext4: volume name, 16 байт
	gptNameOffset    = 0x48    // в GPT header (LBA 1): disk name, 72 байта UTF-16LE
	superblockSize   = 1024   // суперблок ext4 начинается с offset 1024 от начала партиции
	readSectors      = 32768  // 16MB — покрывает выравнивание партиции до 8MB (16384 секторов)
)

// GetVolumeLabelFromImage читает метку тома из образа (ext4 — из суперблока первой партиции).
// Требует qemu-img. Возвращает метку (без пробелов по краям) или "disk" при ошибке/пустой метке.
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
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		logrus.Debugf("[NBD-LABEL] qemu-img dd: %s: %v", string(out), err)
		return "disk"
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil || len(data) < 1024 {
		return "disk"
	}

	// Сначала GPT: имя диска в заголовке (LBA 1)
	if len(data) >= 512+72+72 && bytes.Equal(data[512:520], []byte("EFI PART")) {
		nameUTF16 := data[512+gptNameOffset : 512+gptNameOffset+72]
		if s := decodeUTF16LE(nameUTF16); s != "" {
			return sanitizeExportName(s)
		}
	}

	// MBR: партиция 1 — LBA начала (4 байта LE) по смещению 0x1C6
	if len(data) < mbrPart1StartLBA+4 {
		return "disk"
	}
	part1LBA := binary.LittleEndian.Uint32(data[mbrPart1StartLBA : mbrPart1StartLBA+4])
	if part1LBA == 0 {
		return "disk"
	}

	// Суперблок ext4: offset = part1LBA*512 + 1024
	superOff := int(part1LBA)*512 + superblockSize
	if superOff+1024 > len(data) {
		return "disk"
	}
	super := data[superOff:]
	if len(super) < ext4MagicOffset+2 {
		return "disk"
	}
	// ext4 magic 0xEF53 в little-endian
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

// sanitizeExportName убирает символы, недопустимые в имени экспорта (пробелы → подчёркивания и т.д.).
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
	// Ограничиваем длину (NBD/API могут иметь лимит)
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}
