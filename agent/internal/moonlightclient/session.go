package moonlightclient

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"time"

	"github.com/sirupsen/logrus"
)

// Config describes the local Sunshine instance to drive and the stream
// parameters to request. All network fields are loopback-only by design --
// see the package doc comment for why this client never talks to a remote
// host.
type Config struct {
	// Host is almost always "127.0.0.1" -- this package only exists to
	// drive a Sunshine instance running on the same machine as the agent.
	Host string
	// HTTPSPort is Sunshine's NvHTTP HTTPS port (sunshine.conf's "port"
	// minus 5; default 47984). If 0, defaults to 47984.
	HTTPSPort int
	// HTTPPort is Sunshine's NvHTTP HTTP port (sunshine.conf's "port";
	// default 47989). If 0, defaults to 47989.
	HTTPPort int

	// StateDir, if set, is where this package persists its client identity
	// (RSA key + cert) so it stays paired with Sunshine across agent
	// restarts instead of re-pairing (a fresh handshake + SubmitPIN admin
	// call) every single Start(). Recommended: a subdirectory of the
	// agent's own persistent state directory.
	StateDir string

	// AppID is the Sunshine app ID to launch (see /applist). If 0, Start
	// launches whatever /applist returns first (Sunshine's default
	// installs always have at least "Desktop" as the first configured app).
	AppID int

	Width, Height, FPS int
	BitrateKbps        int

	// SubmitPIN submits a Moonlight pairing PIN to Sunshine on the caller's
	// behalf -- wired by agent/internal/app to streamhost's
	// Backend.SubmitPIN (the same admin-API call used for real remote
	// clients' PINs, see agent/internal/streamhost/sunshine_pairing.go).
	// Deliberately a callback rather than an import of streamhost directly:
	// this package has no reason to know how Sunshine was launched or
	// where its admin credentials live, and taking that dependency would
	// risk an import cycle if streamhost ever needs anything from here.
	SubmitPIN func(pin string) error
}

func (c Config) withDefaults() Config {
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
	if c.HTTPSPort == 0 {
		c.HTTPSPort = 47984
	}
	if c.HTTPPort == 0 {
		c.HTTPPort = 47989
	}
	if c.Width == 0 {
		c.Width = 1920
	}
	if c.Height == 0 {
		c.Height = 1080
	}
	if c.FPS == 0 {
		c.FPS = 60
	}
	if c.BitrateKbps == 0 {
		c.BitrateKbps = 20000
	}
	return c
}

// baseUDPPort mirrors streamhost.sunshineBackend.Ports' offset scheme,
// keyed off the NvHTTP HTTP ("base") port: video=+9, control=+10,
// audio=+11. Only used as a fallback when an RTSP SETUP response's
// Transport header doesn't parse (see rtsp.go's parseServerPort) --
// matches RtspConnection.c's own hardcoded fallbacks (47998/47999/48000
// off the standard 47989 base).
func (c Config) baseUDPPort() int { return c.HTTPPort + 9 }

// Session is a paired, launched, actively-streaming loopback connection to
// the agent's own Sunshine instance. Once Start returns successfully,
// Sunshine is sending H.264 video RTP and Opus audio RTP packets to
// 127.0.0.1 on the ports VideoRTPAddr/AudioRTPAddr report, and will keep
// doing so as long as this Session's keepalive loop keeps running (i.e.
// until Stop is called or the process exits).
type Session struct {
	cfg Config
	nv  *nvhttpClient

	rtsp    *rtspSession
	control *controlChannel

	videoAddr string
	audioAddr string
}

// Start pairs with Sunshine (if not already paired -- see Config.StateDir),
// launches a session, performs the RTSP handshake, and starts the ENet
// control channel + keepalive loop. Returns once Sunshine has acknowledged
// PLAY, at which point it is actively streaming.
func Start(ctx context.Context, cfg Config) (*Session, error) {
	cfg = cfg.withDefaults()
	if cfg.SubmitPIN == nil {
		return nil, fmt.Errorf("moonlightclient: Config.SubmitPIN is required")
	}

	id, err := loadOrGenerateIdentity(cfg.StateDir)
	if err != nil {
		return nil, fmt.Errorf("load/generate identity: %w", err)
	}

	nv := newNVHTTPClient(cfg.Host, cfg.HTTPPort, cfg.HTTPSPort, id)

	if err := ensurePaired(ctx, nv, cfg); err != nil {
		return nil, fmt.Errorf("pairing: %w", err)
	}

	appID := cfg.AppID
	if appID == 0 {
		apps, err := nv.getAppList()
		if err != nil {
			return nil, fmt.Errorf("fetch /applist: %w", err)
		}
		if len(apps) == 0 {
			return nil, fmt.Errorf("Sunshine has no configured apps to launch")
		}
		appID = apps[0].ID
		logrus.Infof("🎮 [moonlightclient] no AppID configured, launching first available app %q (id=%d)", apps[0].Title, appID)
	}
	cfgWithApp := cfg
	cfgWithApp.AppID = appID

	rikey := make([]byte, 16)
	if _, err := rand.Read(rikey); err != nil {
		return nil, fmt.Errorf("generate rikey: %w", err)
	}

	launchRes, err := nv.launch(cfgWithApp, rikey)
	if err != nil {
		return nil, fmt.Errorf("/launch: %w", err)
	}
	logrus.Infof("🎮 [moonlightclient] launched app %d, RTSP session at %s", appID, launchRes.rtspHostPort)

	rtsp, err := performRTSPHandshake(launchRes.rtspHostPort, cfgWithApp)
	if err != nil {
		return nil, fmt.Errorf("RTSP handshake: %w", err)
	}
	logrus.Infof("🎮 [moonlightclient] RTSP handshake complete: video=%d audio=%d control=%d session=%s",
		rtsp.videoPort, rtsp.audioPort, rtsp.controlPort, rtsp.sessionID)

	controlAddr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", rtsp.controlPort))
	peer, err := enetConnect(controlAddr, rtsp.controlConnectData, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("ENet control channel connect to %s: %w", controlAddr, err)
	}

	control, err := newControlChannel(peer, rikey)
	if err != nil {
		_ = peer.close()
		return nil, fmt.Errorf("build control channel: %w", err)
	}
	if err := control.start(); err != nil {
		control.stop()
		return nil, fmt.Errorf("start control channel: %w", err)
	}
	logrus.Infof("🎮 [moonlightclient] control channel started, Sunshine should now be streaming")

	// Sunshine won't send a single RTP packet until it has been shown, via
	// a ping packet, where to send them (see ping.go's doc comment) --
	// this is the step that actually makes VideoRTPAddr/AudioRTPAddr real.
	videoLocalPort, err := establishRTPAddr(cfg.Host, rtsp.videoPort, rtsp.videoPingPayload)
	if err != nil {
		control.stop()
		return nil, fmt.Errorf("establish video RTP address: %w", err)
	}
	audioLocalPort, err := establishRTPAddr(cfg.Host, rtsp.audioPort, rtsp.audioPingPayload)
	if err != nil {
		control.stop()
		return nil, fmt.Errorf("establish audio RTP address: %w", err)
	}
	logrus.Infof("🎮 [moonlightclient] pinged Sunshine's video/audio ports, RTP should now target 127.0.0.1:%d (video) and 127.0.0.1:%d (audio)",
		videoLocalPort, audioLocalPort)

	return &Session{
		cfg:       cfg,
		nv:        nv,
		rtsp:      rtsp,
		control:   control,
		videoAddr: net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", videoLocalPort)),
		audioAddr: net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", audioLocalPort)),
	}, nil
}

// ensurePaired checks Sunshine's current pair status and, if not already
// paired, runs the full NvHTTP pairing handshake with a freshly generated
// PIN while concurrently submitting that same PIN through Config.SubmitPIN
// (the admin-API equivalent of a human typing it into Sunshine's web UI) --
// see the package doc comment and Config.SubmitPIN's doc comment for why
// this is safe and doesn't need a human anywhere in the loop.
func ensurePaired(ctx context.Context, nv *nvhttpClient, cfg Config) error {
	info, err := nv.getServerInfo(false)
	if err == nil && info.PairStatus == 1 {
		return nil // already paired (e.g. StateDir preserved our identity across a restart)
	}

	pin := generatePIN()
	logrus.Infof("🎮 [moonlightclient] not paired with Sunshine yet, pairing with generated PIN %s", pin)

	pairErrCh := make(chan error, 1)
	go func() {
		pairErrCh <- nv.pair(ctx, pin)
	}()

	// SubmitPIN races the pairing goroutine's first request (getservercert),
	// which is the stage Sunshine holds open server-side until a PIN
	// arrives (see nvhttpClient.pairingClient's doc comment). A single
	// early "success" from Sunshine's admin /api/pin does NOT prove the PIN
	// actually landed on a pending pairing session -- confirmed live: it
	// can return 200 even when our own /pair request hasn't reached
	// Sunshine yet (there's nothing pending to apply the PIN to), in which
	// case that PIN is silently discarded and /pair keeps blocking forever
	// with no further chance to retry. So this keeps resubmitting the same
	// PIN on an interval for as long as pairing is still outstanding,
	// instead of stopping at the first nil error -- resubmitting a PIN that
	// already landed is a harmless no-op on Sunshine's side.
	submitTicker := time.NewTicker(500 * time.Millisecond)
	defer submitTicker.Stop()
	stopSubmit := make(chan struct{})
	defer close(stopSubmit)
	go func() {
		if err := cfg.SubmitPIN(pin); err != nil {
			logrus.Warnf("🎮 [moonlightclient] SubmitPIN attempt failed (will keep retrying): %v", err)
		}
		for {
			select {
			case <-submitTicker.C:
				if err := cfg.SubmitPIN(pin); err != nil {
					logrus.Warnf("🎮 [moonlightclient] SubmitPIN attempt failed (will keep retrying): %v", err)
				}
			case <-stopSubmit:
				return
			}
		}
	}()

	select {
	case err := <-pairErrCh:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
		return fmt.Errorf("pairing handshake timed out")
	}

	logrus.Infof("🎮 [moonlightclient] paired with Sunshine successfully")
	return nil
}

// VideoRTPAddr returns the 127.0.0.1:port address Sunshine is sending video
// RTP packets to. A caller (agent/internal/webrtcbridge) should
// net.ListenUDP on this exact address to receive them -- promptly: this is
// the local port establishRTPAddr (ping.go) pinged Sunshine from and then
// released, so Sunshine already believes RTP should go here, but nothing
// is listening on it until the caller binds. See ping.go's doc comment for
// why that hand-off is safe on loopback (no NAT/firewall state to expire
// in the gap) even though it wouldn't be over a real network path.
func (s *Session) VideoRTPAddr() string { return s.videoAddr }

// AudioRTPAddr returns the 127.0.0.1:port address Sunshine is sending Opus
// audio RTP packets to. Same hand-off caveat as VideoRTPAddr.
func (s *Session) AudioRTPAddr() string { return s.audioAddr }

// RequestIDRFrame asks Sunshine for a fresh keyframe on the video stream
// (e.g. when a new WebRTC viewer joins and needs to start decoding from a
// keyframe rather than waiting for Sunshine's own periodic IDR interval).
func (s *Session) RequestIDRFrame() {
	s.control.requestIDRFrame()
}

// Stop tears down the session: stops the control channel's keepalive loop,
// sends ENet DISCONNECT, and asks Sunshine to end the streaming session via
// /cancel so it doesn't sit there waiting for RTP acks that will never come.
func (s *Session) Stop() error {
	if s.control != nil {
		s.control.stop()
	}
	if s.nv != nil {
		return s.nv.cancel()
	}
	return nil
}
