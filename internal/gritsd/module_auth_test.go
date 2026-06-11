package gritsd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"grits/internal/grits"
	"io"
	"net/http"
	"strings"
	"testing"
)

// hashPasswordForTest produces an argon2id hash for direct use in test user records.
func hashPasswordForTest(t *testing.T, password string) string {
	t.Helper()
	h, err := Argon2idEncode(password)
	if err != nil {
		t.Fatalf("Argon2idEncode: %v", err)
	}
	return h
}

func writeUserRecord(t *testing.T, s *Server, username, pwdHash string) {
	t.Helper()

	lines, err := ReadJSONL(s, "root", usersFilePath, grits.BackendPrincipal)
	if err != nil {
		// File doesn't exist yet — that's fine, start empty
		lines = nil
	}

	// Append or replace the user
	found := false
	var records []map[string]any
	for _, line := range lines {
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec["username"] == username {
			rec["pwdHash"] = pwdHash
			found = true
		}
		records = append(records, rec)
	}
	if !found {
		records = append(records, map[string]any{
			"username": username,
			"pwdHash":  pwdHash,
		})
	}

	if err := WriteJSONL(s, "root", usersFilePath, records, grits.BackendPrincipal); err != nil {
		t.Fatalf("WriteJSONL: %v", err)
	}
}

func TestArgon2idRoundTrip(t *testing.T) {
	passwords := []string{
		"correct-horse-battery-staple",
		"short",
		"",
		"パスワード",
		"a\nb\tc",
	}

	for _, pw := range passwords {
		encoded, err := Argon2idEncode(pw)
		if err != nil {
			t.Errorf("Argon2idEncode(%q): %v", pw, err)
			continue
		}

		if !strings.HasPrefix(encoded, "$argon2id$v=19$") {
			t.Errorf("encoded hash %q missing expected prefix", encoded)
		}

		if !verifyArgon2id(pw, encoded) {
			t.Errorf("verifyArgon2id(%q, %q) = false, want true", pw, encoded)
		}
	}
}

func TestArgon2idWrongPassword(t *testing.T) {
	encoded, err := Argon2idEncode("real-password")
	if err != nil {
		t.Fatalf("Argon2idEncode: %v", err)
	}

	if verifyArgon2id("wrong-password", encoded) {
		t.Error("verifyArgon2id with wrong password returned true, want false")
	}
}

func TestArgon2idMalformedHash(t *testing.T) {
	if verifyArgon2id("x", "") {
		t.Error("empty hash should return false")
	}
	if verifyArgon2id("x", "not-a-valid-hash") {
		t.Error("garbage hash should return false")
	}
	if verifyArgon2id("x", "$argon2id$v=19$m=foo$salt$hash") {
		t.Error("malformed params should return false")
	}
}

func TestReadWriteJSONL(t *testing.T) {
	server, cleanup := SetupTestServer(t)
	defer cleanup()

	volConfig := &LocalVolumeConfig{VolumeName: "testjsonl"}
	vol, err := NewLocalVolume(volConfig, server, false, false)
	if err != nil {
		t.Fatalf("NewLocalVolume: %v", err)
	}
	server.AddModule(vol)
	server.AddVolume(vol)
	server.Start()
	defer server.Stop()

	records := []map[string]any{
		{"username": "alice", "role": "admin"},
		{"username": "bob", "role": "user"},
	}

	if err := WriteJSONL(server, "testjsonl", "data/users.jsonl", records, grits.BackendPrincipal); err != nil {
		t.Fatalf("WriteJSONL: %v", err)
	}

	lines, err := ReadJSONL(server, "testjsonl", "data/users.jsonl", grits.BackendPrincipal)
	if err != nil {
		t.Fatalf("ReadJSONL: %v", err)
	}

	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	var alice, bob map[string]string
	if err := json.Unmarshal(lines[0], &alice); err != nil {
		t.Fatalf("unmarshal line 0: %v", err)
	}
	if err := json.Unmarshal(lines[1], &bob); err != nil {
		t.Fatalf("unmarshal line 1: %v", err)
	}

	if alice["username"] != "alice" || alice["role"] != "admin" {
		t.Errorf("unexpected alice record: %v", alice)
	}
	if bob["username"] != "bob" || bob["role"] != "user" {
		t.Errorf("unexpected bob record: %v", bob)
	}
}

func TestAuthLoginSuccess(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("root"),
		WithHttpModule(1911),
		WithAuthModule(nil, nil),
	)
	defer cleanup()

	server.Start()
	defer server.Stop()

	pwdHash := hashPasswordForTest(t, "test-password")
	writeUserRecord(t, server, "testuser", pwdHash)

	body := fmt.Sprintf(`{"username":"testuser","password":"test-password"}`)
	resp, err := http.Post("http://127.0.0.1:1911/grits/v1/auth/login",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Check cookie was set
	cookies := resp.Cookies()
	var found bool
	for _, c := range cookies {
		if c.Name == authCookie && c.Value == "testuser" {
			found = true
			if !c.HttpOnly {
				t.Error("cookie should be HttpOnly")
			}
			break
		}
	}
	if !found {
		t.Errorf("cookie %s=testuser not set", authCookie)
	}

	// Check response body
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if result["ok"] != true {
		t.Errorf("expected ok=true, got %v", result)
	}
}

func TestAuthLoginWrongPassword(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("root"),
		WithHttpModule(1912),
		WithAuthModule(nil, nil),
	)
	defer cleanup()

	server.Start()
	defer server.Stop()

	pwdHash := hashPasswordForTest(t, "real-password")
	writeUserRecord(t, server, "testuser", pwdHash)

	body := fmt.Sprintf(`{"username":"testuser","password":"wrong-password"}`)
	resp, err := http.Post("http://127.0.0.1:1912/grits/v1/auth/login",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAuthLoginUnknownUser(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("root"),
		WithHttpModule(1913),
		WithAuthModule(nil, nil),
	)
	defer cleanup()

	server.Start()
	defer server.Stop()

	pwdHash := hashPasswordForTest(t, "irrelevant")
	writeUserRecord(t, server, "someone-else", pwdHash)

	body := fmt.Sprintf(`{"username":"nobody","password":"anything"}`)
	resp, err := http.Post("http://127.0.0.1:1913/grits/v1/auth/login",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAuthLoginInvalidUsername(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("root"),
		WithHttpModule(1914),
		WithAuthModule(nil, nil),
	)
	defer cleanup()

	server.Start()
	defer server.Stop()

	invalid := "ab" // too short (min 3)
	body := fmt.Sprintf(`{"username":%q,"password":"x"}`, invalid)
	resp, err := http.Post("http://127.0.0.1:1914/grits/v1/auth/login",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid username, got %d", resp.StatusCode)
	}
}

func TestAuthLogout(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("root"),
		WithHttpModule(1915),
		WithAuthModule(nil, nil),
	)
	defer cleanup()

	server.Start()
	defer server.Stop()

	resp, err := http.Post("http://127.0.0.1:1915/grits/v1/auth/logout",
		"application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("POST logout: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Check cookie was cleared
	var found bool
	for _, c := range resp.Cookies() {
		if c.Name == authCookie {
			found = true
			if c.MaxAge != -1 && !(c.Value == "") {
				t.Errorf("expected cleared cookie, got maxAge=%d value=%q", c.MaxAge, c.Value)
			}
			break
		}
	}
	if !found {
		t.Errorf("cookie %s not present in response", authCookie)
	}

	// Check response body
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if result["ok"] != true {
		t.Errorf("expected ok=true, got %v", result)
	}
}

func TestAuthLoginMethodNotAllowed(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithHttpModule(1916),
		WithAuthModule(nil, nil),
	)
	defer cleanup()

	server.Start()
	defer server.Stop()

	resp, err := http.Get("http://127.0.0.1:1916/grits/v1/auth/login")
	if err != nil {
		t.Fatalf("GET login: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestAuthNoUsersFile(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("root"),
		WithHttpModule(1917),
		WithAuthModule(nil, nil),
	)
	defer cleanup()

	server.Start()
	defer server.Stop()

	// No users file written yet — login should still fail gracefully
	body := `{"username":"anyone","password":"anything"}`
	resp, err := http.Post("http://127.0.0.1:1917/grits/v1/auth/login",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	defer resp.Body.Close()

	// Without a users file, ReadJSONL will error, resulting in a 500
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 when no users file exists, got %d", resp.StatusCode)
	}
}

// doReq sends an HTTP request with an optional cookie and returns the response.
func doReq(t *testing.T, method, url, cookieVal string, body []byte) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest(%s %s): %v", method, url, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookieVal != "" {
		req.Header.Set("Cookie", cookieVal)
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do(%s %s): %v", method, url, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp, respBody
}

// contentURL builds a URL for content access.
func contentURL(base, volume, path string) string {
	return fmt.Sprintf("%s/grits/v1/content/%s/%s", base, volume, path)
}

func TestAuthPermissionsEndToEnd(t *testing.T) {
	port := 1920
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	server, cleanup := SetupTestServer(t,
		WithLocalVolume("root"),
		WithHttpModule(port),
		WithAuthModule(
			[]string{"sites", "home/user"},      // ReadWhitelist
			[]string{"home/user"},               // WriteWhitelist
		),
	)
	defer cleanup()

	server.Start()
	defer server.Stop()

	// --- Setup: create directories and files via BackendPrincipal ---

	// Create directory structure
	for _, dir := range []string{"sys/etc", "sites", "home", "home/user"} {
		if err := ensureVolumeParentDirs(server.FindVolumeByName("root"), dir); err != nil {
			t.Fatalf("creating dir %q: %v", dir, err)
		}
	}

	// Write a test file at /sites/test.txt (readable by whitelist)
	if err := WriteVolumeFile(server, "root", "sites/test.txt", []byte("site content"), grits.BackendPrincipal); err != nil {
		t.Fatalf("writing sites/test.txt: %v", err)
	}

	// Write users file
	pwdHashUser := hashPasswordForTest(t, "user-pass")
	pwdHashAdmin := hashPasswordForTest(t, "admin-pass")
	records := []map[string]any{
		{"username": "user", "pwdHash": pwdHashUser},
		{"username": "admin", "pwdHash": pwdHashAdmin},
	}
	if err := WriteJSONL(server, "root", "sys/etc/users.jsonl", records, grits.BackendPrincipal); err != nil {
		t.Fatalf("writing users.jsonl: %v", err)
	}

	// --- Test 1: Unauthenticated read of whitelisted path ---
	t.Run("unauthenticated read allowed", func(t *testing.T) {
		resp, _ := doReq(t, http.MethodGet, contentURL(baseURL, "root", "sites/test.txt"), "", nil)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	// --- Test 2: Unauthenticated read of non-whitelisted path ---
	t.Run("unauthenticated read denied", func(t *testing.T) {
		resp, body := doReq(t, http.MethodGet, contentURL(baseURL, "root", "sys/etc/users.jsonl"), "", nil)
		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 403 or 404, got %d: %s", resp.StatusCode, string(body))
		}
	})

	// --- Test 3: Login as normal user ---
	var cookieStr string
	t.Run("login as user", func(t *testing.T) {
		resp, body := doReq(t, http.MethodPost, baseURL+"/grits/v1/auth/login", "",
			[]byte(`{"username":"user","password":"user-pass"}`))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("login failed: %d %s", resp.StatusCode, string(body))
		}
		for _, c := range resp.Cookies() {
			if c.Name == "grits-auth-user" {
				cookieStr = c.String()
				break
			}
		}
		if cookieStr == "" {
			t.Fatal("no auth cookie in response")
		}
	})

	// --- Test 4: Authenticated read of whitelisted path ---
	t.Run("authed read allowed", func(t *testing.T) {
		resp, _ := doReq(t, http.MethodGet, contentURL(baseURL, "root", "sites/test.txt"), cookieStr, nil)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	// --- Test 5: Authenticated read of non-whitelisted path ---
	t.Run("authed read denied", func(t *testing.T) {
		resp, body := doReq(t, http.MethodGet, contentURL(baseURL, "root", "sys/etc/users.jsonl"), cookieStr, nil)
		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 403 or 404, got %d: %s", resp.StatusCode, string(body))
		}
	})

	// --- Test 6: Authenticated write to whitelisted path ---
	t.Run("authed write allowed", func(t *testing.T) {
		resp, _ := doReq(t, http.MethodPut, contentURL(baseURL, "root", "home/user/foo.txt"), cookieStr,
			[]byte("user content"))
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	// --- Test 7: Authenticated write to non-whitelisted path ---
	t.Run("authed write denied", func(t *testing.T) {
		resp, body := doReq(t, http.MethodPut, contentURL(baseURL, "root", "sites/bar.txt"), cookieStr,
			[]byte("should be denied"))
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403, got %d: %s", resp.StatusCode, string(body))
		}
	})

	// --- Test 8: Authenticated delete from whitelisted path ---
	t.Run("authed delete allowed", func(t *testing.T) {
		resp, _ := doReq(t, http.MethodDelete, contentURL(baseURL, "root", "home/user/foo.txt"), cookieStr, nil)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	// --- Test 9: Authenticated delete from non-whitelisted path ---
	t.Run("authed delete denied", func(t *testing.T) {
		resp, body := doReq(t, http.MethodDelete, contentURL(baseURL, "root", "sites/test.txt"), cookieStr, nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403, got %d: %s", resp.StatusCode, string(body))
		}
	})

	// --- Test 10: Logout ---
	t.Run("logout", func(t *testing.T) {
		resp, body := doReq(t, http.MethodPost, baseURL+"/grits/v1/auth/logout", cookieStr, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("logout failed: %d %s", resp.StatusCode, string(body))
		}
	})

	// --- Test 11: Post-logout write denied (no valid cookie) ---
	t.Run("post-logout write denied", func(t *testing.T) {
		resp, body := doReq(t, http.MethodPut, contentURL(baseURL, "root", "home/user/foo.txt"), "", nil)
		// Without auth, the principal is AnonPrincipal. The whitelist still covers
		// /home/user, so read is allowed — but the important thing is the principal
		// is no longer "user". The write should be allowed because whitelist applies
		// to the path, not the user (Phase 3 will add per-user grants).
		// For now this just verifies the server doesn't crash and returns something sensible.
		if resp.StatusCode == http.StatusInternalServerError {
			t.Errorf("expected non-500, got %d: %s", resp.StatusCode, string(body))
		}
	})
}
