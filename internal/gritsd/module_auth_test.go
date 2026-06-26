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

	lines, err := ReadJSONL(s, "primary", usersFilePath, grits.BackendPrincipal)
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

	if err := WriteJSONL(s, "primary", usersFilePath, records, grits.BackendPrincipal); err != nil {
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
		WithLocalVolume("primary"),
		WithHttpModule(1911),
		WithAuthModule(),
	)
	defer cleanup()

	server.Start()
	defer server.Stop()

	pwdHash := hashPasswordForTest(t, "test-password")
	writeUserRecord(t, server, "testuser", pwdHash)

	body := `{"username":"testuser","password":"test-password"}`
	resp, err := http.Post("http://127.0.0.1:1911/grits/v1/auth/login",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Check response body for token
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if result["ok"] != true {
		t.Errorf("expected ok=true, got %v", result)
	}
	if token, ok := result["token"].(string); !ok || token == "" {
		t.Errorf("expected non-empty token, got %v", result["token"])
	}

	// Session-only login (no global) should NOT set a cookie
	if setCookie := resp.Header.Get("Set-Cookie"); setCookie != "" {
		t.Errorf("session-only login should not set cookie, got Set-Cookie: %s", setCookie)
	}
}

func TestAuthLoginGlobalSetsCookie(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("primary"),
		WithHttpModule(1918),
		WithAuthModule(),
	)
	defer cleanup()

	server.Start()
	defer server.Stop()

	pwdHash := hashPasswordForTest(t, "test-password")
	writeUserRecord(t, server, "testuser", pwdHash)

	body := `{"username":"testuser","password":"test-password","global":true}`
	resp, err := http.Post("http://127.0.0.1:1918/grits/v1/auth/login",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	setCookie := resp.Header.Get("Set-Cookie")
	if setCookie == "" {
		t.Fatal("global login should set Set-Cookie header")
	}
	if !strings.Contains(setCookie, "grits-auth-user=") {
		t.Errorf("expected cookie grits-auth-user, got: %s", setCookie)
	}
	if !strings.Contains(setCookie, "Max-Age=") {
		t.Errorf("expected Max-Age in cookie, got: %s", setCookie)
	}

	// Response should still include the token for session use
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if token, ok := result["token"].(string); !ok || token == "" {
		t.Errorf("expected non-empty token in body, got %v", result["token"])
	}
}

func TestAuthLoginWrongPassword(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("primary"),
		WithHttpModule(1912),
		WithAuthModule(),
	)
	defer cleanup()

	server.Start()
	defer server.Stop()

	pwdHash := hashPasswordForTest(t, "real-password")
	writeUserRecord(t, server, "testuser", pwdHash)

	body := `{"username":"testuser","password":"wrong-password"}`
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
		WithLocalVolume("primary"),
		WithHttpModule(1913),
		WithAuthModule(),
	)
	defer cleanup()

	server.Start()
	defer server.Stop()

	pwdHash := hashPasswordForTest(t, "irrelevant")
	writeUserRecord(t, server, "someone-else", pwdHash)

	body := `{"username":"nobody","password":"anything"}`
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
		WithLocalVolume("primary"),
		WithHttpModule(1914),
		WithAuthModule(),
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
		WithLocalVolume("primary"),
		WithHttpModule(1915),
		WithAuthModule(),
	)
	defer cleanup()

	server.Start()
	defer server.Stop()

	// First do a global login to set a cookie
	pwdHash := hashPasswordForTest(t, "test-password")
	writeUserRecord(t, server, "testuser", pwdHash)

	loginBody := `{"username":"testuser","password":"test-password","global":true}`
	loginResp, err := http.Post("http://127.0.0.1:1915/grits/v1/auth/login",
		"application/json", strings.NewReader(loginBody))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	loginResp.Body.Close()

	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login expected 200, got %d", loginResp.StatusCode)
	}

	// Check that Set-Cookie was present
	if lc := loginResp.Header.Get("Set-Cookie"); !strings.Contains(lc, "grits-auth-user=") {
		t.Fatalf("login should set grits-auth-user cookie, got: %s", lc)
	}

	// Now log out with no username (clears all)
	resp, err := http.Post("http://127.0.0.1:1915/grits/v1/auth/logout",
		"application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("POST logout: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if result["ok"] != true {
		t.Errorf("expected ok=true, got %v", result)
	}

	// Check that the cookie was cleared
	setCookie := resp.Header.Values("Set-Cookie")
	foundClear := false
	for _, sc := range setCookie {
		if strings.HasPrefix(sc, "grits-auth-user=;") || strings.HasPrefix(sc, "grits-auth-user=") {
			foundClear = true
			if !strings.Contains(sc, "Max-Age=0") && !strings.Contains(sc, "max-age=0") {
				t.Errorf("expected Max-Age=0 (or negative) in clearing cookie, got: %s", sc)
			}
		}
	}
	if !foundClear {
		t.Errorf("logout should return Set-Cookie to clear grits-auth-user, got: %v", setCookie)
	}
}

func TestAuthLogoutWithUsername(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("primary"),
		WithHttpModule(1919),
		WithAuthModule(),
	)
	defer cleanup()

	server.Start()
	defer server.Stop()

	// Login globally to set cookie
	pwdHash := hashPasswordForTest(t, "test-password")
	writeUserRecord(t, server, "testuser", pwdHash)

	loginResp, err := http.Post("http://127.0.0.1:1919/grits/v1/auth/login",
		"application/json",
		strings.NewReader(`{"username":"testuser","password":"test-password","global":true}`))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	loginResp.Body.Close()

	// Logout with username specified
	resp, err := http.Post("http://127.0.0.1:1919/grits/v1/auth/logout",
		"application/json",
		strings.NewReader(`{"username":"testuser"}`))
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Cookie should be cleared
	setCookie := resp.Header.Values("Set-Cookie")
	foundClear := false
	for _, sc := range setCookie {
		if strings.HasPrefix(sc, "grits-auth-user=") {
			foundClear = true
		}
	}
	if !foundClear {
		t.Errorf("logout with username should clear grits-auth-user cookie, got: %v", setCookie)
	}
}

func TestAuthLoginMethodNotAllowed(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithHttpModule(1916),
		WithAuthModule(),
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

func TestWhoamiWithSessionToken(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("primary"),
		WithHttpModule(1925),
		WithAuthModule(),
	)
	defer cleanup()

	server.Start()
	defer server.Stop()

	pwdHash := hashPasswordForTest(t, "test-password")
	writeUserRecord(t, server, "whoamiuser", pwdHash)

	// Login (session-only, no cookie) and get token
	loginResp, loginBody := doReq(t, http.MethodPost, "http://127.0.0.1:1925/grits/v1/auth/login", "",
		[]byte(`{"username":"whoamiuser","password":"test-password"}`))
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login failed: %d %s", loginResp.StatusCode, string(loginBody))
	}
	var loginResult struct {
		Token string `json:"token"`
	}
	json.Unmarshal(loginBody, &loginResult)

	// Query whoami with session token
	whoamiResp, whoamiBody := doReq(t, http.MethodGet, "http://127.0.0.1:1925/grits/v1/whoami",
		loginResult.Token, nil)
	if whoamiResp.StatusCode != http.StatusOK {
		t.Fatalf("whoami: expected 200, got %d: %s", whoamiResp.StatusCode, string(whoamiBody))
	}

	var whoamiResult struct {
		Identities []struct {
			Username string      `json:"username"`
			Status   TokenStatus `json:"status"`
			Expiry   int64       `json:"expiry"`
		} `json:"identities"`
	}
	if err := json.Unmarshal(whoamiBody, &whoamiResult); err != nil {
		t.Fatalf("unmarshal whoami: %v", err)
	}
	if len(whoamiResult.Identities) == 0 {
		t.Fatal("whoami: expected at least 1 identity")
	}
	found := false
	for _, id := range whoamiResult.Identities {
		if id.Username == "whoamiuser" && id.Status == TokenActive {
			found = true
		}
	}
	if !found {
		t.Errorf("whoami: expected active identity for whoamiuser, got %+v", whoamiResult.Identities)
	}
}

func TestWhoamiWithCookie(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("primary"),
		WithHttpModule(1926),
		WithAuthModule(),
	)
	defer cleanup()

	server.Start()
	defer server.Stop()

	pwdHash := hashPasswordForTest(t, "test-password")
	writeUserRecord(t, server, "cookieuser", pwdHash)

	// Login globally to set cookie
	loginResp, err := http.Post("http://127.0.0.1:1926/grits/v1/auth/login",
		"application/json",
		strings.NewReader(`{"username":"cookieuser","password":"test-password","global":true}`))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login failed: %d", loginResp.StatusCode)
	}

	// Read the Set-Cookie from login response
	cookieHeader := loginResp.Header.Get("Set-Cookie")

	// Query whoami with the cookie but no session token
	req, _ := http.NewRequest("GET", "http://127.0.0.1:1926/grits/v1/whoami", nil)
	req.Header.Set("Cookie", extractCookieValue(cookieHeader))
	client := &http.Client{}
	whoamiResp, err := client.Do(req)
	if err != nil {
		t.Fatalf("whoami request: %v", err)
	}
	defer whoamiResp.Body.Close()
	whoamiBody, _ := io.ReadAll(whoamiResp.Body)

	if whoamiResp.StatusCode != http.StatusOK {
		t.Fatalf("whoami: expected 200, got %d: %s", whoamiResp.StatusCode, string(whoamiBody))
	}

	var whoamiResult struct {
		Identities []struct {
			Username string      `json:"username"`
			Status   TokenStatus `json:"status"`
			Expiry   int64       `json:"expiry"`
		} `json:"identities"`
	}
	if err := json.Unmarshal(whoamiBody, &whoamiResult); err != nil {
		t.Fatalf("unmarshal whoami: %v", err)
	}
	found := false
	for _, id := range whoamiResult.Identities {
		if id.Username == "cookieuser" && id.Status == TokenActive {
			found = true
		}
	}
	if !found {
		t.Errorf("whoami with cookie: expected active identity for cookieuser, got %+v", whoamiResult.Identities)
	}
}

// extractCookieValue pulls out "name=value" from a Set-Cookie header string.
func extractCookieValue(setCookie string) string {
	if idx := strings.Index(setCookie, ";"); idx >= 0 {
		return setCookie[:idx]
	}
	return setCookie
}

func TestCookieAuthFallback(t *testing.T) {
	port := 1927
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	server, cleanup := SetupTestServer(t,
		WithLocalVolume("primary"),
		WithHttpModule(port),
		WithAuthModule(),
	)
	defer cleanup()

	server.Start()
	defer server.Stop()

	// Setup: public grant + user-specific grant
	bt := boolTrue()
	vol := server.FindVolumeByName("primary")
	ensureVolumeParentDirs(vol, "sites/.grits")
	sitesAccess := AccessConfig{Allow: []Grant{{All: bt, Origin: "*", Permission: PermRead}}}
	raw, _ := json.Marshal(sitesAccess)
	WriteVolumeFile(server, "primary", "sites/.grits/access.json", raw, grits.BackendPrincipal)

	ensureVolumeParentDirs(vol, "home/user/.grits")
	homeAccess := AccessConfig{Allow: []Grant{{User: "testuser", Origin: "*", Permission: PermReadWrite}}}
	raw2, _ := json.Marshal(homeAccess)
	WriteVolumeFile(server, "primary", "home/user/.grits/access.json", raw2, grits.BackendPrincipal)

	WriteVolumeFile(server, "primary", "sites/test.txt", []byte("site content"), grits.BackendPrincipal)

	// Create a file in user's home directory
	WriteVolumeFile(server, "primary", "home/user/myfile.txt", []byte("user content"), grits.BackendPrincipal)

	// Create user
	pwdHash := hashPasswordForTest(t, "test-password")
	writeUserRecord(t, server, "testuser", pwdHash)

	// Login globally to get a cookie
	loginResp, err := http.Post(baseURL+"/grits/v1/auth/login",
		"application/json",
		strings.NewReader(`{"username":"testuser","password":"test-password","global":true}`))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	loginResp.Body.Close()
	cookieValue := extractCookieValue(loginResp.Header.Get("Set-Cookie"))

	// Request content with cookie but NO X-Grits-Auth-Token header
	req, _ := http.NewRequest("GET", contentURL(baseURL, "primary", "sites/test.txt"), nil)
	req.Header.Set("Cookie", cookieValue)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("cookie auth: expected 200 for public file, got %d", resp.StatusCode)
	}

	// Request user-specific content with cookie
	req2, _ := http.NewRequest("GET", contentURL(baseURL, "primary", "home/user/myfile.txt"), nil)
	req2.Header.Set("Cookie", cookieValue)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("request2: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("cookie auth: expected 200 for user's file, got %d", resp2.StatusCode)
	}
}

func TestAuthNoUsersFile(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("primary"),
		WithHttpModule(1917),
		WithAuthModule(),
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

// doReq sends an HTTP request with an optional auth token and returns the response.
func doReq(t *testing.T, method, url, authToken string, body []byte) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest(%s %s): %v", method, url, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if authToken != "" {
		req.Header.Set("X-Grits-Auth-Token", authToken)
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
		WithLocalVolume("primary"),
		WithHttpModule(port),
		WithAuthModule(),
	)
	defer cleanup()

	server.Start()
	defer server.Stop()

	// --- Setup: create directories and files via BackendPrincipal ---

	// Create directory structure and .grits directories for access control
	for _, dir := range []string{"sys/etc", "sites", "sites/.grits", "home", "home/user", "home/user/.grits"} {
		if err := ensureVolumeParentDirs(server.FindVolumeByName("primary"), dir); err != nil {
			t.Fatalf("creating dir %q: %v", dir, err)
		}
	}

	// Grant everyone (including anonymous) read access to /sites
	anonTrue := true
	sitesAccess := AccessConfig{
		Allow: []Grant{
			{All: &anonTrue, Origin: "*", Permission: PermRead},
		},
	}
	sitesAccessData, _ := json.Marshal(sitesAccess)
	if err := WriteVolumeFile(server, "primary", "sites/.grits/access.json", sitesAccessData, grits.BackendPrincipal); err != nil {
		t.Fatalf("writing sites/.grits/access.json: %v", err)
	}

	// Grant user read+write access to /home/user
	homeAccess := AccessConfig{
		Allow: []Grant{
			{User: "user", Origin: "*", Permission: PermReadWrite},
		},
	}
	homeAccessData, _ := json.Marshal(homeAccess)
	if err := WriteVolumeFile(server, "primary", "home/user/.grits/access.json", homeAccessData, grits.BackendPrincipal); err != nil {
		t.Fatalf("writing home/user/.grits/access.json: %v", err)
	}

	// Write a test file at /sites/test.txt
	if err := WriteVolumeFile(server, "primary", "sites/test.txt", []byte("site content"), grits.BackendPrincipal); err != nil {
		t.Fatalf("writing sites/test.txt: %v", err)
	}

	// Write users file
	pwdHashUser := hashPasswordForTest(t, "user-pass")
	pwdHashAdmin := hashPasswordForTest(t, "admin-pass")
	records := []map[string]any{
		{"username": "user", "pwdHash": pwdHashUser},
		{"username": "admin", "pwdHash": pwdHashAdmin},
	}
	if err := WriteJSONL(server, "primary", "sys/etc/users.jsonl", records, grits.BackendPrincipal); err != nil {
		t.Fatalf("writing users.jsonl: %v", err)
	}

	// --- Test 1: Unauthenticated read of path with public grant ---
	t.Run("unauthenticated read allowed", func(t *testing.T) {
		resp, _ := doReq(t, http.MethodGet, contentURL(baseURL, "primary", "sites/test.txt"), "", nil)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	// --- Test 2: Unauthenticated read of non-whitelisted path ---
	t.Run("unauthenticated read denied", func(t *testing.T) {
		resp, body := doReq(t, http.MethodGet, contentURL(baseURL, "primary", "sys/etc/users.jsonl"), "", nil)
		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 403 or 404, got %d: %s", resp.StatusCode, string(body))
		}
	})

	// --- Test 3: Login as normal user ---
	var authToken string
	t.Run("login as user", func(t *testing.T) {
		resp, body := doReq(t, http.MethodPost, baseURL+"/grits/v1/auth/login", "",
			[]byte(`{"username":"user","password":"user-pass"}`))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("login failed: %d %s", resp.StatusCode, string(body))
		}
		var loginResp struct {
			Ok    bool   `json:"ok"`
			Token string `json:"token"`
		}
		if err := json.Unmarshal(body, &loginResp); err != nil {
			t.Fatalf("unmarshal login response: %v", err)
		}
		if !loginResp.Ok || loginResp.Token == "" {
			t.Fatal("login response missing token")
		}
		authToken = loginResp.Token
	})

	// --- Test 4: Authenticated read of whitelisted path ---
	t.Run("authed read allowed", func(t *testing.T) {
		resp, _ := doReq(t, http.MethodGet, contentURL(baseURL, "primary", "sites/test.txt"), authToken, nil)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	// --- Test 5: Authenticated read of non-whitelisted path ---
	t.Run("authed read denied", func(t *testing.T) {
		resp, body := doReq(t, http.MethodGet, contentURL(baseURL, "primary", "sys/etc/users.jsonl"), authToken, nil)
		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 403 or 404, got %d: %s", resp.StatusCode, string(body))
		}
	})

	// --- Test 6: Authenticated write to whitelisted path ---
	t.Run("authed write allowed", func(t *testing.T) {
		resp, _ := doReq(t, http.MethodPut, contentURL(baseURL, "primary", "home/user/foo.txt"), authToken,
			[]byte("user content"))
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	// --- Test 7: Authenticated write to non-whitelisted path ---
	t.Run("authed write denied", func(t *testing.T) {
		resp, body := doReq(t, http.MethodPut, contentURL(baseURL, "primary", "sites/bar.txt"), authToken,
			[]byte("should be denied"))
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403, got %d: %s", resp.StatusCode, string(body))
		}
	})

	// --- Test 8: Authenticated delete from whitelisted path ---
	t.Run("authed delete allowed", func(t *testing.T) {
		resp, _ := doReq(t, http.MethodDelete, contentURL(baseURL, "primary", "home/user/foo.txt"), authToken, nil)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	// --- Test 9: Authenticated delete from non-whitelisted path ---
	t.Run("authed delete denied", func(t *testing.T) {
		resp, body := doReq(t, http.MethodDelete, contentURL(baseURL, "primary", "sites/test.txt"), authToken, nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403, got %d: %s", resp.StatusCode, string(body))
		}
	})

	// --- Test 10: Logout ---
	t.Run("logout", func(t *testing.T) {
		resp, body := doReq(t, http.MethodPost, baseURL+"/grits/v1/auth/logout", authToken, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("logout failed: %d %s", resp.StatusCode, string(body))
		}
	})

	// --- Test 11: Post-logout write denied (no valid cookie) ---
	t.Run("post-logout write denied", func(t *testing.T) {
		resp, body := doReq(t, http.MethodPut, contentURL(baseURL, "primary", "home/user/foo.txt"), "", nil)
		// Without auth, the principal is AnonPrincipal. The access.json at
		// /home/user only grants write to user "user", so AnonPrincipal gets denied.
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403, got %d: %s", resp.StatusCode, string(body))
		}
	})
}

// boolTrue returns a *bool pointing to true, for use in Grant literals.
func boolTrue() *bool { t := true; return &t }

/////
// Permission helper unit tests

func TestCanRead(t *testing.T) {
	tests := []struct {
		p  Permission
		ok bool
	}{
		{PermRead, true}, {PermInsert, false},
		{PermReadInsert, true}, {PermReadWrite, true},
		{PermOwner, true}, {"", false}, {"blah", false},
	}
	for _, tc := range tests {
		if got := CanRead(tc.p); got != tc.ok {
			t.Errorf("CanRead(%q) = %v, want %v", tc.p, got, tc.ok)
		}
	}
}

func TestCanInsert(t *testing.T) {
	tests := []struct {
		p  Permission
		ok bool
	}{
		{PermRead, false}, {PermInsert, true},
		{PermReadInsert, true}, {PermReadWrite, true},
		{PermOwner, true}, {"", false},
	}
	for _, tc := range tests {
		if got := CanInsert(tc.p); got != tc.ok {
			t.Errorf("CanInsert(%q) = %v, want %v", tc.p, got, tc.ok)
		}
	}
}

func TestCanWrite(t *testing.T) {
	tests := []struct {
		p  Permission
		ok bool
	}{
		{PermRead, false}, {PermInsert, false},
		{PermReadInsert, false}, {PermReadWrite, true},
		{PermOwner, true}, {"", false},
	}
	for _, tc := range tests {
		if got := CanWrite(tc.p); got != tc.ok {
			t.Errorf("CanWrite(%q) = %v, want %v", tc.p, got, tc.ok)
		}
	}
}

func TestCanOwn(t *testing.T) {
	tests := []struct {
		p  Permission
		ok bool
	}{
		{PermRead, false}, {PermInsert, false},
		{PermReadInsert, false}, {PermReadWrite, false},
		{PermOwner, true}, {"", false},
	}
	for _, tc := range tests {
		if got := CanOwn(tc.p); got != tc.ok {
			t.Errorf("CanOwn(%q) = %v, want %v", tc.p, got, tc.ok)
		}
	}
}

func TestMergePerm(t *testing.T) {
	tests := []struct {
		a, b, want Permission
	}{
		{"", "", ""},
		{PermRead, "", PermRead},
		{"", PermRead, PermRead},
		{PermRead, PermRead, PermRead},
		{PermRead, PermInsert, PermReadInsert},
		{PermRead, PermReadInsert, PermReadInsert},
		{PermRead, PermReadWrite, PermReadWrite},
		{PermRead, PermOwner, PermOwner},
		{PermInsert, PermReadWrite, PermReadWrite},
		{PermInsert, PermOwner, PermOwner},
		{PermReadWrite, PermOwner, PermOwner},
		{PermReadInsert, PermReadWrite, PermReadWrite},
		{PermReadInsert, PermInsert, PermReadInsert},
	}
	for _, tc := range tests {
		if got := mergePerm(tc.a, tc.b); got != tc.want {
			t.Errorf("mergePerm(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestParentPath(t *testing.T) {
	tests := []struct {
		path, want string
	}{
		{"", ""},
		{"foo", ""},
		{"foo/bar", "foo"},
		{"foo/bar/baz", "foo/bar"},
		{"foo/", ""},
		{"/foo/bar", "/foo"},
	}
	for _, tc := range tests {
		if got := parentPath(tc.path); got != tc.want {
			t.Errorf("parentPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestResolveOrigin(t *testing.T) {
	tests := []struct {
		name      string
		origin    string
		coreVhost string
		want      string
	}{
		{"star passthrough", "*", "http://test.local", "*"},
		{"empty passthrough", "", "http://test.local", ""},
		{"single-word expands to subdomain of coreVhost", "gimbal", "http://test.local", "https://gimbal.test.local"},
		{"single-word with https coreVhost", "music", "https://gimbal.example.org", "https://music.gimbal.example.org"},
		{"single-word with dotless coreVhost", "foo", "http://localhost:8080", "https://foo.localhost"},
		{"absolute http passthrough", "http://example.com/path", "http://test.local", "http://example.com/path"},
		{"absolute https passthrough", "https://app.example.com/foo", "http://test.local", "https://app.example.com/foo"},
		{"bare hostname expands to subdomain of coreVhost", "gimbal.example.com", "http://test.local", "https://gimbal.example.com.test.local"},
		{"slash becomes inert", "/", "http://test.local", ""},
		{"slash with text becomes inert", "foo/bar", "http://test.local", ""},
		{"asterisk in origin becomes inert", "foo*", "http://test.local", ""},
	}
	for _, tc := range tests {
		m := &AuthModule{Config: &AuthModuleConfig{CoreVhost: tc.coreVhost}}
		got := m.resolveOrigin(tc.origin)
		if got != tc.want {
			t.Errorf("%s: resolveOrigin(%q) with coreVhost=%q = %q, want %q",
				tc.name, tc.origin, tc.coreVhost, got, tc.want)
		}
	}
}

func TestGrantMatchesPrincipal(t *testing.T) {
	bt := boolTrue()
	anon := &grits.Principal{}
	auth := &grits.Principal{User: "alice"}
	tests := []struct {
		name  string
		grant Grant
		princ *grits.Principal
		match bool
	}{
		{"specific user matches", Grant{User: "alice", Origin: "*"}, auth, true},
		{"specific user no match", Grant{User: "alice", Origin: "*"}, anon, false},
		{"specific wrong user", Grant{User: "bob", Origin: "*"}, auth, false},
		{"auth matches authenticated", Grant{Auth: bt, Origin: "*"}, auth, true},
		{"auth no match anonymous", Grant{Auth: bt, Origin: "*"}, anon, false},
		{"anon matches anonymous", Grant{All: bt, Origin: "*"}, anon, true},
		{"anon matches auth", Grant{All: bt, Origin: "*"}, auth, true},
		{"user overrides auth", Grant{User: "alice", Auth: bt, Origin: "*"}, anon, false},
		{"user overrides anon", Grant{User: "alice", All: bt, Origin: "*"}, anon, false},
		{"empty grant no match", Grant{}, auth, false},
		{"origin alone matches specific", Grant{Origin: "https://app.example.com"}, &grits.Principal{Origin: "https://app.example.com"}, true},
		{"origin alone rejects different", Grant{Origin: "https://app.example.com"}, &grits.Principal{Origin: "https://evil.com"}, false},
		{"origin alone with empty principal (direct nav) passes", Grant{Origin: "https://app.example.com"}, &grits.Principal{}, true},
		{"origin star matches any", Grant{Origin: "*", All: bt}, &grits.Principal{Origin: "https://anything"}, true},
		{"origin star matches empty", Grant{Origin: "*", All: bt}, &grits.Principal{}, true},
		{"origin narrows user", Grant{User: "alice", Origin: "https://app.example.com"}, &grits.Principal{User: "alice", Origin: "https://app.example.com"}, true},
		{"origin rejects user from wrong", Grant{User: "alice", Origin: "https://app.example.com"}, &grits.Principal{User: "alice", Origin: "https://evil.com"}, false},
		{"no origin matches any origin", Grant{User: "alice", Origin: "*"}, &grits.Principal{User: "alice", Origin: "https://anything"}, true},
		{"origin star with user", Grant{User: "bob", Origin: "*"}, &grits.Principal{User: "bob", Origin: "https://anything"}, true},
		{"empty origin never matches", Grant{User: "alice"}, &grits.Principal{User: "alice"}, false},
		// Direct navigation (empty principal origin) always passes the
		// origin check even when the grant has a specific origin.
		{"direct nav passes origin check", Grant{Origin: "https://app.example.com"}, &grits.Principal{}, true},
	}
	for _, tc := range tests {
		got := grantMatchesPrincipal(tc.grant, tc.princ)
		if got != tc.match {
			t.Errorf("%s: grantMatchesPrincipal(%+v, %+v) = %v, want %v",
				tc.name, tc.grant, tc.princ, got, tc.match)
		}
	}
}

/////
// readAccessConfig tests

func TestReadAccessConfig(t *testing.T) {
	server, cleanup := SetupTestServer(t)
	defer cleanup()

	volConfig := &LocalVolumeConfig{VolumeName: "primary"}
	vol, err := NewLocalVolume(volConfig, server, false, false)
	if err != nil {
		t.Fatalf("NewLocalVolume: %v", err)
	}
	server.AddModule(vol)
	server.AddVolume(vol)
	server.Start()
	defer server.Stop()

	// Create auth module so we can call readAccessConfig
	authMod, err := NewAuthModule(server, &AuthModuleConfig{CoreVhost: "http://test.local"})
	if err != nil {
		t.Fatalf("NewAuthModule: %v", err)
	}

	// No access.json yet — should return nil, nil
	cfg, err := authMod.readAccessConfig(vol, "some/dir")
	if err != nil {
		t.Errorf("readAccessConfig on missing file: %v", err)
	}
	if cfg != nil {
		t.Errorf("expected nil config for missing file, got %+v", cfg)
	}

	// Create a valid access.json
	ensureVolumeParentDirs(vol, "data/.grits")
	bt := boolTrue()
	access := AccessConfig{
		Allow: []Grant{
			{User: "alice", Origin: "*", Permission: PermOwner},
			{All: bt, Origin: "*", Permission: PermRead},
		},
	}
	raw, _ := json.Marshal(access)
	WriteVolumeFile(server, "primary", "data/.grits/access.json", raw, grits.BackendPrincipal)

	cfg, err = authMod.readAccessConfig(vol, "data")
	if err != nil {
		t.Fatalf("readAccessConfig on existing file: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Allow) != 2 {
		t.Fatalf("expected 2 grants, got %d", len(cfg.Allow))
	}
	if cfg.Allow[0].User != "alice" || cfg.Allow[0].Permission != PermOwner {
		t.Errorf("first grant: %+v", cfg.Allow[0])
	}
	if cfg.Allow[1].Permission != PermRead {
		t.Errorf("second grant permission: got %q, want %q", cfg.Allow[1].Permission, PermRead)
	}

	// Test with root-level path
	ensureVolumeParentDirs(vol, ".grits")
	rootAccess := AccessConfig{
		Allow: []Grant{{User: "glenda", Origin: "*", Permission: PermOwner}},
	}
	raw2, _ := json.Marshal(rootAccess)
	WriteVolumeFile(server, "primary", ".grits/access.json", raw2, grits.BackendPrincipal)

	cfg, err = authMod.readAccessConfig(vol, "")
	if err != nil {
		t.Fatalf("readAccessConfig at root: %v", err)
	}
	if cfg == nil || len(cfg.Allow) != 1 || cfg.Allow[0].User != "glenda" {
		t.Errorf("root access.json: %+v", cfg)
	}
}

/////
// resolvePermission tests

// setupPermTest creates a server with a "primary" volume and an auth module,
// creates a .grits/access.json at the given path, and returns the volume and auth module.
func setupPermTest(t *testing.T, accessPath string, access AccessConfig) (Volume, *AuthModule) {
	t.Helper()
	server, _ := SetupTestServer(t)

	volConfig := &LocalVolumeConfig{VolumeName: "primary"}
	vol, err := NewLocalVolume(volConfig, server, false, false)
	if err != nil {
		t.Fatalf("NewLocalVolume: %v", err)
	}
	server.AddModule(vol)
	server.AddVolume(vol)
	server.Start()

	authMod, err := NewAuthModule(server, &AuthModuleConfig{CoreVhost: "http://test.local"})
	if err != nil {
		t.Fatalf("NewAuthModule: %v", err)
	}

	if accessPath != "" {
		gritsDir := accessPath + "/.grits"
		if err := ensureVolumeParentDirs(vol, gritsDir); err != nil {
			t.Fatalf("ensureVolumeParentDirs(%q): %v", gritsDir, err)
		}
		raw, _ := json.Marshal(access)
		if err := WriteVolumeFile(server, "primary", gritsDir+"/access.json", raw, grits.BackendPrincipal); err != nil {
			t.Fatalf("WriteVolumeFile access.json: %v", err)
		}
	}

	return vol, authMod
}

func TestResolvePermissionDefaultDeny(t *testing.T) {
	vol, authMod := setupPermTest(t, "", AccessConfig{})
	anon := &grits.Principal{}
	auth := &grits.Principal{User: "alice"}

	for _, princ := range []*grits.Principal{anon, auth} {
		perm := authMod.resolvePermissionAtRoot(vol, nil, "anything", princ)
		if perm != "" {
			t.Errorf("expected deny, got %q for %+v", perm, princ)
		}
	}
}

func TestResolvePermissionReadInherited(t *testing.T) {
	bt := boolTrue()
	vol, authMod := setupPermTest(t, "data",
		AccessConfig{Allow: []Grant{{All: bt, Origin: "*", Permission: PermRead}}})

	anon := &grits.Principal{}

	for _, path := range []string{"data", "data/sub", "data/sub/deep"} {
		perm := authMod.resolvePermissionAtRoot(vol, nil, path, anon)
		if !CanRead(perm) {
			t.Errorf("expected read for %q, got %q", path, perm)
		}
	}

	perm := authMod.resolvePermissionAtRoot(vol, nil, "other", anon)
	if CanRead(perm) {
		t.Errorf("expected deny for 'other', got %q", perm)
	}
}

func TestResolvePermissionWriteInherited(t *testing.T) {
	bt := boolTrue()
	vol, authMod := setupPermTest(t, "work",
		AccessConfig{Allow: []Grant{{All: bt, Origin: "*", Permission: PermReadWrite}}})

	anon := &grits.Principal{}

	for _, path := range []string{"work", "work/sub"} {
		perm := authMod.resolvePermissionAtRoot(vol, nil, path, anon)
		if !CanWrite(perm) {
			t.Errorf("expected write for %q, got %q", path, perm)
		}
	}
}

func TestResolvePermissionInsertNotInherited(t *testing.T) {
	bt := boolTrue()
	vol, authMod := setupPermTest(t, "parent",
		AccessConfig{Allow: []Grant{{All: bt, Origin: "*", Permission: PermInsert}}})

	anon := &grits.Principal{}

	perm := authMod.resolvePermissionAtRoot(vol, nil, "parent", anon)
	if !CanInsert(perm) {
		t.Errorf("expected insert at 'parent', got %q", perm)
	}

	perm = authMod.resolvePermissionAtRoot(vol, nil, "parent/child", anon)
	if CanInsert(perm) {
		t.Errorf("insert should NOT be inherited, got %q", perm)
	}
	vol2, authMod2 := setupPermTest(t, "base",
		AccessConfig{Allow: []Grant{{All: bt, Origin: "*", Permission: PermReadInsert}}})
	perm = authMod2.resolvePermissionAtRoot(vol2, nil, "base/child", anon)
	if !CanRead(perm) {
		t.Errorf("expected read at 'base/child', got %q", perm)
	}
	if CanInsert(perm) {
		t.Errorf("insert should NOT be inherited, got %q", perm)
	}
}

func TestResolvePermissionGritsProtection(t *testing.T) {
	bt := boolTrue()
	// Grant read+write at "data". Since .grits is the immediate child,
	// read+write should NOT escalate to owner — it contributes only read.
	vol, authMod := setupPermTest(t, "data",
		AccessConfig{Allow: []Grant{{All: bt, Origin: "*", Permission: PermReadWrite}}})

	anon := &grits.Principal{}

	perm := authMod.resolvePermissionAtRoot(vol, nil, "data/.grits", anon)
	if !CanRead(perm) {
		t.Errorf("expected read for .grits, got %q", perm)
	}
	if CanWrite(perm) {
		t.Errorf("expected NO write for .grits (read+write at parent should NOT escalate to owner), got %q", perm)
	}

	perm = authMod.resolvePermissionAtRoot(vol, nil, "data/.grits/access.json", anon)
	if !CanRead(perm) {
		t.Errorf("expected read for .grits/file, got %q", perm)
	}
	if CanWrite(perm) {
		t.Errorf("expected NO write for .grits/file (read+write at parent should NOT escalate to owner), got %q", perm)
	}
}

func TestResolvePermissionGritsBarrierNonImmediate(t *testing.T) {
	bt := boolTrue()
	// Grant read+write at "foo". For path "foo/bar/.grits", the .grits is NOT
	// the immediate child of "foo" — "bar" is. So read+write escalates to owner
	// at "foo/bar", and owner passes through .grits normally.
	vol, authMod := setupPermTest(t, "foo",
		AccessConfig{Allow: []Grant{{All: bt, Origin: "*", Permission: PermReadWrite}}})

	anon := &grits.Principal{}

	perm := authMod.resolvePermissionAtRoot(vol, nil, "foo/bar/.grits", anon)
	if !CanWrite(perm) {
		t.Errorf("expected write at foo/bar/.grits (read+write at foo, not direct parent), got %q", perm)
	}
	if !CanRead(perm) {
		t.Errorf("expected read at foo/bar/.grits, got %q", perm)
	}
}

func TestResolvePermissionGritsBarrierNested(t *testing.T) {
	bt := boolTrue()
	// Grant read+write at "projects". For path "projects/alice/docs/.grits",
	// the .grits is three levels deep. read+write escalates to owner at
	// "projects/alice", then "projects/alice/docs", and owner passes through
	// .grits normally.
	vol, authMod := setupPermTest(t, "projects",
		AccessConfig{Allow: []Grant{{All: bt, Origin: "*", Permission: PermReadWrite}}})

	anon := &grits.Principal{}

	perm := authMod.resolvePermissionAtRoot(vol, nil, "projects/alice/docs/.grits", anon)
	if !CanWrite(perm) {
		t.Errorf("expected write at projects/alice/docs/.grits (read+write at projects, not direct parent), got %q", perm)
	}
}

func TestResolvePermissionGritsBarrierDirectOnly(t *testing.T) {
	bt := boolTrue()
	// Two-level grant: read+write at "projects" and read+write at "projects/alice".
	// For path "projects/alice/.grits":
	//   - grant at "projects" -> next seg is "alice" (not .grits) -> inherits as owner
	//   - grant at "projects/alice" -> next seg is .grits -> contributes only read
	// Merged: owner + read = owner. The higher-level read+write that already
	// escalated before reaching .grits should still give effective ownership.
	server, _ := SetupTestServer(t)
	defer func() { server.Stop() }()

	volConfig := &LocalVolumeConfig{VolumeName: "primary"}
	vol, err := NewLocalVolume(volConfig, server, false, false)
	if err != nil {
		t.Fatalf("NewLocalVolume: %v", err)
	}
	server.AddModule(vol)
	server.AddVolume(vol)
	server.Start()

	authMod, err := NewAuthModule(server, &AuthModuleConfig{CoreVhost: "http://test.local"})
	if err != nil {
		t.Fatalf("NewAuthModule: %v", err)
	}

	// read+write at root level
	rootAccess := AccessConfig{Allow: []Grant{{All: bt, Origin: "*", Permission: PermReadWrite}}}
	raw, _ := json.Marshal(rootAccess)
	ensureVolumeParentDirs(vol, ".grits")
	WriteVolumeFile(server, "primary", ".grits/access.json", raw, grits.BackendPrincipal)

	// read+write at "projects/alice" — this is the direct parent of .grits
	aliceAccess := AccessConfig{Allow: []Grant{{All: bt, Origin: "*", Permission: PermReadWrite}}}
	raw2, _ := json.Marshal(aliceAccess)
	ensureVolumeParentDirs(vol, "projects/alice/.grits")
	WriteVolumeFile(server, "primary", "projects/alice/.grits/access.json", raw2, grits.BackendPrincipal)

	anon := &grits.Principal{}

	perm := authMod.resolvePermissionAtRoot(vol, nil, "projects/alice/.grits", anon)
	if !CanWrite(perm) {
		t.Errorf("expected write at projects/alice/.grits (owner from root combines with read from alice), got %q", perm)
	}
}

func TestLinkCallbackGritsWritePermission(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("primary"),
		WithHttpModule(1924),
		WithAuthModule(),
	)
	defer cleanup()
	server.Start()
	defer server.Stop()

	vol := server.FindVolumeByName("primary")

	// r+w dir: user "alice" has read+write
	ensureVolumeParentDirs(vol, "readwritedir/.grits")
	rwAccess := AccessConfig{Allow: []Grant{{User: "alice", Origin: "*", Permission: PermReadWrite}}}
	raw, _ := json.Marshal(rwAccess)
	WriteVolumeFile(server, "primary", "readwritedir/.grits/access.json", raw, grits.BackendPrincipal)

	// owner dir: user "bob" has owner
	ensureVolumeParentDirs(vol, "ownerdir/.grits")
	ownAccess := AccessConfig{Allow: []Grant{{User: "bob", Origin: "*", Permission: PermOwner}}}
	raw2, _ := json.Marshal(ownAccess)
	WriteVolumeFile(server, "primary", "ownerdir/.grits/access.json", raw2, grits.BackendPrincipal)

	// Setup users for auth
	pwdHashAlice := hashPasswordForTest(t, "alice-pass")
	pwdHashBob := hashPasswordForTest(t, "bob-pass")
	records := []map[string]any{
		{"username": "alice", "pwdHash": pwdHashAlice},
		{"username": "bob", "pwdHash": pwdHashBob},
	}
	ensureVolumeParentDirs(vol, "sys/etc")
	WriteJSONL(server, "primary", "sys/etc/users.jsonl", records, grits.BackendPrincipal)

	baseURL := "http://127.0.0.1:1924"

	// Login helpers
	login := func(user, pass string) string {
		resp, body := doReq(t, http.MethodPost, baseURL+"/grits/v1/auth/login", "",
			[]byte(`{"username":"`+user+`","password":"`+pass+`"}`))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("login failed: %d %s", resp.StatusCode, string(body))
		}
		var loginResp struct {
			Ok    bool   `json:"ok"`
			Token string `json:"token"`
		}
		if err := json.Unmarshal(body, &loginResp); err != nil {
			t.Fatalf("unmarshal login response: %v", err)
		}
		return loginResp.Token
	}

	aliceToken := login("alice", "alice-pass")
	bobToken := login("bob", "bob-pass")

	t.Run("read+write cannot write .grits", func(t *testing.T) {
		// alice has read+write on readwritedir — trying to create readwritedir/.grits/newfile should fail
		resp, _ := doReq(t, http.MethodPut,
			contentURL(baseURL, "primary", "readwritedir/.grits/newfile"), aliceToken,
			[]byte("should be denied"))
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("write to .grits with read+write: expected 403, got %d", resp.StatusCode)
		}
	})

	t.Run("read+write cannot overwrite .grits entry", func(t *testing.T) {
		// First create the .grits dir as backend so it exists
		ensureVolumeParentDirs(vol, "readwritedir/.grits")

		// alice tries to replace .grits entirely — should be denied
		resp, _ := doReq(t, http.MethodPut,
			contentURL(baseURL, "primary", "readwritedir/.grits"), aliceToken,
			[]byte("should be denied"))
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("replace .grits with read+write: expected 403, got %d", resp.StatusCode)
		}
	})

	t.Run("owner can write .grits", func(t *testing.T) {
		resp, _ := doReq(t, http.MethodPut,
			contentURL(baseURL, "primary", "ownerdir/.grits/newfile"), bobToken,
			[]byte("owner content"))
		if resp.StatusCode != http.StatusOK {
			t.Errorf("write to .grits with owner: expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("owner can overwrite .grits entry", func(t *testing.T) {
		ensureVolumeParentDirs(vol, "ownerdir/.grits")

		resp, _ := doReq(t, http.MethodPut,
			contentURL(baseURL, "primary", "ownerdir/.grits"), bobToken,
			[]byte("owner replaces .grits"))
		if resp.StatusCode != http.StatusOK {
			t.Errorf("replace .grits with owner: expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("read+write can write regular files", func(t *testing.T) {
		resp, _ := doReq(t, http.MethodPut,
			contentURL(baseURL, "primary", "readwritedir/regular.txt"), aliceToken,
			[]byte("regular content"))
		if resp.StatusCode != http.StatusOK {
			t.Errorf("write regular file with read+write: expected 200, got %d", resp.StatusCode)
		}
	})
}

func TestResolvePermissionOwnerBypassesGritsProtection(t *testing.T) {
	vol, authMod := setupPermTest(t, "data",
		AccessConfig{Allow: []Grant{{User: "admin", Origin: "*", Permission: PermOwner}}})

	admin := &grits.Principal{User: "admin"}
	anon := &grits.Principal{}

	perm := authMod.resolvePermissionAtRoot(vol, nil, "data/.grits/access.json", admin)
	if !CanWrite(perm) {
		t.Errorf("owner should be able to write .grits, got %q", perm)
	}

	perm = authMod.resolvePermissionAtRoot(vol, nil, "data/.grits/access.json", anon)
	if CanRead(perm) {
		t.Errorf("anonymous should NOT be able to read .grits, got %q", perm)
	}
}

func TestResolvePermissionMultiLevelMerge(t *testing.T) {
	bt := boolTrue()
	server, _ := SetupTestServer(t)
	defer func() { server.Stop() }()

	volConfig := &LocalVolumeConfig{VolumeName: "primary"}
	vol, err := NewLocalVolume(volConfig, server, false, false)
	if err != nil {
		t.Fatalf("NewLocalVolume: %v", err)
	}
	server.AddModule(vol)
	server.AddVolume(vol)
	server.Start()

	authMod, err := NewAuthModule(server, &AuthModuleConfig{CoreVhost: "http://test.local"})
	if err != nil {
		t.Fatalf("NewAuthModule: %v", err)
	}

	rootAccess := AccessConfig{Allow: []Grant{{All: bt, Origin: "*", Permission: PermRead}}}
	raw, _ := json.Marshal(rootAccess)
	ensureVolumeParentDirs(vol, ".grits")
	WriteVolumeFile(server, "primary", ".grits/access.json", raw, grits.BackendPrincipal)

	aliceAccess := AccessConfig{Allow: []Grant{{User: "alice", Origin: "*", Permission: PermReadWrite}}}
	raw2, _ := json.Marshal(aliceAccess)
	ensureVolumeParentDirs(vol, "projects/alice/.grits")
	WriteVolumeFile(server, "primary", "projects/alice/.grits/access.json", raw2, grits.BackendPrincipal)

	anon := &grits.Principal{}
	bob := &grits.Principal{User: "bob"}
	alice := &grits.Principal{User: "alice"}

	perm := authMod.resolvePermissionAtRoot(vol, nil, "projects", anon)
	if !CanRead(perm) {
		t.Errorf("anon should read 'projects', got %q", perm)
	}

	perm = authMod.resolvePermissionAtRoot(vol, nil, "projects/alice", bob)
	if !CanRead(perm) {
		t.Errorf("bob should read 'projects/alice' via root grant, got %q", perm)
	}
	if CanWrite(perm) {
		t.Errorf("bob should NOT write 'projects/alice', got %q", perm)
	}

	perm = authMod.resolvePermissionAtRoot(vol, nil, "projects/alice", alice)
	if !CanWrite(perm) {
		t.Errorf("alice should write 'projects/alice', got %q", perm)
	}
}

func TestResolvePermissionOriginConstraint(t *testing.T) {
	bt := boolTrue()
	vol, authMod := setupPermTest(t, "data",
		AccessConfig{Allow: []Grant{
			{All: bt, Origin: "https://app.example.com", Permission: PermRead},
			{User: "admin", Origin: "*", Permission: PermOwner},
		}})

	// Same origin should get read
	sameOrigin := &grits.Principal{Origin: "https://app.example.com"}
	perm := authMod.resolvePermissionAtRoot(vol, nil, "data", sameOrigin)
	if !CanRead(perm) {
		t.Errorf("expected read for same origin, got %q", perm)
	}

	// Different origin should get nothing
	diffOrigin := &grits.Principal{Origin: "https://evil.com"}
	perm = authMod.resolvePermissionAtRoot(vol, nil, "data", diffOrigin)
	if CanRead(perm) {
		t.Errorf("expected deny for different origin, got %q", perm)
	}

	// Empty origin (direct nav) passes the origin check
	emptyOrigin := &grits.Principal{}
	perm = authMod.resolvePermissionAtRoot(vol, nil, "data", emptyOrigin)
	if !CanRead(perm) {
		t.Errorf("expected read for empty origin (direct nav), got %q", perm)
	}

	// Admin with origin: "*" should get owner regardless of origin
	adminSame := &grits.Principal{User: "admin", Origin: "https://app.example.com"}
	perm = authMod.resolvePermissionAtRoot(vol, nil, "data", adminSame)
	if !CanOwn(perm) {
		t.Errorf("expected owner for admin same origin, got %q", perm)
	}

	adminDiff := &grits.Principal{User: "admin", Origin: "https://evil.com"}
	perm = authMod.resolvePermissionAtRoot(vol, nil, "data", adminDiff)
	if !CanOwn(perm) {
		t.Errorf("expected owner for admin diff origin, got %q", perm)
	}

	adminEmpty := &grits.Principal{User: "admin"}
	perm = authMod.resolvePermissionAtRoot(vol, nil, "data", adminEmpty)
	if !CanOwn(perm) {
		t.Errorf("expected owner for admin empty origin, got %q", perm)
	}

	// --- Bare hostname origin resolution ---
	// Bare hostnames without a scheme are expanded to subdomains of coreVhost.
	// setupPermTest uses coreVhost "http://test.local", so "allowed.example.com"
	// resolves to "https://allowed.example.com.test.local".
	volRel, authModRel := setupPermTest(t, "other",
		AccessConfig{Allow: []Grant{
			{All: bt, Origin: "allowed.example.com", Permission: PermRead},
		}})

	// Principal with matching resolved origin should get read
	matchOrigin := &grits.Principal{Origin: "https://allowed.example.com.test.local"}
	perm = authModRel.resolvePermissionAtRoot(volRel, nil, "other", matchOrigin)
	if !CanRead(perm) {
		t.Errorf("bare hostname: expected read for matching resolved origin, got %q", perm)
	}

	// Principal with wrong origin should get nothing
	wrongOrigin := &grits.Principal{Origin: "https://other.example.com"}
	perm = authModRel.resolvePermissionAtRoot(volRel, nil, "other", wrongOrigin)
	if CanRead(perm) {
		t.Errorf("bare hostname: expected deny for wrong origin, got %q", perm)
	}
}

////
// Lookup callback tests

func TestLookupCallbackAccessControl(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("primary"),
		WithHttpModule(1921),
		WithAuthModule(),
	)
	defer cleanup()
	server.Start()
	defer server.Stop()

	// Create dirs and permissions
	bt := boolTrue()
	for _, dir := range []string{"public", "public/.grits", "private"} {
		ensureVolumeParentDirs(server.FindVolumeByName("primary"), dir)
	}

	// public: anyone can read
	pubAccess := AccessConfig{Allow: []Grant{{All: bt, Origin: "*", Permission: PermRead}}}
	raw, _ := json.Marshal(pubAccess)
	WriteVolumeFile(server, "primary", "public/.grits/access.json", raw, grits.BackendPrincipal)

	// Write files
	WriteVolumeFile(server, "primary", "public/hello.txt", []byte("hello"), grits.BackendPrincipal)
	WriteVolumeFile(server, "primary", "private/secret.txt", []byte("secret"), grits.BackendPrincipal)

	// Public file should be readable by anon
	resp, _ := doReq(t, http.MethodGet, contentURL("http://127.0.0.1:1921", "primary", "public/hello.txt"), "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("public file: expected 200, got %d", resp.StatusCode)
	}

	// Private file should be denied
	resp, _ = doReq(t, http.MethodGet, contentURL("http://127.0.0.1:1921", "primary", "private/secret.txt"), "", nil)
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
		t.Errorf("private file: expected 403/404, got %d", resp.StatusCode)
	}
}

/////
// Link callback tests

func TestLinkCallbackWritePermission(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("primary"),
		WithHttpModule(1922),
		WithAuthModule(),
	)
	defer cleanup()
	server.Start()
	defer server.Stop()

	bt := boolTrue()
	vol := server.FindVolumeByName("primary")

	// write-allowed dir: anyone can read+write
	ensureVolumeParentDirs(vol, "writabledir/.grits")
	writeAccess := AccessConfig{Allow: []Grant{{All: bt, Origin: "*", Permission: PermReadWrite}}}
	raw, _ := json.Marshal(writeAccess)
	WriteVolumeFile(server, "primary", "writabledir/.grits/access.json", raw, grits.BackendPrincipal)

	// insert-only dir: anyone can read+insert
	ensureVolumeParentDirs(vol, "insertdir/.grits")
	insAccess := AccessConfig{Allow: []Grant{{All: bt, Origin: "*", Permission: PermReadInsert}}}
	raw2, _ := json.Marshal(insAccess)
	WriteVolumeFile(server, "primary", "insertdir/.grits/access.json", raw2, grits.BackendPrincipal)

	// deny dir: no permissions
	ensureVolumeParentDirs(vol, "denydir")

	// Write to writable dir — should succeed
	resp, _ := doReq(t, http.MethodPut,
		contentURL("http://127.0.0.1:1922", "primary", "writabledir/test.txt"), "",
		[]byte("content"))
	if resp.StatusCode != http.StatusOK {
		t.Errorf("write to writable dir: expected 200, got %d", resp.StatusCode)
	}

	// Insert new file to insert dir — should succeed (file doesn't exist yet)
	resp, _ = doReq(t, http.MethodPut,
		contentURL("http://127.0.0.1:1922", "primary", "insertdir/newfile.txt"), "",
		[]byte("new content"))
	if resp.StatusCode != http.StatusOK {
		t.Errorf("insert to insert dir: expected 200, got %d", resp.StatusCode)
	}

	// Write to denied dir — should fail
	resp, _ = doReq(t, http.MethodPut,
		contentURL("http://127.0.0.1:1922", "primary", "denydir/test.txt"), "",
		[]byte("content"))
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("write to denied dir: expected 403, got %d", resp.StatusCode)
	}

	// Insert to a path that already exists — should fail for insert-only dir
	// The file was created above, so trying again should be denied (insert only)
	resp, _ = doReq(t, http.MethodPut,
		contentURL("http://127.0.0.1:1922", "primary", "insertdir/newfile.txt"), "",
		[]byte("modified"))
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("modify in insert dir: expected 403, got %d", resp.StatusCode)
	}
}

func TestLookupCallbackOriginEnforcement(t *testing.T) {
	port := 1923
	coreVhost := fmt.Sprintf("http://127.0.0.1:%d", port)
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("primary"),
		WithHttpModule(port),
		WithAuthModuleVhost(coreVhost),
	)
	defer cleanup()
	server.Start()
	defer server.Stop()

	bt := boolTrue()
	vol := server.FindVolumeByName("primary")
	client := &http.Client{}
	fileURL := contentURL(coreVhost, "primary", "originprotected/file.txt")

	ensureVolumeParentDirs(vol, "originprotected/.grits")

	t.Run("absolute origin grant", func(t *testing.T) {
		access := AccessConfig{Allow: []Grant{{All: bt, Origin: "https://allowed.example.com", Permission: PermRead}}}
		raw, _ := json.Marshal(access)
		WriteVolumeFile(server, "primary", "originprotected/.grits/access.json", raw, grits.BackendPrincipal)
		WriteVolumeFile(server, "primary", "originprotected/file.txt", []byte("content"), grits.BackendPrincipal)

		// Matching origin
		req, _ := http.NewRequest("GET", fileURL, nil)
		req.Header.Set("Origin", "https://allowed.example.com")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("absolute match request: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("absolute match: expected 200, got %d", resp.StatusCode)
		}

		// Wrong origin
		req2, _ := http.NewRequest("GET", fileURL, nil)
		req2.Header.Set("Origin", "https://evil.com")
		resp2, _ := client.Do(req2)
		resp2.Body.Close()
		if resp2.StatusCode != http.StatusForbidden {
			t.Errorf("absolute wrong: expected 403, got %d", resp2.StatusCode)
		}

		// No origin header (direct navigation)
		req3, _ := http.NewRequest("GET", fileURL, nil)
		resp3, _ := client.Do(req3)
		resp3.Body.Close()
		if resp3.StatusCode != http.StatusOK {
			t.Errorf("absolute direct nav: expected 200, got %d", resp3.StatusCode)
		}
	})

	t.Run("core vhost origin grant", func(t *testing.T) {
		// Grant read only from the core vhost origin.
		access := AccessConfig{Allow: []Grant{{All: bt, Origin: coreVhost, Permission: PermRead}}}
		raw, _ := json.Marshal(access)
		WriteVolumeFile(server, "primary", "originprotected/.grits/access.json", raw, grits.BackendPrincipal)
		WriteVolumeFile(server, "primary", "originprotected/file.txt", []byte("content"), grits.BackendPrincipal)

		// Matching core vhost origin
		req, _ := http.NewRequest("GET", fileURL, nil)
		req.Header.Set("Origin", coreVhost)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("core vhost match request: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("core vhost match: expected 200, got %d", resp.StatusCode)
		}

		// Different origin
		req2, _ := http.NewRequest("GET", fileURL, nil)
		req2.Header.Set("Origin", "https://evil.com")
		resp2, _ := client.Do(req2)
		resp2.Body.Close()
		if resp2.StatusCode != http.StatusForbidden {
			t.Errorf("core vhost wrong: expected 403, got %d", resp2.StatusCode)
		}

		// No origin header (direct navigation)
		req3, _ := http.NewRequest("GET", fileURL, nil)
		resp3, _ := client.Do(req3)
		resp3.Body.Close()
		if resp3.StatusCode != http.StatusOK {
			t.Errorf("core vhost direct nav: expected 200, got %d", resp3.StatusCode)
		}
	})
}

////
// adduser command tests

func TestAdduserCreatesHomeDir(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("primary"),
		WithAuthModule(),
	)
	defer cleanup()
	server.Start()
	defer server.Stop()

	// Run adduser command
	resp := server.ExecuteCommand([]string{"adduser", "testuser__", "correct horse battery staple"})
	if resp.Status != 0 {
		t.Fatalf("adduser failed: %s", resp.Output)
	}

	// Verify user was added to users.jsonl
	lines, err := ReadJSONL(server, "primary", "sys/etc/users.jsonl", grits.BackendPrincipal)
	if err != nil {
		t.Fatalf("reading users.jsonl: %v", err)
	}
	found := false
	for _, line := range lines {
		var rec struct {
			Username string `json:"username"`
			PwdHash  string `json:"pwdHash"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Username == "testuser__" {
			found = true
			if rec.PwdHash == "" {
				t.Error("password hash should not be empty")
			}
			break
		}
	}
	if !found {
		t.Fatal("user testuser__ not found in users.jsonl")
	}

	// Verify home directory exists with owner permission
	cfg, err := authModReadAccessConfig(t, server, server.FindVolumeByName("primary"), "home/testuser__")
	if err != nil {
		t.Fatalf("reading home access.json: %v", err)
	}
	if cfg == nil {
		t.Fatal("home .grits/access.json not found")
	}
	if len(cfg.Allow) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(cfg.Allow))
	}
	if cfg.Allow[0].User != "testuser__" || cfg.Allow[0].Permission != PermOwner {
		t.Errorf("expected owner for testuser__, got %+v", cfg.Allow[0])
	}
}

// authModReadAccessConfig is a test helper that creates a temporary auth module
// and reads an access.json from the given volume and directory path.
func authModReadAccessConfig(t *testing.T, s *Server, vol Volume, dirPath string) (*AccessConfig, error) {
	t.Helper()
	authMod, err := NewAuthModule(s, &AuthModuleConfig{CoreVhost: "http://test.local"})
	if err != nil {
		return nil, err
	}
	return authMod.readAccessConfig(vol, dirPath)
}
