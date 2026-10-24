//go:build linux

package hostload

import (
	"bufio"
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Linux: CPU from /proc (the whole machine from /proc/stat, the streamer
// processes from /proc/<pid>/stat). GPU:
//   - NVIDIA (proprietary driver): nvidia-smi -- the whole GPU's
//     3d/encode/decode, the streamer's share from "nvidia-smi pmon";
//   - everything else (Intel i915/xe, AMD amdgpu, ...): the kernel's DRM
//     fdinfo (5.19+), each process's busy time per engine in
//     /proc/<pid>/fdinfo -- readable without root for this user's
//     processes (the streamer included); the whole GPU is the sum over
//     the clients we can see, or amdgpu's gpu_busy_percent where larger.

func logf(format string, args ...any) { log.Printf("[hostload] "+format, args...) }

type reader struct {
	busy0, total0 uint64
	procs0        map[int]uint64 // streamer pid -> utime+stime

	nv     *nvidiaWatch
	amdGPU []string // gpu_busy_percent files

	drm0   map[string]drmClient // DRM fdinfo, by client id
	drmAt0 time.Time
}

func newReader() (*reader, error) {
	busy, total, err := cpuJiffies()
	if err != nil {
		return nil, err
	}
	r := &reader{busy0: busy, total0: total, procs0: streamerJiffies()}
	if path, err := exec.LookPath("nvidia-smi"); err == nil {
		r.nv = startNvidiaWatch(path)
	} else {
		r.amdGPU, _ = filepath.Glob("/sys/class/drm/card*/device/gpu_busy_percent")
		r.drm0, r.drmAt0 = drmClients(), time.Now()
	}
	return r, nil
}

func (r *reader) close() {
	if r.nv != nil {
		r.nv.stop()
	}
}

func (r *reader) read() (Sample, error) {
	busy, total, err := cpuJiffies()
	if err != nil {
		return Sample{}, err
	}
	procs := streamerJiffies()
	var s Sample
	if dt := float64(total - r.total0); dt > 0 {
		s.CPU = 100 * float64(busy-r.busy0) / dt
		var used uint64
		for pid, j := range procs {
			if j0, ok := r.procs0[pid]; ok && j >= j0 {
				used += j - j0
			}
		}
		s.StreamerCPU = 100 * float64(used) / dt
	}
	r.busy0, r.total0, r.procs0 = busy, total, procs

	if r.nv != nil {
		s.GPU, s.StreamerGPU = r.nv.values()
		return s, nil
	}
	now, clients := time.Now(), drmClients()
	s.GPU, s.StreamerGPU = drmLoad(r.drm0, clients, now.Sub(r.drmAt0))
	r.drm0, r.drmAt0 = clients, now
	if len(r.amdGPU) > 0 {
		var max float64
		for _, f := range r.amdGPU {
			if data, err := os.ReadFile(f); err == nil {
				if v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil && v > max {
					max = v
				}
			}
		}
		if s.GPU == nil {
			s.GPU = map[string]float64{}
		}
		if max > s.GPU["3d"] {
			s.GPU["3d"] = max
		}
	}
	return s, nil
}

// cpuJiffies: busy and total jiffies of all CPUs (/proc/stat "cpu" line).
func cpuJiffies() (busy, total uint64, err error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	f := strings.Fields(strings.SplitN(string(data), "\n", 2)[0])
	if len(f) < 5 || f[0] != "cpu" {
		return 0, 0, errors.New("/proc/stat: no cpu line")
	}
	for i, v := range f[1:] {
		n, _ := strconv.ParseUint(v, 10, 64)
		total += n
		if i != 3 && i != 4 { // idle, iowait
			busy += n
		}
	}
	return busy, total, nil
}

// streamerJiffies: utime+stime of each streamer process (same unit as
// /proc/stat). The name comes from argv[0]: /proc/<pid>/comm is cut at 15
// characters ("usbridge-stream").
func streamerJiffies() map[int]uint64 {
	out := map[int]uint64{}
	dirs, _ := filepath.Glob("/proc/[0-9]*")
	for _, d := range dirs {
		pid, err := strconv.Atoi(filepath.Base(d))
		if err != nil || !isStreamer(procName(d)) {
			continue
		}
		stat, err := os.ReadFile(filepath.Join(d, "stat"))
		if err != nil {
			continue
		}
		st := string(stat)
		if i := strings.LastIndexByte(st, ')'); i >= 0 {
			st = st[i+1:]
		}
		f := strings.Fields(st)
		if len(f) < 13 { // after "(comm)": state f[0] ... utime f[11], stime f[12]
			continue
		}
		u, _ := strconv.ParseUint(f[11], 10, 64)
		s, _ := strconv.ParseUint(f[12], 10, 64)
		out[pid] = u + s
	}
	return out
}

func procName(dir string) string {
	if cmd, err := os.ReadFile(filepath.Join(dir, "cmdline")); err == nil && len(cmd) > 0 {
		argv0 := string(cmd)
		if i := strings.IndexByte(argv0, 0); i >= 0 {
			argv0 = argv0[:i]
		}
		if argv0 != "" {
			return filepath.Base(argv0)
		}
	}
	comm, _ := os.ReadFile(filepath.Join(dir, "comm"))
	return strings.TrimSpace(string(comm))
}

// nvidiaWatch keeps two nvidia-smi processes running for the length of a
// recording -- one starting a process per 500 ms tick would cost more than
// it measures: the whole GPU ("--query-gpu ... -lms") and per-process use
// ("pmon", once a second), and keeps the latest of each.
type nvidiaWatch struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu       sync.Mutex
	gpu      map[string]float64
	streamer map[string]float64
}

func startNvidiaWatch(path string) *nvidiaWatch {
	ctx, cancel := context.WithCancel(context.Background())
	w := &nvidiaWatch{cancel: cancel}
	w.run(ctx, path, []string{"--query-gpu=utilization.gpu,utilization.encoder,utilization.decoder", "--format=csv,noheader,nounits", "-lms", strconv.Itoa(int(Interval.Milliseconds()))}, w.gpuLine)
	w.run(ctx, path, []string{"pmon", "-s", "u", "-d", "1"}, w.pmonLine)
	return w
}

func (w *nvidiaWatch) run(ctx context.Context, path string, args []string, line func(string)) {
	cmd := exec.CommandContext(ctx, path, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		logf("nvidia-smi: %v", err)
		return
	}
	if err := cmd.Start(); err != nil {
		logf("nvidia-smi: %v", err)
		return
	}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			line(sc.Text())
		}
		cmd.Wait()
	}()
}

func (w *nvidiaWatch) stop() {
	w.cancel()
	w.wg.Wait()
}

func (w *nvidiaWatch) values() (gpu, streamer map[string]float64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return copyMap(w.gpu), copyMap(w.streamer)
}

func copyMap(m map[string]float64) map[string]float64 {
	if m == nil {
		return nil
	}
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// gpuLine: "37, 12, 0" (gpu, encoder, decoder %), one line per GPU: the
// busiest GPU per engine, like the Windows counters' busiest engine.
func (w *nvidiaWatch) gpuLine(line string) {
	f := strings.Split(line, ",")
	if len(f) != 3 {
		return
	}
	keys := []string{"3d", "encode", "decode"}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.gpu == nil {
		w.gpu = map[string]float64{}
	}
	for i, k := range keys {
		if v, err := strconv.ParseFloat(strings.TrimSpace(f[i]), 64); err == nil {
			w.gpu[k] = v
		}
	}
}

// pmonLine: "    0   1234     C     40     10     25      0  ...  sunshine"
// (gpu, pid, type, sm, mem, enc, dec, ..., command); "-" when idle. A
// header ("#") starts each round: the streamer's sums restart there.
func (w *nvidiaWatch) pmonLine(line string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if strings.HasPrefix(line, "#") {
		if strings.Contains(line, "pid") {
			w.streamer = map[string]float64{"3d": 0, "encode": 0, "decode": 0}
		}
		return
	}
	f := strings.Fields(line)
	if len(f) < 8 || w.streamer == nil {
		return
	}
	// The command column is comm (cut at 15 characters, and whatever the
	// process renamed itself to: usbridge-streamer's main thread is "5"),
	// so go by the pid's argv[0] and keep the column as a fallback.
	if !isStreamer(procName("/proc/"+f[1])) && !isStreamer(f[len(f)-1]) {
		return
	}
	add := func(key, v string) {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			w.streamer[key] += n
		}
	}
	add("3d", f[3])
	add("encode", f[5])
	add("decode", f[6])
}

// drmClient is one DRM client (an open GPU file) from fdinfo: busy
// nanoseconds per engine, and whether a streamer process owns it.
type drmClient struct {
	engines  map[string]uint64
	streamer bool
}

// drmClients reads every DRM client this user can see: /proc/<pid>/fdinfo
// of each file open on /dev/dri, deduplicated by drm-client-id (a client
// shared by several fds or a forked process counts once).
func drmClients() map[string]drmClient {
	out := map[string]drmClient{}
	dirs, _ := filepath.Glob("/proc/[0-9]*")
	for _, d := range dirs {
		fds, err := os.ReadDir(filepath.Join(d, "fd"))
		if err != nil {
			continue
		}
		streamer := false
		named := false
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(d, "fd", fd.Name()))
			if err != nil || !strings.HasPrefix(target, "/dev/dri/") {
				continue
			}
			if !named {
				streamer, named = isStreamer(procName(d)), true
			}
			data, err := os.ReadFile(filepath.Join(d, "fdinfo", fd.Name()))
			if err != nil {
				continue
			}
			id, engines := parseDRMFdinfo(string(data))
			if id == "" || len(engines) == 0 {
				continue
			}
			c := out[id]
			if c.engines == nil {
				c.engines = engines
			}
			c.streamer = c.streamer || streamer
			out[id] = c
		}
	}
	return out
}

// parseDRMFdinfo picks the client id ("drm-pdev" + "drm-client-id": ids
// are per device) and the engines' busy time ("drm-engine-<name>: N ns")
// out of one fdinfo file.
func parseDRMFdinfo(text string) (id string, engines map[string]uint64) {
	var pdev, client string
	for _, line := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch {
		case k == "drm-pdev":
			pdev = v
		case k == "drm-client-id":
			client = v
		case strings.HasPrefix(k, "drm-engine-") && !strings.HasPrefix(k, "drm-engine-capacity-"):
			n, err := strconv.ParseUint(strings.TrimSuffix(v, " ns"), 10, 64)
			if err != nil {
				continue
			}
			if engines == nil {
				engines = map[string]uint64{}
			}
			engines[strings.TrimPrefix(k, "drm-engine-")] = n
		}
	}
	if client == "" {
		return "", nil
	}
	return pdev + "/" + client, engines
}

// drmEngineKind maps a driver's engine name onto the Sample keys: i915
// "render"/"video"/"video-enhance"/"copy", xe "rcs"/"vcs"/"vecs"/"bcs"/
// "ccs", amdgpu "gfx"/"enc"/"dec"/"compute"/"dma". Intel's video engine
// does both encode and decode: it's counted as encode (what a streamer
// uses it for).
func drmEngineKind(engine string) string {
	switch engine {
	case "render", "gfx", "rcs", "ccs", "compute":
		return "3d"
	case "video", "vcs", "enc", "enc_1", "vcn":
		return "encode"
	case "dec", "jpeg":
		return "decode"
	case "copy", "bcs", "dma", "video-enhance", "vecs":
		return "copy"
	}
	return ""
}

// drmLoad: per engine kind, the share of dt the clients kept it busy
// (the whole GPU: every client; the streamer: its own), capped at 100.
func drmLoad(prev, cur map[string]drmClient, dt time.Duration) (gpu, streamer map[string]float64) {
	if dt <= 0 || len(cur) == 0 {
		return nil, nil
	}
	gpu, streamer = map[string]float64{}, map[string]float64{}
	any := false
	for id, c := range cur {
		p, ok := prev[id]
		if !ok {
			continue
		}
		for engine, ns := range c.engines {
			kind := drmEngineKind(engine)
			if kind == "" || ns < p.engines[engine] {
				continue
			}
			pct := 100 * float64(ns-p.engines[engine]) / float64(dt.Nanoseconds())
			gpu[kind] += pct
			if c.streamer {
				streamer[kind] += pct
			}
			any = true
		}
	}
	if !any {
		return nil, nil
	}
	for _, m := range []map[string]float64{gpu, streamer} {
		for k, v := range m {
			if v > 100 {
				m[k] = 100
			}
		}
	}
	return gpu, streamer
}
