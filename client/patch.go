package main

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"regexp"
	"strings"
)

func main() {
	b, err := ioutil.ReadFile("internal/service/shader_arrays.h")
	if err != nil {
		panic(err)
	}
	s := string(b)

	warpRe := regexp.MustCompile(`(?s)//   warp_extrapolate\.comp:\r?\n(.*)$`)
	warpMatch := warpRe.FindStringSubmatch(s)
	if len(warpMatch) < 2 {
		panic("shader not found in header")
	}

	lines := strings.Split(warpMatch[1], "\n")
	var out []string
	for _, l := range lines {
		l = strings.TrimRight(l, "\r")
		if strings.Contains(l, "glslc") {
			break
		}
		if strings.HasPrefix(l, "//     ") {
			out = append(out, l[7:])
		} else if strings.HasPrefix(l, "// ") {
			out = append(out, l[3:])
		} else if strings.TrimSpace(l) == "" {
			continue
		} else {
            out = append(out, l)
        }
	}
	shaderText := strings.Join(out, "\n")

	// Inject logic right before "vec2 srcPx"
	// Find the line index
	lines = strings.Split(shaderText, "\n")
	for i, line := range lines {
		if strings.Contains(line, "vec2 srcPx") {
			injection := `    // 1. HARD CLAMP
    float max_disp = 24.0;
    if (length(v) > max_disp) {
        v = normalize(v) * max_disp;
    }
    // 2. DAMPING (A*k=1 for linear start, asymptotes at A=3.0)
    float t_eff = 3.0 * (1.0 - exp(-0.3333 * pc.extrapolateT));
`
			line = strings.ReplaceAll(line, "pc.extrapolateT", "t_eff")
			lines[i] = injection + line
			break
		}
	}

	shaderText = strings.Join(lines, "\n")
	ioutil.WriteFile("warp_extrapolate.comp", []byte(shaderText), 0644)
	fmt.Println("Wrote warp_extrapolate.comp")

	cmd := exec.Command("glslc", "-fshader-stage=compute", "warp_extrapolate.comp", "-o", "warp_extrapolate.spv")
	outBytes, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(string(outBytes))
		panic(err)
	}

	spvBytes, err := ioutil.ReadFile("warp_extrapolate.spv")
	if err != nil {
		panic(err)
	}

	var hex []string
	for i := 0; i < len(spvBytes); i += 4 {
		// Little endian uint32
		val := uint32(spvBytes[i]) | uint32(spvBytes[i+1])<<8 | uint32(spvBytes[i+2])<<16 | uint32(spvBytes[i+3])<<24
		hex = append(hex, fmt.Sprintf("0x%08xu", val))
	}

	// Format neatly
	var arrayStrBuilder strings.Builder
	arrayStrBuilder.WriteString("static const uint32_t g_warp_extrapolate_comp_spv[] = {\n")
	for i, h := range hex {
		arrayStrBuilder.WriteString(h)
		arrayStrBuilder.WriteString(",")
		if (i+1)%4 == 0 {
			arrayStrBuilder.WriteString("\n")
		}
	}
	arrayStrBuilder.WriteString("\n};")
	arrayStr := arrayStrBuilder.String()

	arrayRe := regexp.MustCompile(`(?s)static const uint32_t g_warp_extrapolate_comp_spv\[\] = \{.*?\};`)
	s = arrayRe.ReplaceAllString(s, arrayStr)
	ioutil.WriteFile("internal/service/shader_arrays.h", []byte(s), 0644)
	fmt.Println("Patched shader_arrays.h successfully!")
}
