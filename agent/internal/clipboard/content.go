// Package clipboard syncs the system clipboard (text, images, files) with
// the paired peer over an authenticated duplex channel.
package clipboard

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
)

type Kind string

const (
	KindText  Kind = "text"
	KindImage Kind = "image"
	KindFile  Kind = "file"
)

// FileItem is one file (or directory) carried by a KindFile clipboard
// payload. When IsDir is true, Data is a tar archive of the directory's
// contents (see tarDir/untarDir) rather than a single file's raw bytes.
type FileItem struct {
	Name  string
	Data  []byte
	IsDir bool
}

// Content is a clipboard payload of exactly one kind.
type Content struct {
	Kind  Kind
	Text  string
	Image []byte // PNG-encoded
	Files []FileItem
}

// Size returns the payload's total byte size, checked against the configured
// per-transfer cap before it's put on the wire or written to the native
// clipboard.
func (c Content) Size() int64 {
	switch c.Kind {
	case KindText:
		return int64(len(c.Text))
	case KindImage:
		return int64(len(c.Image))
	case KindFile:
		var n int64
		for _, f := range c.Files {
			n += int64(len(f.Data))
		}
		return n
	default:
		return 0
	}
}

// Hash fingerprints the payload so the sync loop can recognize its own echo
// (see Manager) and skip retransmitting content that hasn't actually changed.
func (c Content) Hash() string {
	h := sha256.New()
	h.Write([]byte(c.Kind))
	switch c.Kind {
	case KindText:
		h.Write([]byte(c.Text))
	case KindImage:
		h.Write(c.Image)
	case KindFile:
		for _, f := range c.Files {
			h.Write([]byte(f.Name))
			if f.IsDir {
				h.Write([]byte{1})
			} else {
				h.Write([]byte{0})
			}
			h.Write(f.Data)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Empty reports whether the payload carries no actual content.
func (c Content) Empty() bool {
	switch c.Kind {
	case KindText:
		return c.Text == ""
	case KindImage:
		return len(c.Image) == 0
	case KindFile:
		return len(c.Files) == 0
	default:
		return true
	}
}

// EncodeFiles packs multiple files (and/or directories) into a single blob
// for wire transfer — a sequence of [4-byte name length][name][1-byte
// isDir][8-byte data length][data] — so a multi-file clipboard copy
// (selecting several files in a file manager) still moves as one blob over
// one blob-transfer request instead of needing a blob ID per file. For a
// directory entry, data is a tar archive of its contents (see tarDir).
func EncodeFiles(files []FileItem) []byte {
	var buf bytes.Buffer
	for _, f := range files {
		nameBytes := []byte(f.Name)
		var nameLen [4]byte
		binary.BigEndian.PutUint32(nameLen[:], uint32(len(nameBytes)))
		buf.Write(nameLen[:])
		buf.Write(nameBytes)
		if f.IsDir {
			buf.WriteByte(1)
		} else {
			buf.WriteByte(0)
		}
		var dataLen [8]byte
		binary.BigEndian.PutUint64(dataLen[:], uint64(len(f.Data)))
		buf.Write(dataLen[:])
		buf.Write(f.Data)
	}
	return buf.Bytes()
}

// DecodeFiles reverses EncodeFiles.
func DecodeFiles(data []byte) ([]FileItem, error) {
	var files []FileItem
	r := bytes.NewReader(data)
	for r.Len() > 0 {
		var nameLen [4]byte
		if _, err := io.ReadFull(r, nameLen[:]); err != nil {
			return nil, fmt.Errorf("clipboard: decode file name length: %w", err)
		}
		nameBuf := make([]byte, binary.BigEndian.Uint32(nameLen[:]))
		if _, err := io.ReadFull(r, nameBuf); err != nil {
			return nil, fmt.Errorf("clipboard: decode file name: %w", err)
		}
		isDirByte, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("clipboard: decode file isDir flag: %w", err)
		}
		var dataLen [8]byte
		if _, err := io.ReadFull(r, dataLen[:]); err != nil {
			return nil, fmt.Errorf("clipboard: decode file data length: %w", err)
		}
		dataBuf := make([]byte, binary.BigEndian.Uint64(dataLen[:]))
		if _, err := io.ReadFull(r, dataBuf); err != nil {
			return nil, fmt.Errorf("clipboard: decode file data: %w", err)
		}
		files = append(files, FileItem{Name: string(nameBuf), Data: dataBuf, IsDir: isDirByte == 1})
	}
	return files, nil
}
