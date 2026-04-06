package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	AppName         string `yaml:"app_name"`
	ListenHost      string `yaml:"listen_host"`
	HTTPPort        int    `yaml:"http_port"`
	FRPBindHost     string `yaml:"frp_bind_host"`
	FRPBindPort     int    `yaml:"frp_bind_port"`
	FRPToken        string `yaml:"frp_token"`
	FRPTLSCertFile  string `yaml:"frp_tls_cert_file"`
	FRPTLSKeyFile   string `yaml:"frp_tls_key_file"`
	VideoUDPPort    int    `yaml:"video_udp_port"`
	FFmpegPath      string `yaml:"ffmpeg_path"`
	VideoFPS        int    `yaml:"video_fps"`
	VideoWidth      int    `yaml:"video_width"`
	VideoHeight     int    `yaml:"video_height"`
	VideoBitrate    string `yaml:"video_bitrate"`
	VideoCodec      string `yaml:"video_codec"`
	VideoCapture    string `yaml:"video_capture"`
	NBDMountCommand string `yaml:"nbd_mount_command"`
	StateDir        string `yaml:"state_dir"`
}

func Default() Config {
	stateDir := filepath.Join(".", "var")
	return Config{
		AppName:         "USBridge Agent",
		ListenHost:      "127.0.0.1",
		HTTPPort:        8080,
		FRPBindHost:     "0.0.0.0",
		FRPBindPort:     443,
		FRPToken:        "usbridge-secret-token",
		FRPTLSCertFile:  filepath.Join(stateDir, "frp.crt"),
		FRPTLSKeyFile:   filepath.Join(stateDir, "frp.key"),
		VideoUDPPort:    55000,
		FFmpegPath:      "ffmpeg",
		VideoFPS:        30,
		VideoWidth:      1280,
		VideoHeight:     720,
		VideoBitrate:    "4M",
		VideoCodec:      "libx264",
		VideoCapture:    "dxgi",
		NBDMountCommand: "",
		StateDir:        stateDir,
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
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

func (c Config) EnsureState() error {
	if err := os.MkdirAll(c.StateDir, 0o755); err != nil {
		return err
	}
	return ensureSelfSignedPair(c.FRPTLSCertFile, c.FRPTLSKeyFile)
}

func ensureSelfSignedPair(certPath, keyPath string) error {
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
