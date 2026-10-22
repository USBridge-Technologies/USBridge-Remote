package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestFramePacer runs moonlight-common-c's FramePacer.h (the jitter buffer
// every platform shares) through the host C compiler against a simulated
// 60.56 fps capture-card stream over Wi-Fi: frames must come out evenly
// spaced, while the latency it adds stays near the measured spread -- the old
// pacer ratcheted toward the slowest frames and added 3x the jitter on top.
func TestFramePacer(t *testing.T) {
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no C compiler")
	}
	hdr, _ := filepath.Abs("../../moonlight-common-c/src/FramePacer.h")
	if _, err := os.Stat(hdr); err != nil {
		t.Skip("moonlight-common-c submodule not checked out")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "pacer.c")
	prog := `#include "` + hdr + `"
#include <stdio.h>
#include <stdlib.h>
#include <math.h>

static unsigned rng = 1;
static double urand(void) { rng = rng * 1103515245u + 12345u; return ((rng >> 8) & 0xffff) / 65536.0; }

/* One run: returns 0 if it met the bounds. jitter_us: uniform spread on top
   of the path's fixed delay; spike_every: every Nth frame arrives 30 ms late.
   uneven: intervals more than 4 ms off the frame period -- what reads as
   judder on a 120 Hz panel. */
static int run(const char *name, int level, double jitter_us, int spike_every, double max_uneven, double max_added_us) {
    FRAME_PACER p; FramePacerConfigure(&p, level);
    const double period = 1e6 / 60.56;
    double prev = 0, added = 0; int n = 0, late = 0, uneven = 0;
    for (int i = 0; i < 3000; i++) {
        int64_t pts = (int64_t)(i * period) + 1000000;
        double ready = pts + 25000 + jitter_us * urand() + ((spike_every && i % spike_every == 0) ? 30000 : 0);
        int64_t target = FramePacerTarget(&p, pts, (int64_t)ready);
        double rel = target > ready ? target : ready;
        if (i >= 300) { /* after settling */
            double d = rel - prev; n++;
            if (fabs(d - period) > 4000) uneven++;
            added += rel - ready;
            if (target < ready) late++;
        }
        prev = rel;
    }
    printf("L%d %s: uneven %d/%d, added %.1f ms avg, late %d\n", level, name, uneven, n, added / n / 1000, late);
    int bad = 0;
    if (uneven > max_uneven * n) { printf("FAIL %s: uneven %d/%d > %.1f%%\n", name, uneven, n, max_uneven * 100); bad = 1; }
    if (added / n > max_added_us) { printf("FAIL %s: added %.0f > %.0f us\n", name, added / n, max_added_us); bad = 1; }
    return bad;
}

int main(void) {
    int bad = 0;
    for (int level = 1; level <= FRAME_PACER_LEVELS; level++) {
        bad |= run("clean link", level, 300, 0, 0, 3000);
        /* each spike: its own frame late, the next on time -- 2 uneven intervals */
        bad |= run("wifi + spikes", level, 8000, 120, 0.02, 6000);
    }
    bad |= run("wifi 8ms", 3, 8000, 0, 0.01, 6000);
    /* the smoother the level, the more frames on time on the same link */
    {
        double prev_unev = 1e9;
        for (int level = 1; level <= FRAME_PACER_LEVELS; level++) {
            FRAME_PACER p; FramePacerConfigure(&p, level);
            const double period = 1e6 / 60.56; double prevr = 0; int uneven = 0;
            rng = 7;
            for (int i = 0; i < 6000; i++) {
                int64_t pts = (int64_t)(i * period) + 1000000;
                double r = urand(), ready = pts + 25000 + (r < 0.9 ? 6000 * urand() : 6000 + 20000 * urand());
                int64_t t = FramePacerTarget(&p, pts, (int64_t)ready);
                double rel = t > ready ? t : ready;
                if (i > 600 && fabs(rel - prevr - period) > 4000) uneven++;
                prevr = rel;
            }
            printf("L%d heavy-tail link: uneven %d/5399\n", level, uneven);
            if (uneven > prev_unev) { printf("FAIL: level %d less smooth than level %d\n", level, level - 1); bad = 1; }
            prev_unev = uneven;
        }
    }
    /* unpaced, the same link is judder on nearly every other frame */
    {
        double period = 1e6 / 60.56, prev = 0; int uneven = 0;
        for (int i = 0; i < 3000; i++) {
            double ready = i * period + 25000 + 8000 * urand();
            if (i && fabs(ready - prev - period) > 4000) uneven++;
            prev = ready;
        }
        printf("unpaced wifi 8ms: uneven %d/2999\n", uneven);
    }
    /* A host clock running 200 ppm fast: base must follow, not reset. */
    {
        FRAME_PACER p; FramePacerConfigure(&p, 3);
        int resets = 0; int64_t last_margin = 0;
        for (int i = 0; i < 20000; i++) {
            int64_t pts = (int64_t)(i * 16512.0);
            int64_t ready = (int64_t)(pts * 1.0002) + 20000;
            int64_t t = FramePacerTarget(&p, pts, ready);
            if (i > 0 && t == ready && last_margin) resets++;
            last_margin = FramePacerMarginUs(&p);
        }
        if (resets) { printf("FAIL drift: %d resets\n", resets); bad = 1; }
    }
    return bad;
}
`
	if err := os.WriteFile(src, []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "pacer")
	if out, err := exec.Command(cc, "-O1", "-o", bin, src, "-lm").CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	t.Logf("\n%s", strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("FramePacer: %v", err)
	}
}

// TestPlayoutBufferLevelsMatchPacer: the dialog's slider range must be the
// levels FramePacer.h defines.
func TestPlayoutBufferLevelsMatchPacer(t *testing.T) {
	b, err := os.ReadFile("../../moonlight-common-c/src/FramePacer.h")
	if err != nil {
		t.Skip("moonlight-common-c submodule not checked out")
	}
	if !strings.Contains(string(b), "#define FRAME_PACER_LEVELS "+strconv.Itoa(PlayoutBufferMaxLevel)+"\n") {
		t.Errorf("PlayoutBufferMaxLevel = %d, not FramePacer.h's FRAME_PACER_LEVELS", PlayoutBufferMaxLevel)
	}
	if ClampPlayoutBufferLevel(-1) != 0 || ClampPlayoutBufferLevel(99) != PlayoutBufferMaxLevel {
		t.Error("ClampPlayoutBufferLevel does not bound to 0..PlayoutBufferMaxLevel")
	}
}
