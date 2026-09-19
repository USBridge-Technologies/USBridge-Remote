package remotelock

import (
	"bufio"
	"strconv"
	"strings"
)

func eventPathsFromDevices(data string) []string {
	var paths []string
	var name string
	var vendor, product uint16
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			name, vendor, product = "", 0, 0
			continue
		}
		switch {
		case strings.HasPrefix(line, "I:"):
			vendor = parseHexField(line, "Vendor=")
			product = parseHexField(line, "Product=")
		case strings.HasPrefix(line, "N: Name="):
			name = strings.Trim(strings.TrimPrefix(line, "N: Name="), `"`)
		case strings.HasPrefix(line, "H: Handlers="):
			if !isVirtualInput(name, vendor, product) {
				continue
			}
			for _, tok := range strings.Fields(strings.TrimPrefix(line, "H: Handlers=")) {
				if strings.HasPrefix(tok, "event") {
					paths = append(paths, "/dev/input/"+tok)
				}
			}
		}
	}
	return paths
}

func parseHexField(line, key string) uint16 {
	i := strings.Index(strings.ToLower(line), strings.ToLower(key))
	if i < 0 {
		return 0
	}
	rest := line[i+len(key):]
	end := strings.IndexAny(rest, " \t")
	if end >= 0 {
		rest = rest[:end]
	}
	v, err := strconv.ParseUint(rest, 16, 16)
	if err != nil {
		return 0
	}
	return uint16(v)
}

func isVirtualInput(name string, vendor, product uint16) bool {
	if vendor == 0xbeef && product == 0xdead {
		return true
	}
	n := strings.ToLower(name)
	for _, p := range []string{
		"mouse passthrough",
		"keyboard passthrough",
		"touch passthrough",
		"pen passthrough",
		"usbridge-mouse",
		"usbridge-keyboard",
		"usbridge-streamer",
		"gamestream-server",
	} {
		if strings.Contains(n, p) {
			return true
		}
	}
	return n == "gamestream"
}
