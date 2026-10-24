// Package benchvideo plays the streamer benchmark's test content on the
// host: a native 60fps 1080p clip (a 30s cut of Blender's "Big Buck Bunny",
// CC-BY) fullscreen and looping, played as-is with no filter chain.
//
// This used to be the (24fps) Sintel trailer with an ffmpeg filter chain
// bolted on: an fps=60 conversion plus a small white marker square overlaid
// and re-timed to 60fps, needed because a 24fps source only changes the
// screen 24 times a second, and a capture path that only wakes on real
// damage (DXGI Desktop Duplication, see rust-shine's scripts/
// bench_animator.ps1) would legitimately send fewer frames at that rate.
// That software filter chain (fps conversion + overlay compositing, both
// per-frame CPU work on top of decode) was itself the bottleneck on modest
// hosts: ffplay's *output* dropped to a 0.5-1fps slideshow even though the
// source decoded fine, which made the benchmark measure ffplay's filter
// chain instead of the streamer. A source that's already native 60fps
// (every one of its own frames is a new picture) needs neither the fps
// conversion nor the marker, so there's no filter chain left to bottleneck
// on, and no marker square left to show up in every recorded frame either.
//
// The client's benchmark (client/internal/gui/benchmark_runner.go) starts
// the content only after its stream has shown the first frame, so the
// streamers' different startup times never shift which part of the clip
// each one is measured on.
package benchvideo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"usbridge_agent/internal/monitors"
)

const (
	// ContentURL is a 30s cut of Blender's "Big Buck Bunny" (CC-BY 3.0),
	// natively encoded at 1920x1080@60fps -- every frame is already a new
	// picture, unlike the 24fps trailer this replaced (see this package's
	// doc comment for why that mattered). ~14MB, so the first run doesn't
	// stall on a download.
	ContentURL  = "https://raw.githubusercontent.com/bower-media-samples/big-buck-bunny-1080p-60fps-30s/master/video.mp4"
	contentName = "big_buck_bunny_1080p_60fps_30s.mp4"
	// contentMinBytes rejects an HTML error page saved as the video.
	contentMinBytes = 1 << 20

	// maxPlayTime stops the player on its own if the client that started
	// it vanishes mid-benchmark, so a fullscreen video never stays on the
	// host's screen indefinitely.
	maxPlayTime = 15 * time.Minute

	windowTitle = "USBridge Benchmark"
)

// Info describes the player state reported to the client.
type Info struct {
	Player      string    `json:"player"`
	Content     string    `json:"content"`
	Playing     bool      `json:"playing"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	ContentPath string    `json:"content_path,omitempty"`
	// RequestedMonitor is the monitor Start was asked to play on ("" for
	// the player's own default); Monitor is where its window actually is,
	// "" when that couldn't be seen (the player runs in a session this
	// process can't inspect).
	RequestedMonitor string `json:"requested_monitor,omitempty"`
	Monitor          string `json:"monitor,omitempty"`
}

// process is a started player: an *exec.Cmd, or on Windows possibly a
// process launched into the interactive console session.
type process interface {
	Kill() error
	Wait() error
	Pid() int
}

// Player owns at most one running player process.
type Player struct {
	dir string

	mu       sync.Mutex
	proc     process
	info     Info
	stopTime *time.Timer
	dlMu     sync.Mutex
	fps      int
}

// SetFPS tells the next Start the frame rate the streamers are asked for. The clip is
// 60fps, so above that it is the ceiling of the whole benchmark: the desktop shows only
// 60 new pictures a second and no streamer can deliver more, however fast it is set up.
// Above 60 ffplay plays a generated pattern at that rate instead (the other players
// can't generate one and keep the clip).
func (p *Player) SetFPS(fps int) {
	p.mu.Lock()
	p.fps = fps
	p.mu.Unlock()
}

// New returns a Player caching its content under stateDir/bench.
func New(stateDir string) *Player {
	return &Player{dir: filepath.Join(stateDir, "bench")}
}

// Status reports whether a player is running and which one.
func (p *Player) Status() Info {
	p.mu.Lock()
	defer p.mu.Unlock()
	info := p.info
	info.Playing = p.proc != nil
	if info.Player == "" {
		info.Player, _ = findPlayer()
	}
	if info.ContentPath == "" {
		if path, ok := p.cachedContent(); ok {
			info.ContentPath = path
		}
	}
	return info
}

// Prepare makes sure the trailer is cached locally, downloading it once.
// USBRIDGE_BENCH_VIDEO points at a local file to use instead.
func (p *Player) Prepare(ctx context.Context) (string, error) {
	if path := os.Getenv("USBRIDGE_BENCH_VIDEO"); path != "" {
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("USBRIDGE_BENCH_VIDEO: %w", err)
		}
		return path, nil
	}
	p.dlMu.Lock()
	defer p.dlMu.Unlock()
	if path, ok := p.cachedContent(); ok {
		return path, nil
	}
	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(p.dir, contentName)
	tmp := dst + ".part"
	log.Printf("[bench] downloading benchmark content %s", ContentURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ContentURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download benchmark content: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download benchmark content: HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	n, err := io.Copy(f, resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n < contentMinBytes {
		err = fmt.Errorf("download benchmark content: only %d bytes", n)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return "", err
	}
	log.Printf("[bench] benchmark content cached at %s (%d bytes)", dst, n)
	return dst, nil
}

func (p *Player) cachedContent() (string, bool) {
	if path := os.Getenv("USBRIDGE_BENCH_VIDEO"); path != "" {
		return path, true
	}
	dst := filepath.Join(p.dir, contentName)
	if st, err := os.Stat(dst); err == nil && st.Size() >= contentMinBytes {
		return dst, true
	}
	return "", false
}

// Start (re)starts the content from its first frame, fullscreen on target
// (nil: the player's default monitor). If the trailer can't be fetched it
// falls back to ffplay's built-in testsrc2 pattern (still full-motion
// 60fps), which only ffplay can generate.
//
// output, when set, is the start of the name of a compositor output the
// player's window must be moved to: a streamer that shows a virtual display
// of its own (Punktfunk's "Virtual-punktfunk*") never sees a video that
// opened on the physical monitor, and the client would measure an empty
// desktop. Failing to get it there fails the start for the same reason.
func (p *Player) Start(ctx context.Context, target *monitors.Monitor, output string) (Info, error) {
	p.Stop()

	content, prepErr := p.Prepare(ctx)
	if prepErr != nil {
		log.Printf("[bench] %v -- falling back to a generated test pattern", prepErr)
	}
	player, path := findPlayer()
	if player == "" {
		return Info{}, errors.New("no video player on the host: install ffmpeg (ffplay), mpv or VLC")
	}
	p.mu.Lock()
	fps := p.fps
	p.mu.Unlock()
	if fps > 60 && player == "ffplay" {
		content = ""
		log.Printf("[bench] streamers run at %d fps: the 60fps clip would cap them, playing a generated %dfps pattern", fps, fps)
	}
	args, label, err := playerArgs(player, content)
	if err != nil {
		return Info{}, err
	}
	if content == "" && fps > 60 && player == "ffplay" {
		// Two boxes crossing a flat background: every frame is a new picture, and it
		// costs the host's CPU/GPU next to nothing (testsrc2 draws a whole test card).
		// (overlay re-evaluates its position every frame; drawbox does not.)
		args = append(args[:len(args)-1], fmt.Sprintf("color=c=0x202830:s=1920x1080:r=%[1]d[bg];"+
			"color=c=white:s=120x120:r=%[1]d[a];color=c=0x61f0d3:s=120x120:r=%[1]d[b];"+
			`[bg][a]overlay=x=mod(t*1500\,1800):y=300[m];[m][b]overlay=x=1800-mod(t*1000\,1800):y=660`, fps))
		label = fmt.Sprintf("moving boxes 1080p%d", fps)
	}
	if target != nil {
		args = append(placementArgs(player, *target), args...)
	}
	proc, err := launch(path, args)
	if err != nil {
		return Info{}, fmt.Errorf("start %s: %w", player, err)
	}

	exited := make(chan error, 1)
	go func() { exited <- proc.Wait() }()
	// A player that can't open a display exits right away; report that
	// instead of letting the client measure an empty desktop.
	select {
	case err := <-exited:
		return Info{}, fmt.Errorf("%s exited immediately: %v", player, err)
	case <-time.After(1500 * time.Millisecond):
	}

	info := Info{Player: player, Content: label, Playing: true, StartedAt: time.Now(), ContentPath: content}
	if output != "" {
		name, err := moveToOutput(proc.Pid(), output, placementTimeout)
		if err != nil {
			_ = proc.Kill()
			return Info{}, fmt.Errorf("putting the test video on the streamed display: %w", err)
		}
		log.Printf("[bench] test video moved to %s", name)
		info.RequestedMonitor, info.Monitor = name, name
	} else if target != nil {
		info.RequestedMonitor = target.ID
		info.Monitor = ensureOnMonitor(proc.Pid(), *target)
	}
	p.mu.Lock()
	p.proc = proc
	p.info = info
	p.stopTime = time.AfterFunc(maxPlayTime, func() {
		log.Printf("[bench] benchmark video ran %v without a stop -- stopping it", maxPlayTime)
		p.Stop()
	})
	p.mu.Unlock()
	go func() {
		<-exited
		p.mu.Lock()
		if p.proc == proc {
			p.proc = nil
			p.info.Playing = false
		}
		p.mu.Unlock()
	}()
	log.Printf("[bench] benchmark video started (%s, %s)", player, label)
	return info, nil
}

// Stop closes the player if one is running.
func (p *Player) Stop() {
	p.mu.Lock()
	proc := p.proc
	p.proc = nil
	p.info.Playing = false
	if p.stopTime != nil {
		p.stopTime.Stop()
		p.stopTime = nil
	}
	p.mu.Unlock()
	if proc != nil {
		_ = proc.Kill()
		log.Printf("[bench] benchmark video stopped")
	}
}

// placementArgs asks the player to open on m. Each player takes this
// differently: ffplay (SDL) positions its window in DPI-unaware
// coordinates and goes fullscreen on the monitor that window is on, mpv
// names the screen directly, VLC places its video window.
func placementArgs(player string, m monitors.Monitor) []string {
	switch player {
	case "ffplay":
		x, y, ok := monitors.LogicalOrigin(m.ID)
		if !ok {
			x, y = m.X, m.Y
		}
		return []string{"-left", fmt.Sprint(x + 50), "-top", fmt.Sprint(y + 50)}
	case "mpv":
		return []string{"--screen-name=" + m.ID, "--fs-screen-name=" + m.ID}
	case "vlc":
		return []string{fmt.Sprintf("--video-x=%d", m.X+50), fmt.Sprintf("--video-y=%d", m.Y+50)}
	}
	return nil
}

// placementTimeout bounds how long ensureOnMonitor waits for the player's
// window to show up and settle.
var placementTimeout = 5 * time.Second

// ensureOnMonitor checks which monitor pid's window landed on and moves it
// onto target when the player's own placement missed. Returns the monitor
// the window ends up on, "" if its window can't be seen from here.
func ensureOnMonitor(pid int, target monitors.Monitor) string {
	deadline := time.Now().Add(placementTimeout)
	moved := false
	for {
		if id, ok := monitors.ProcessMonitor(pid); ok {
			if id == target.ID {
				return id
			}
			if !moved {
				log.Printf("[bench] player opened on %s instead of %s -- moving it", id, target.ID)
				if err := monitors.MoveProcessWindows(pid, target); err != nil {
					log.Printf("[bench] moving the player to %s: %v", target.ID, err)
					return id
				}
				moved = true
				continue
			}
			if time.Now().After(deadline) {
				log.Printf("[bench] player still on %s, not %s", id, target.ID)
				return id
			}
		} else if time.Now().After(deadline) {
			log.Printf("[bench] can't see the player's window to check its monitor")
			return ""
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func playerArgs(player, content string) (args []string, label string, err error) {
	switch player {
	case "ffplay":
		args = []string{"-hide_banner", "-loglevel", "error", "-fs", "-an", "-alwaysontop", "-window_title", windowTitle}
		if content != "" {
			return append(args, "-loop", "0", content), filepath.Base(content), nil
		}
		return append(args, "-f", "lavfi", "-i", "testsrc2=size=1920x1080:rate=60"), "testsrc2 1080p60", nil
	case "mpv":
		if content == "" {
			return nil, "", errors.New("benchmark content unavailable and mpv can't generate a test pattern (install ffmpeg)")
		}
		return []string{"--fs", "--ontop", "--no-audio", "--loop-file=inf", "--no-osc", "--title=" + windowTitle, content}, filepath.Base(content), nil
	case "vlc":
		if content == "" {
			return nil, "", errors.New("benchmark content unavailable and VLC can't generate a test pattern (install ffmpeg)")
		}
		return []string{"--fullscreen", "--video-on-top", "--no-audio", "--loop", "--no-video-title-show", "--no-osd", content}, filepath.Base(content), nil
	}
	return nil, "", fmt.Errorf("unsupported player %q", player)
}

// findPlayer returns the first usable player: VLC first (preferred), then
// ffplay (the only one that can also fall back to a generated testsrc2
// pattern when the content download fails), then mpv.
func findPlayer() (name, path string) {
	for _, name := range []string{"vlc", "ffplay", "mpv"} {
		if path := lookPlayer(name); path != "" {
			return name, path
		}
	}
	return "", ""
}

func lookPlayer(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	// launchd and Windows services start with a minimal PATH.
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{"/opt/homebrew/bin/" + name, "/usr/local/bin/" + name}
		if name == "vlc" {
			candidates = append(candidates, "/Applications/VLC.app/Contents/MacOS/VLC")
		}
	case "windows":
		exe := name + ".exe"
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			if m, _ := filepath.Glob(filepath.Join(la, "Microsoft", "WinGet", "Packages", "*FFmpeg*", "*", "bin", exe)); len(m) > 0 {
				candidates = append(candidates, m...)
			}
			candidates = append(candidates, filepath.Join(la, "Microsoft", "WinGet", "Links", exe))
		}
		// The service runs as LocalSystem, whose LOCALAPPDATA is not the
		// user's -- winget installs land in the user's profile.
		if m, _ := filepath.Glob(filepath.Join(`C:\Users`, "*", "AppData", "Local", "Microsoft", "WinGet", "Packages", "*FFmpeg*", "*", "bin", exe)); len(m) > 0 {
			candidates = append(candidates, m...)
		}
		for _, pf := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if pf == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(pf, "ffmpeg", "bin", exe),
				filepath.Join(pf, "mpv", exe),
				filepath.Join(pf, "VideoLAN", "VLC", exe))
		}
		candidates = append(candidates, filepath.Join(`C:\ffmpeg\bin`, exe))
	default:
		candidates = []string{"/usr/local/bin/" + name, "/snap/bin/" + name}
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}
