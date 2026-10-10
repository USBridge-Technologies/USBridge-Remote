package devicecert

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// server starts an httptest.Server answering both routes this package
// calls, and points backendBaseURL at it for the duration of the test --
// mirrors internal/entitlement/download_test.go's downloadInfoServer.
func server(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	prev := TestSetBackendBaseURL(srv.URL)
	t.Cleanup(func() { TestSetBackendBaseURL(prev) })
}

func TestRegisterIP_SendsHwIDAndIPReturnsHostname(t *testing.T) {
	var gotBody map[string]string
	server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/device/dns" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"hostname": "abc123.device.usbridge.io"})
	})

	hostname, err := RegisterIP(context.Background(), "hw-1", "192.168.1.50")
	if err != nil {
		t.Fatalf("RegisterIP: %v", err)
	}
	if hostname != "abc123.device.usbridge.io" {
		t.Errorf("hostname = %q, want abc123.device.usbridge.io", hostname)
	}
	if gotBody["hw_id"] != "hw-1" || gotBody["ip"] != "192.168.1.50" {
		t.Errorf("request body = %+v, want hw_id=hw-1 ip=192.168.1.50", gotBody)
	}
}

func TestRegisterIP_PropagatesBackendError(t *testing.T) {
	server(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"refusing to publish non-private address"}`))
	})

	if _, err := RegisterIP(context.Background(), "hw-1", "8.8.8.8"); err == nil {
		t.Fatal("expected an error when the backend rejects the IP, got nil")
	}
}

func TestRequestCert_ReturnsCertAndMetadataNoKey(t *testing.T) {
	var gotBody struct {
		HwID string `json:"hw_id"`
		CSR  string `json:"csr"`
	}
	server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/device/cert" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Cert{
			CertPEM:  "CERTPEM",
			Hostname: "abc123.device.usbridge.io",
			NotAfter: "2027-01-01T00:00:00Z",
		})
	})

	cert, err := RequestCert(context.Background(), "hw-1", []byte("fake-csr-der"), "test-account-token")
	if err != nil {
		t.Fatalf("RequestCert: %v", err)
	}
	if cert.CertPEM != "CERTPEM" || cert.Hostname != "abc123.device.usbridge.io" {
		t.Errorf("cert = %+v, unexpected shape", cert)
	}
	if gotBody.HwID != "hw-1" {
		t.Errorf("request hw_id = %q, want hw-1", gotBody.HwID)
	}
	if decoded, err := base64.StdEncoding.DecodeString(gotBody.CSR); err != nil || string(decoded) != "fake-csr-der" {
		t.Errorf("request csr = %q, want base64 of fake-csr-der (decode err: %v)", gotBody.CSR, err)
	}
}

func TestRequestCert_PropagatesBackendError(t *testing.T) {
	server(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"device cert unavailable: boom"}`))
	})

	if _, err := RequestCert(context.Background(), "hw-1", []byte("csr"), "test-account-token"); err == nil {
		t.Fatal("expected an error on HTTP 503, got nil")
	}
}

func TestRequestCert_RateLimitedResponseReturnsErrRateLimited(t *testing.T) {
	server(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"rate_limited","detail":"too many certificates"}`))
	})

	_, err := RequestCert(context.Background(), "hw-1", []byte("csr"), "test-account-token")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("RequestCert error = %v, want errors.Is(err, ErrRateLimited)", err)
	}
}

func TestRequestCertPendingIsErrPending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"pending","hostname":"x.device.usbridge.io"}`))
	}))
	defer srv.Close()
	prev := TestSetBackendBaseURL(srv.URL)
	defer TestSetBackendBaseURL(prev)

	_, err := RequestCert(context.Background(), "hw", []byte("csr"), "test-account-token")
	if !errors.Is(err, ErrPending) {
		t.Fatalf("want ErrPending, got %v", err)
	}
}

func TestRequestCert_SendsAccountTokenAndMapsAccountRequired(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"account_required"}`))
	}))
	defer srv.Close()
	defer TestSetBackendBaseURL(TestSetBackendBaseURL(srv.URL))

	_, err := RequestCert(context.Background(), "hw-1", []byte("csr"), "tok")
	if gotAuth != "Bearer tok" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if !errors.Is(err, ErrAccountRequired) {
		t.Fatalf("err = %v, want ErrAccountRequired", err)
	}
	gotAuth = "unset"
	if _, err := RequestCert(context.Background(), "hw-1", []byte("csr"), ""); !errors.Is(err, ErrAccountRequired) || gotAuth != "unset" {
		t.Fatalf("signed out: err = %v, request sent = %v", err, gotAuth != "unset")
	}
}
