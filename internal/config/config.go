package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type TailscaleMode int

const (
	TailscaleModeUserspace TailscaleMode = iota
	TailscaleModeSystem
)

func (m TailscaleMode) MarshalYAML() (interface{}, error) {
	switch m {
	case TailscaleModeUserspace:
		return "userspace", nil
	case TailscaleModeSystem:
		return "system", nil
	default:
		return "system", nil
	}
}

func (m *TailscaleMode) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err == nil {
		switch strings.ToLower(s) {
		case "userspace":
			*m = TailscaleModeUserspace
			return nil
		case "system":
			*m = TailscaleModeSystem
			return nil
		}
	}

	var i int
	if err := value.Decode(&i); err == nil {
		*m = TailscaleMode(i)
		return nil
	}

	*m = TailscaleModeSystem
	return nil
}

type Config struct {
	AppName          string        `yaml:"app_name"`
	ListenHost       string        `yaml:"listen_host"`
	HTTPPort         int           `yaml:"http_port"`
	TailscaleEnabled bool          `yaml:"tailscale_enabled"`
	TailscaleMode    TailscaleMode `yaml:"tailscale_mode"`
	FRPBindHost      string        `yaml:"frp_bind_host"`
	FRPBindPort      int           `yaml:"frp_bind_port"`
	FRPToken         string        `yaml:"frp_token"`
	FRPTLSCertFile   string        `yaml:"frp_tls_cert_file"`
	FRPTLSKeyFile    string        `yaml:"frp_tls_key_file"`
	VideoUDPPort     int           `yaml:"video_udp_port"`
	FFmpegPath       string        `yaml:"ffmpeg_path"`
	VideoFPS         int           `yaml:"video_fps"`
	VideoWidth       int           `yaml:"video_width"`
	VideoHeight      int           `yaml:"video_height"`
	VideoBitrate     string        `yaml:"video_bitrate"`
	VideoCodec       string        `yaml:"video_codec"`
	VideoCapture     string        `yaml:"video_capture"`
	NBDMountCommand  string        `yaml:"nbd_mount_command"`
	StateDir         string        `yaml:"state_dir"`
}

func Default() Config {
	stateDir := defaultStateDir()
	videoCapture := "dxgi"
	videoCodec := "auto"
	if runtime.GOOS == "darwin" {
		videoCapture = "avfoundation"
	} else if runtime.GOOS == "linux" {
		videoCapture = "auto"
	}
	return Config{
		AppName:          "USBridge Agent",
		ListenHost:       "127.0.0.1",
		HTTPPort:         8080,
		TailscaleEnabled: true,
		TailscaleMode:    TailscaleModeSystem,
		FRPBindHost:      "0.0.0.0",
		FRPBindPort:      8443,
		FRPToken:         "usbridge-secret-token",
		FRPTLSCertFile:   filepath.Join(stateDir, "frp.crt"),
		FRPTLSKeyFile:    filepath.Join(stateDir, "frp.key"),
		VideoUDPPort:     55000,
		FFmpegPath:       "ffmpeg",
		VideoFPS:         30,
		VideoWidth:       1280,
		VideoHeight:      720,
		VideoBitrate:     "4M",
		VideoCodec:       videoCodec,
		VideoCapture:     videoCapture,
		NBDMountCommand:  "",
		StateDir:         stateDir,
	}
}

func (c Config) EffectiveListenHost() string {
	host := strings.TrimSpace(c.ListenHost)
	if host == "" {
		return "127.0.0.1"
	}
	return host
}

func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return finalize(cfg, "")
		}
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return finalize(cfg, path)
}

func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func GenerateSecureToken() (string, error) {
	randomBytes := make([]byte, 24)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(randomBytes)
	if len(token) != 32 {
		return "", errors.New("unexpected token length")
	}
	return token, nil
}

func (c Config) EnsureState() error {
	if err := os.MkdirAll(c.StateDir, 0o755); err != nil {
		return err
	}
	return ensureSelfSignedPair(c.FRPTLSCertFile, c.FRPTLSKeyFile)
}

func ensureSelfSignedPair(certPath, keyPath string) error {
	if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			return nil
		}
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	tpl := &x509.Certificate{
		SerialNumber:          newSerial(),
		Subject:               pkix.Name{CommonName: "usbridge-agent"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost", "usbridge-agent"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tpl, tpl, pub, priv)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return err
	}
	return os.WriteFile(keyPath, keyPEM, 0o600)
}

func newSerial() *big.Int {
	n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	return n
}

func defaultStateDir() string {
	if base, err := os.UserConfigDir(); err == nil && strings.TrimSpace(base) != "" {
		return filepath.Join(base, "usbridge-agent")
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, "Library", "Application Support", "usbridge-agent")
		}
		return filepath.Join(home, ".config", "usbridge-agent")
	}
	return filepath.Join(".", "var")
}

func resolvePaths(cfg Config, cfgPath string) Config {
	if strings.TrimSpace(cfgPath) == "" {
		return cfg
	}
	defaults := Default()
	configDir := filepath.Dir(cfgPath)

	if strings.TrimSpace(cfg.StateDir) == "" || cfg.StateDir == "./var" {
		cfg.StateDir = defaults.StateDir
	} else if !filepath.IsAbs(cfg.StateDir) {
		cfg.StateDir = filepath.Clean(filepath.Join(configDir, cfg.StateDir))
	}

	if strings.TrimSpace(cfg.FRPTLSCertFile) == "" || cfg.FRPTLSCertFile == "./var/frp.crt" {
		cfg.FRPTLSCertFile = filepath.Join(cfg.StateDir, "frp.crt")
	} else if !filepath.IsAbs(cfg.FRPTLSCertFile) {
		cfg.FRPTLSCertFile = filepath.Clean(filepath.Join(configDir, cfg.FRPTLSCertFile))
	}

	if strings.TrimSpace(cfg.FRPTLSKeyFile) == "" || cfg.FRPTLSKeyFile == "./var/frp.key" {
		cfg.FRPTLSKeyFile = filepath.Join(cfg.StateDir, "frp.key")
	} else if !filepath.IsAbs(cfg.FRPTLSKeyFile) {
		cfg.FRPTLSKeyFile = filepath.Clean(filepath.Join(configDir, cfg.FRPTLSKeyFile))
	}

	return cfg
}

func finalize(cfg Config, cfgPath string) (Config, error) {
	cfg = resolvePaths(cfg, cfgPath)
	cfg.FFmpegPath = resolveFFmpegPath(cfg.FFmpegPath)
	return cfg, nil
}

func resolveFFmpegPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "ffmpeg"
	}

	if filepath.IsAbs(value) {
		return value
	}

	if strings.ContainsRune(value, filepath.Separator) {
		return filepath.Clean(value)
	}

	if path, err := exec.LookPath(value); err == nil {
		return path
	}

	if runtime.GOOS == "darwin" {
		for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
			candidate := filepath.Join(dir, value)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				return candidate
			}
		}
	}

	return value
}
