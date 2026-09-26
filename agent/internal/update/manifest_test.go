package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestReleaseAssetURL(t *testing.T) {
	want := "https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/manifest-agent.json"
	if got := releaseAssetURL("manifest-agent.json"); got != want {
		t.Fatalf("releaseAssetURL() = %q, want %q", got, want)
	}
}

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		remote string
		local  string
		want   bool
	}{
		{remote: "2.1.9", local: "2.1.8", want: true},
		{remote: "v3.0.0", local: " 2.99.99 ", want: true},
		{remote: "2.0", local: "2.0.0", want: false},
		{remote: "2.0.0", local: "2", want: false},
		{remote: "1.9.9", local: "2.0.0", want: false},
		{remote: "1.2.0.1", local: "1.2", want: true},
		{remote: "1.bad.1", local: "1.0.0", want: true},
		{remote: "1.bad", local: "1.0", want: false},
	}

	for _, tt := range tests {
		name := tt.remote + "_vs_" + tt.local
		t.Run(name, func(t *testing.T) {
			if got := isNewerVersion(tt.remote, tt.local); got != tt.want {
				t.Fatalf("isNewerVersion(%q, %q) = %v, want %v", tt.remote, tt.local, got, tt.want)
			}
		})
	}
}

func TestSortedPlatformKeys(t *testing.T) {
	got := sortedPlatformKeys(map[string]PlatformAsset{
		"windows-amd64": {},
		"darwin-arm64":  {},
		"linux-amd64":   {},
	})
	want := []string{"darwin-arm64", "linux-amd64", "windows-amd64"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("sortedPlatformKeys() = %v, want %v", got, want)
	}
}

func TestFetchBytes(t *testing.T) {
	var gotUserAgent string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotUserAgent = req.Header.Get("User-Agent")
		return response(http.StatusOK, "manifest"), nil
	})}

	got, err := fetchBytes(context.Background(), client, "https://example.test/manifest")
	if err != nil {
		t.Fatalf("fetchBytes: %v", err)
	}
	if string(got) != "manifest" {
		t.Fatalf("fetchBytes body = %q", got)
	}
	if gotUserAgent != "USBridge-agent-updater" {
		t.Fatalf("User-Agent = %q", gotUserAgent)
	}
}

func TestFetchBytesErrorsAndLimitsBody(t *testing.T) {
	t.Run("status", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusBadGateway, "nope"), nil
		})}
		if _, err := fetchBytes(context.Background(), client, "https://example.test"); err == nil || !strings.Contains(err.Error(), "unexpected status 502") {
			t.Fatalf("fetchBytes status error = %v", err)
		}
	})

	t.Run("transport", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("offline")
		})}
		if _, err := fetchBytes(context.Background(), client, "https://example.test"); err == nil || !strings.Contains(err.Error(), "offline") {
			t.Fatalf("fetchBytes transport error = %v", err)
		}
	})

	t.Run("invalid URL", func(t *testing.T) {
		if _, err := fetchBytes(context.Background(), http.DefaultClient, "://bad-url"); err == nil {
			t.Fatal("fetchBytes(invalid URL) returned nil error")
		}
	})

	t.Run("body limit", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusOK, strings.Repeat("x", manifestMaxBytes+64)), nil
		})}
		got, err := fetchBytes(context.Background(), client, "https://example.test")
		if err != nil {
			t.Fatalf("fetchBytes: %v", err)
		}
		if len(got) != manifestMaxBytes {
			t.Fatalf("body length = %d, want limit %d", len(got), manifestMaxBytes)
		}
	})
}

func TestFetchManifestVerifiesSignatureAndApp(t *testing.T) {
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	originalPublicKey := publicKey
	publicKey = pub
	t.Cleanup(func() { publicKey = originalPublicKey })

	manifest := Manifest{
		App:     appName,
		Version: "9.9.9",
		Platforms: map[string]PlatformAsset{
			"linux-amd64": {Asset: "agent.AppImage", SHA256: "abc123"},
		},
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(private, body))

	clientFor := func(manifestBody, signatureBody string) *http.Client {
		return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.HasSuffix(req.URL.Path, ".sig") {
				return response(http.StatusOK, signatureBody), nil
			}
			return response(http.StatusOK, manifestBody), nil
		})}
	}

	got, err := fetchManifest(context.Background(), clientFor(string(body), " \n"+signature+"\n"))
	if err != nil {
		t.Fatalf("fetchManifest: %v", err)
	}
	if got.Version != manifest.Version || got.Platforms["linux-amd64"].Asset != "agent.AppImage" {
		t.Fatalf("fetchManifest() = %#v", got)
	}

	t.Run("bad base64", func(t *testing.T) {
		if _, err := fetchManifest(context.Background(), clientFor(string(body), "%%%")); err == nil || !strings.Contains(err.Error(), "decode manifest signature") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("bad signature", func(t *testing.T) {
		badSignature := base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
		if _, err := fetchManifest(context.Background(), clientFor(string(body), badSignature)); err == nil || !strings.Contains(err.Error(), "signature verification failed") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		invalid := []byte("not-json")
		signed := base64.StdEncoding.EncodeToString(ed25519.Sign(private, invalid))
		if _, err := fetchManifest(context.Background(), clientFor(string(invalid), signed)); err == nil || !strings.Contains(err.Error(), "parse manifest") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("wrong app", func(t *testing.T) {
		wrongBody := []byte(`{"app":"client","version":"9.9.9","platforms":{}}`)
		signed := base64.StdEncoding.EncodeToString(ed25519.Sign(private, wrongBody))
		if _, err := fetchManifest(context.Background(), clientFor(string(wrongBody), signed)); err == nil || !strings.Contains(err.Error(), `manifest is for app "client"`) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("manifest fetch failure", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusNotFound, "missing"), nil
		})}
		if _, err := fetchManifest(context.Background(), client); err == nil || !strings.Contains(err.Error(), "fetch manifest") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("signature fetch failure", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.HasSuffix(req.URL.Path, ".sig") {
				return response(http.StatusNotFound, "missing"), nil
			}
			return response(http.StatusOK, string(body)), nil
		})}
		if _, err := fetchManifest(context.Background(), client); err == nil || !strings.Contains(err.Error(), "fetch manifest signature") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestMustDecodePublicKey(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize))
	if got := mustDecodePublicKey(encoded); len(got) != ed25519.PublicKeySize {
		t.Fatalf("decoded key length = %d", len(got))
	}

	for _, input := range []string{"%%%", base64.StdEncoding.EncodeToString([]byte("short"))} {
		t.Run(input, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("mustDecodePublicKey did not panic")
				}
			}()
			mustDecodePublicKey(input)
		})
	}
}
