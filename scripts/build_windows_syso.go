package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/josephspurrier/goversioninfo"
)

type iconEntry struct {
	size int
	data []byte
}

func main() {
	var (
		iconDir = flag.String("icon-dir", "", "Directory with appicon-16.png/appicon-32.png/appicon-256.png")
		outPath = flag.String("out", "", "Output .syso path")
		name    = flag.String("name", "USBridge Client", "Product name")
		version = flag.String("version", "1.0.0", "Product version")
		arch    = flag.String("arch", "amd64", "GOARCH")
	)
	flag.Parse()

	if *iconDir == "" || *outPath == "" {
		fail("icon-dir and out are required")
	}

	icoPath := filepath.Join(filepath.Dir(*outPath), "FyneApp.ico")
	manifestPath := filepath.Join(filepath.Dir(*outPath), "FyneApp.exe.manifest")

	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		fail("create output dir: %v", err)
	}

	if err := writeMultiIconICO(*iconDir, icoPath); err != nil {
		fail("write ico: %v", err)
	}
	defer os.Remove(icoPath)

	if err := os.WriteFile(manifestPath, []byte(windowsManifest(*name, *version)), 0o644); err != nil {
		fail("write manifest: %v", err)
	}
	defer os.Remove(manifestPath)

	vi := &goversioninfo.VersionInfo{}
	vi.ProductName = *name
	vi.IconPath = icoPath
	vi.ManifestPath = manifestPath
	vi.StringFileInfo.ProductVersion = *version
	vi.StringFileInfo.FileDescription = *name
	vi.FixedFileInfo.FileVersion = fixedVersionInfo(*version)

	vi.Build()
	vi.Walk()

	if err := vi.WriteSyso(*outPath, *arch); err != nil {
		fail("write syso: %v", err)
	}
}

func writeMultiIconICO(iconDir, outPath string) error {
	entries := make([]iconEntry, 0, 3)
	for _, size := range []int{16, 32, 256} {
		filename := filepath.Join(iconDir, fmt.Sprintf("appicon-%d.png", size))
		data, err := os.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("read %s: %w", filename, err)
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("decode %s: %w", filename, err)
		}
		if cfg.Width != size || cfg.Height != size {
			return fmt.Errorf("%s must be %dx%d, got %dx%d", filename, size, size, cfg.Width, cfg.Height)
		}
		entries = append(entries, iconEntry{size: size, data: data})
	}

	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.LittleEndian, uint16(0)); err != nil {
		return err
	}
	if err := binary.Write(&buf, binary.LittleEndian, uint16(1)); err != nil {
		return err
	}
	if err := binary.Write(&buf, binary.LittleEndian, uint16(len(entries))); err != nil {
		return err
	}

	offset := 6 + 16*len(entries)
	for _, entry := range entries {
		width := byte(entry.size)
		height := byte(entry.size)
		if entry.size >= 256 {
			width = 0
			height = 0
		}
		buf.WriteByte(width)
		buf.WriteByte(height)
		buf.WriteByte(0)
		buf.WriteByte(0)
		if err := binary.Write(&buf, binary.LittleEndian, uint16(1)); err != nil {
			return err
		}
		if err := binary.Write(&buf, binary.LittleEndian, uint16(32)); err != nil {
			return err
		}
		if err := binary.Write(&buf, binary.LittleEndian, uint32(len(entry.data))); err != nil {
			return err
		}
		if err := binary.Write(&buf, binary.LittleEndian, uint32(offset)); err != nil {
			return err
		}
		offset += len(entry.data)
	}

	for _, entry := range entries {
		if _, err := buf.Write(entry.data); err != nil {
			return err
		}
	}

	return os.WriteFile(outPath, buf.Bytes(), 0o644)
}

func fixedVersionInfo(ver string) (ret goversioninfo.FileVersion) {
	ret.Build = 1
	parts := strings.Split(ver, ".")
	setVersionField(&ret.Major, parts, 0)
	setVersionField(&ret.Minor, parts, 1)
	setVersionField(&ret.Patch, parts, 2)
	setVersionField(&ret.Build, parts, 3)
	return ret
}

func setVersionField(target *int, parts []string, index int) {
	if index >= len(parts) {
		return
	}
	n, err := strconv.Atoi(parts[index])
	if err == nil {
		*target = n
	}
}

func windowsManifest(name, version string) string {
	escapedName := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&apos;").Replace(name)
	assemblyVersion := manifestAssemblyVersion(version)
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity version="%s" processorArchitecture="*" name="USBridge.Client" type="win32"/>
  <description>%s</description>
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
</assembly>
`, assemblyVersion, escapedName)
}

func manifestAssemblyVersion(version string) string {
	parts := strings.Split(version, ".")
	normalized := [4]string{"0", "0", "0", "0"}
	for i := 0; i < len(normalized) && i < len(parts); i++ {
		if _, err := strconv.Atoi(parts[i]); err == nil {
			normalized[i] = parts[i]
		}
	}
	return strings.Join(normalized[:], ".")
}

func fail(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
