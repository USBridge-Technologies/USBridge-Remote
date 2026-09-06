package account

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStartLogin_ReturnsCodeAndVerificationURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/account/login/start" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":             "abc123",
			"verification_url": "https://billing.usbridge.io/auth/device?code=abc123",
			"expires_in":       600,
		})
	}))
	defer srv.Close()
	prev := TestSetBackendBaseURL(srv.URL)
	defer TestSetBackendBaseURL(prev)

	start, err := StartLogin(context.Background())
	if err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	if start.Code != "abc123" || start.ExpiresIn != 600 {
		t.Fatalf("unexpected LoginStart: %+v", start)
	}
}

func TestPoll_PendingThenComplete(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":        "complete",
			"email":         "some.customer@gmail.com",
			"account_token": "tok123",
			"expires_in":    2592000,
		})
	}))
	defer srv.Close()
	prev := TestSetBackendBaseURL(srv.URL)
	defer TestSetBackendBaseURL(prev)

	first, err := Poll(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("Poll (1st): %v", err)
	}
	if first.Status != "pending" {
		t.Fatalf("first poll status = %q, want pending", first.Status)
	}

	second, err := Poll(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("Poll (2nd): %v", err)
	}
	if second.Status != "complete" || second.Email != "some.customer@gmail.com" || second.AccountToken != "tok123" {
		t.Fatalf("unexpected complete result: %+v", second)
	}
}

func TestListLicenses_SendsBearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Query().Get("kind") != "desktop" {
			t.Fatalf("expected kind=desktop, got %q", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"licenses": []License{{Identifier: "HW-1", Status: "licensed", Tier: "desktop-license"}},
		})
	}))
	defer srv.Close()
	prev := TestSetBackendBaseURL(srv.URL)
	defer TestSetBackendBaseURL(prev)

	licenses, err := ListLicenses(context.Background(), "tok123")
	if err != nil {
		t.Fatalf("ListLicenses: %v", err)
	}
	if gotAuth != "Bearer tok123" {
		t.Fatalf("Authorization header = %q, want %q", gotAuth, "Bearer tok123")
	}
	if len(licenses) != 1 || licenses[0].Identifier != "HW-1" {
		t.Fatalf("unexpected licenses: %+v", licenses)
	}
}

func TestRebind_PostsExpectedBody(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/manage/api/rebind" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()
	prev := TestSetBackendBaseURL(srv.URL)
	defer TestSetBackendBaseURL(prev)

	if err := Rebind(context.Background(), "tok123", "OLD-HW", "NEW-HW"); err != nil {
		t.Fatalf("Rebind: %v", err)
	}
	if gotBody["identifier"] != "OLD-HW" || gotBody["new_hw_id"] != "NEW-HW" {
		t.Fatalf("unexpected request body: %+v", gotBody)
	}
}
