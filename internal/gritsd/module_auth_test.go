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
}

func TestAuthLoginWrongPassword(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("root"),
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
		WithLocalVolume("root"),
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
		WithLocalVolume("root"),
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
		WithLocalVolume("root"),
		WithHttpModule(1915),
		WithAuthModule(),
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

func TestAuthNoUsersFile(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("root"),
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
		WithLocalVolume("root"),
		WithHttpModule(port),
		WithAuthModule(),
	)
	defer cleanup()

	server.Start()
	defer server.Stop()

	// --- Setup: create directories and files via BackendPrincipal ---

	// Create directory structure and .grits directories for access control
	for _, dir := range []string{"sys/etc", "sites", "sites/.grits", "home", "home/user", "home/user/.grits"} {
		if err := ensureVolumeParentDirs(server.FindVolumeByName("root"), dir); err != nil {
			t.Fatalf("creating dir %q: %v", dir, err)
		}
	}

	// Grant everyone (including anonymous) read access to /sites
	anonTrue := true
	sitesAccess := AccessConfig{
		Allow: []Grant{
			{All: &anonTrue, Referer: "*", Permission: PermRead},
		},
	}
	sitesAccessData, _ := json.Marshal(sitesAccess)
	if err := WriteVolumeFile(server, "root", "sites/.grits/access.json", sitesAccessData, grits.BackendPrincipal); err != nil {
		t.Fatalf("writing sites/.grits/access.json: %v", err)
	}

	// Grant user read+write access to /home/user
	homeAccess := AccessConfig{
		Allow: []Grant{
			{User: "user", Referer: "*", Permission: PermReadWrite},
		},
	}
	homeAccessData, _ := json.Marshal(homeAccess)
	if err := WriteVolumeFile(server, "root", "home/user/.grits/access.json", homeAccessData, grits.BackendPrincipal); err != nil {
		t.Fatalf("writing home/user/.grits/access.json: %v", err)
	}

	// Write a test file at /sites/test.txt
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

	// --- Test 1: Unauthenticated read of path with public grant ---
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
		resp, _ := doReq(t, http.MethodGet, contentURL(baseURL, "root", "sites/test.txt"), authToken, nil)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	// --- Test 5: Authenticated read of non-whitelisted path ---
	t.Run("authed read denied", func(t *testing.T) {
		resp, body := doReq(t, http.MethodGet, contentURL(baseURL, "root", "sys/etc/users.jsonl"), authToken, nil)
		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 403 or 404, got %d: %s", resp.StatusCode, string(body))
		}
	})

	// --- Test 6: Authenticated write to whitelisted path ---
	t.Run("authed write allowed", func(t *testing.T) {
		resp, _ := doReq(t, http.MethodPut, contentURL(baseURL, "root", "home/user/foo.txt"), authToken,
			[]byte("user content"))
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	// --- Test 7: Authenticated write to non-whitelisted path ---
	t.Run("authed write denied", func(t *testing.T) {
		resp, body := doReq(t, http.MethodPut, contentURL(baseURL, "root", "sites/bar.txt"), authToken,
			[]byte("should be denied"))
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403, got %d: %s", resp.StatusCode, string(body))
		}
	})

	// --- Test 8: Authenticated delete from whitelisted path ---
	t.Run("authed delete allowed", func(t *testing.T) {
		resp, _ := doReq(t, http.MethodDelete, contentURL(baseURL, "root", "home/user/foo.txt"), authToken, nil)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	// --- Test 9: Authenticated delete from non-whitelisted path ---
	t.Run("authed delete denied", func(t *testing.T) {
		resp, body := doReq(t, http.MethodDelete, contentURL(baseURL, "root", "sites/test.txt"), authToken, nil)
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
		resp, body := doReq(t, http.MethodPut, contentURL(baseURL, "root", "home/user/foo.txt"), "", nil)
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

func TestResolveReferer(t *testing.T) {
	tests := []struct {
		name      string
		referer   string
		coreVhost string
		want      string
	}{
		{"star passthrough", "*", "http://test.local", "*"},
		{"empty passthrough", "", "http://test.local", ""},
		{"absolute http passthrough", "http://example.com/path", "http://test.local", "http://example.com/path"},
		{"absolute https passthrough", "https://app.example.com/foo", "http://test.local", "https://app.example.com/foo"},
		{"relative resolved", "/grits/v1/content/root/lib/gimbal/", "https://gimbal.example.com", "https://gimbal.example.com/grits/v1/content/root/lib/gimbal/"},
		{"relative with vhost trailing slash", "/grits/thing", "https://g.example.com/", "https://g.example.com/grits/thing"},
		{"relative no leading slash", "grits/thing", "https://g.example.com", "https://g.example.com/grits/thing"},
	}
	for _, tc := range tests {
		m := &AuthModule{Config: &AuthModuleConfig{CoreVhost: tc.coreVhost}}
		got := m.resolveReferer(tc.referer)
		if got != tc.want {
			t.Errorf("%s: resolveReferer(%q) with coreVhost=%q = %q, want %q",
				tc.name, tc.referer, tc.coreVhost, got, tc.want)
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
		{"specific user matches", Grant{User: "alice", Referer: "*"}, auth, true},
		{"specific user no match", Grant{User: "alice", Referer: "*"}, anon, false},
		{"specific wrong user", Grant{User: "bob", Referer: "*"}, auth, false},
		{"auth matches authenticated", Grant{Auth: bt, Referer: "*"}, auth, true},
		{"auth no match anonymous", Grant{Auth: bt, Referer: "*"}, anon, false},
		{"anon matches anonymous", Grant{All: bt, Referer: "*"}, anon, true},
		{"anon matches auth", Grant{All: bt, Referer: "*"}, auth, true},
		{"user overrides auth", Grant{User: "alice", Auth: bt, Referer: "*"}, anon, false},
		{"user overrides anon", Grant{User: "alice", All: bt, Referer: "*"}, anon, false},
		{"empty grant no match", Grant{}, auth, false},
		{"referer alone matches specific", Grant{Referer: "https://app.example.com"}, &grits.Principal{Referer: "https://app.example.com"}, true},
		{"referer alone rejects different", Grant{Referer: "https://app.example.com"}, &grits.Principal{Referer: "https://evil.com"}, false},
		{"referer alone with empty principal (direct nav) passes", Grant{Referer: "https://app.example.com"}, &grits.Principal{}, true},
		{"referer star matches any", Grant{Referer: "*", All: bt}, &grits.Principal{Referer: "https://anything"}, true},
		{"referer star matches empty", Grant{Referer: "*", All: bt}, &grits.Principal{}, true},
		{"referer narrows user", Grant{User: "alice", Referer: "https://app.example.com"}, &grits.Principal{User: "alice", Referer: "https://app.example.com"}, true},
		{"referer rejects user from wrong", Grant{User: "alice", Referer: "https://app.example.com"}, &grits.Principal{User: "alice", Referer: "https://evil.com"}, false},
		{"no referer matches any referer", Grant{User: "alice", Referer: "*"}, &grits.Principal{User: "alice", Referer: "https://anything"}, true},
		{"referer star with user", Grant{User: "bob", Referer: "*"}, &grits.Principal{User: "bob", Referer: "https://anything"}, true},
		{"empty referer never matches", Grant{User: "alice"}, &grits.Principal{User: "alice"}, false},
		// Direct navigation (empty principal referer) always passes the
		// referer check even when the grant has a specific referer.
		{"direct nav passes referer check", Grant{Referer: "https://app.example.com"}, &grits.Principal{}, true},
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

	volConfig := &LocalVolumeConfig{VolumeName: "root"}
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
			{User: "alice", Referer: "*", Permission: PermOwner},
			{All: bt, Referer: "*", Permission: PermRead},
		},
	}
	raw, _ := json.Marshal(access)
	WriteVolumeFile(server, "root", "data/.grits/access.json", raw, grits.BackendPrincipal)

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
		Allow: []Grant{{User: "glenda", Referer: "*", Permission: PermOwner}},
	}
	raw2, _ := json.Marshal(rootAccess)
	WriteVolumeFile(server, "root", ".grits/access.json", raw2, grits.BackendPrincipal)

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

// setupPermTest creates a server with a "root" volume and an auth module,
// creates a .grits/access.json at the given path, and returns the volume and auth module.
func setupPermTest(t *testing.T, accessPath string, access AccessConfig) (Volume, *AuthModule) {
	t.Helper()
	server, _ := SetupTestServer(t)

	volConfig := &LocalVolumeConfig{VolumeName: "root"}
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
		if err := WriteVolumeFile(server, "root", gritsDir+"/access.json", raw, grits.BackendPrincipal); err != nil {
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
		perm := authMod.resolvePermission(vol, "anything", princ)
		if perm != "" {
			t.Errorf("expected deny, got %q for %+v", perm, princ)
		}
	}
}

func TestResolvePermissionReadInherited(t *testing.T) {
	bt := boolTrue()
	vol, authMod := setupPermTest(t, "data",
		AccessConfig{Allow: []Grant{{All: bt, Referer: "*", Permission: PermRead}}})

	anon := &grits.Principal{}

	for _, path := range []string{"data", "data/sub", "data/sub/deep"} {
		perm := authMod.resolvePermission(vol, path, anon)
		if !CanRead(perm) {
			t.Errorf("expected read for %q, got %q", path, perm)
		}
	}

	perm := authMod.resolvePermission(vol, "other", anon)
	if CanRead(perm) {
		t.Errorf("expected deny for 'other', got %q", perm)
	}
}

func TestResolvePermissionWriteInherited(t *testing.T) {
	bt := boolTrue()
	vol, authMod := setupPermTest(t, "work",
		AccessConfig{Allow: []Grant{{All: bt, Referer: "*", Permission: PermReadWrite}}})

	anon := &grits.Principal{}

	for _, path := range []string{"work", "work/sub"} {
		perm := authMod.resolvePermission(vol, path, anon)
		if !CanWrite(perm) {
			t.Errorf("expected write for %q, got %q", path, perm)
		}
	}
}

func TestResolvePermissionInsertNotInherited(t *testing.T) {
	bt := boolTrue()
	vol, authMod := setupPermTest(t, "parent",
		AccessConfig{Allow: []Grant{{All: bt, Referer: "*", Permission: PermInsert}}})

	anon := &grits.Principal{}

	perm := authMod.resolvePermission(vol, "parent", anon)
	if !CanInsert(perm) {
		t.Errorf("expected insert at 'parent', got %q", perm)
	}

	perm = authMod.resolvePermission(vol, "parent/child", anon)
	if CanInsert(perm) {
		t.Errorf("insert should NOT be inherited, got %q", perm)
	}
	vol2, authMod2 := setupPermTest(t, "base",
		AccessConfig{Allow: []Grant{{All: bt, Referer: "*", Permission: PermReadInsert}}})
	perm = authMod2.resolvePermission(vol2, "base/child", anon)
	if !CanRead(perm) {
		t.Errorf("expected read at 'base/child', got %q", perm)
	}
	if CanInsert(perm) {
		t.Errorf("insert should NOT be inherited, got %q", perm)
	}
}

func TestResolvePermissionGritsProtection(t *testing.T) {
	bt := boolTrue()
	// Grant read+write at "data". Since write at an ancestor grants effective
	// ownership over descendants (you can replace the entire subtree), the
	// inherited permission at data/.grits is owner — bypassing .grits protection.
	vol, authMod := setupPermTest(t, "data",
		AccessConfig{Allow: []Grant{{All: bt, Referer: "*", Permission: PermReadWrite}}})

	anon := &grits.Principal{}

	perm := authMod.resolvePermission(vol, "data/.grits", anon)
	if !CanRead(perm) {
		t.Errorf("expected read for .grits, got %q", perm)
	}
	if !CanWrite(perm) {
		t.Errorf("expected write for .grits (read+write inherited as owner), got %q", perm)
	}

	perm = authMod.resolvePermission(vol, "data/.grits/access.json", anon)
	if !CanRead(perm) {
		t.Errorf("expected read for .grits/file, got %q", perm)
	}
	if !CanWrite(perm) {
		t.Errorf("expected write for .grits/file (read+write inherited as owner), got %q", perm)
	}
}

func TestResolvePermissionOwnerBypassesGritsProtection(t *testing.T) {
	vol, authMod := setupPermTest(t, "data",
		AccessConfig{Allow: []Grant{{User: "admin", Referer: "*", Permission: PermOwner}}})

	admin := &grits.Principal{User: "admin"}
	anon := &grits.Principal{}

	perm := authMod.resolvePermission(vol, "data/.grits/access.json", admin)
	if !CanWrite(perm) {
		t.Errorf("owner should be able to write .grits, got %q", perm)
	}

	perm = authMod.resolvePermission(vol, "data/.grits/access.json", anon)
	if CanRead(perm) {
		t.Errorf("anonymous should NOT be able to read .grits, got %q", perm)
	}
}

func TestResolvePermissionMultiLevelMerge(t *testing.T) {
	bt := boolTrue()
	server, _ := SetupTestServer(t)
	defer func() { server.Stop() }()

	volConfig := &LocalVolumeConfig{VolumeName: "root"}
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

	rootAccess := AccessConfig{Allow: []Grant{{All: bt, Referer: "*", Permission: PermRead}}}
	raw, _ := json.Marshal(rootAccess)
	ensureVolumeParentDirs(vol, ".grits")
	WriteVolumeFile(server, "root", ".grits/access.json", raw, grits.BackendPrincipal)

	aliceAccess := AccessConfig{Allow: []Grant{{User: "alice", Referer: "*", Permission: PermReadWrite}}}
	raw2, _ := json.Marshal(aliceAccess)
	ensureVolumeParentDirs(vol, "projects/alice/.grits")
	WriteVolumeFile(server, "root", "projects/alice/.grits/access.json", raw2, grits.BackendPrincipal)

	anon := &grits.Principal{}
	bob := &grits.Principal{User: "bob"}
	alice := &grits.Principal{User: "alice"}

	perm := authMod.resolvePermission(vol, "projects", anon)
	if !CanRead(perm) {
		t.Errorf("anon should read 'projects', got %q", perm)
	}

	perm = authMod.resolvePermission(vol, "projects/alice", bob)
	if !CanRead(perm) {
		t.Errorf("bob should read 'projects/alice' via root grant, got %q", perm)
	}
	if CanWrite(perm) {
		t.Errorf("bob should NOT write 'projects/alice', got %q", perm)
	}

	perm = authMod.resolvePermission(vol, "projects/alice", alice)
	if !CanWrite(perm) {
		t.Errorf("alice should write 'projects/alice', got %q", perm)
	}
}

func TestResolvePermissionRefererConstraint(t *testing.T) {
	bt := boolTrue()
	vol, authMod := setupPermTest(t, "data",
		AccessConfig{Allow: []Grant{
			{All: bt, Referer: "https://app.example.com", Permission: PermRead},
			{User: "admin", Referer: "*", Permission: PermOwner},
		}})

	// Same referer should get read
	sameReferer := &grits.Principal{Referer: "https://app.example.com"}
	perm := authMod.resolvePermission(vol, "data", sameReferer)
	if !CanRead(perm) {
		t.Errorf("expected read for same referer, got %q", perm)
	}

	// Different referer should get nothing
	diffReferer := &grits.Principal{Referer: "https://evil.com"}
	perm = authMod.resolvePermission(vol, "data", diffReferer)
	if CanRead(perm) {
		t.Errorf("expected deny for different referer, got %q", perm)
	}

	// Empty referer (direct nav) passes the referer check
	emptyReferer := &grits.Principal{}
	perm = authMod.resolvePermission(vol, "data", emptyReferer)
	if !CanRead(perm) {
		t.Errorf("expected read for empty referer (direct nav), got %q", perm)
	}

	// Admin with referer: "*" should get owner regardless of referer
	adminSame := &grits.Principal{User: "admin", Referer: "https://app.example.com"}
	perm = authMod.resolvePermission(vol, "data", adminSame)
	if !CanOwn(perm) {
		t.Errorf("expected owner for admin same referer, got %q", perm)
	}

	adminDiff := &grits.Principal{User: "admin", Referer: "https://evil.com"}
	perm = authMod.resolvePermission(vol, "data", adminDiff)
	if !CanOwn(perm) {
		t.Errorf("expected owner for admin diff referer, got %q", perm)
	}

	adminEmpty := &grits.Principal{User: "admin"}
	perm = authMod.resolvePermission(vol, "data", adminEmpty)
	if !CanOwn(perm) {
		t.Errorf("expected owner for admin empty referer, got %q", perm)
	}

	// --- Relative referer resolution ---
	// Grants with relative referers should be resolved via coreVhost.
	// setupPermTest uses coreVhost "http://test.local", so a grant with
	// referer "/grits/foo" resolves to "http://test.local/grits/foo".
	volRel, authModRel := setupPermTest(t, "other",
		AccessConfig{Allow: []Grant{
			{All: bt, Referer: "/grits/foo", Permission: PermRead},
		}})

	// Principal with matching resolved referer should get read
	matchReferer := &grits.Principal{Referer: "http://test.local/grits/foo"}
	perm = authModRel.resolvePermission(volRel, "other", matchReferer)
	if !CanRead(perm) {
		t.Errorf("relative referer: expected read for matching resolved referer, got %q", perm)
	}

	// Principal with wrong referer should get nothing
	wrongReferer := &grits.Principal{Referer: "http://test.local/other"}
	perm = authModRel.resolvePermission(volRel, "other", wrongReferer)
	if CanRead(perm) {
		t.Errorf("relative referer: expected deny for wrong referer, got %q", perm)
	}

	// Principal with non-matching root host should get nothing
	foreignReferer := &grits.Principal{Referer: "https://evil.com/grits/foo"}
	perm = authModRel.resolvePermission(volRel, "other", foreignReferer)
	if CanRead(perm) {
		t.Errorf("relative referer: expected deny for foreign host, got %q", perm)
	}
}

////
// Lookup callback tests

func TestLookupCallbackAccessControl(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("root"),
		WithHttpModule(1921),
		WithAuthModule(),
	)
	defer cleanup()
	server.Start()
	defer server.Stop()

	// Create dirs and permissions
	bt := boolTrue()
	for _, dir := range []string{"public", "public/.grits", "private"} {
		ensureVolumeParentDirs(server.FindVolumeByName("root"), dir)
	}

	// public: anyone can read
	pubAccess := AccessConfig{Allow: []Grant{{All: bt, Referer: "*", Permission: PermRead}}}
	raw, _ := json.Marshal(pubAccess)
	WriteVolumeFile(server, "root", "public/.grits/access.json", raw, grits.BackendPrincipal)

	// Write files
	WriteVolumeFile(server, "root", "public/hello.txt", []byte("hello"), grits.BackendPrincipal)
	WriteVolumeFile(server, "root", "private/secret.txt", []byte("secret"), grits.BackendPrincipal)

	// Public file should be readable by anon
	resp, _ := doReq(t, http.MethodGet, contentURL("http://127.0.0.1:1921", "root", "public/hello.txt"), "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("public file: expected 200, got %d", resp.StatusCode)
	}

	// Private file should be denied
	resp, _ = doReq(t, http.MethodGet, contentURL("http://127.0.0.1:1921", "root", "private/secret.txt"), "", nil)
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
		t.Errorf("private file: expected 403/404, got %d", resp.StatusCode)
	}
}

/////
// Link callback tests

func TestLinkCallbackWritePermission(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("root"),
		WithHttpModule(1922),
		WithAuthModule(),
	)
	defer cleanup()
	server.Start()
	defer server.Stop()

	bt := boolTrue()
	vol := server.FindVolumeByName("root")

	// write-allowed dir: anyone can read+write
	ensureVolumeParentDirs(vol, "writabledir/.grits")
	writeAccess := AccessConfig{Allow: []Grant{{All: bt, Referer: "*", Permission: PermReadWrite}}}
	raw, _ := json.Marshal(writeAccess)
	WriteVolumeFile(server, "root", "writabledir/.grits/access.json", raw, grits.BackendPrincipal)

	// insert-only dir: anyone can read+insert
	ensureVolumeParentDirs(vol, "insertdir/.grits")
	insAccess := AccessConfig{Allow: []Grant{{All: bt, Referer: "*", Permission: PermReadInsert}}}
	raw2, _ := json.Marshal(insAccess)
	WriteVolumeFile(server, "root", "insertdir/.grits/access.json", raw2, grits.BackendPrincipal)

	// deny dir: no permissions
	ensureVolumeParentDirs(vol, "denydir")

	// Write to writable dir — should succeed
	resp, _ := doReq(t, http.MethodPut,
		contentURL("http://127.0.0.1:1922", "root", "writabledir/test.txt"), "",
		[]byte("content"))
	if resp.StatusCode != http.StatusOK {
		t.Errorf("write to writable dir: expected 200, got %d", resp.StatusCode)
	}

	// Insert new file to insert dir — should succeed (file doesn't exist yet)
	resp, _ = doReq(t, http.MethodPut,
		contentURL("http://127.0.0.1:1922", "root", "insertdir/newfile.txt"), "",
		[]byte("new content"))
	if resp.StatusCode != http.StatusOK {
		t.Errorf("insert to insert dir: expected 200, got %d", resp.StatusCode)
	}

	// Write to denied dir — should fail
	resp, _ = doReq(t, http.MethodPut,
		contentURL("http://127.0.0.1:1922", "root", "denydir/test.txt"), "",
		[]byte("content"))
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("write to denied dir: expected 403, got %d", resp.StatusCode)
	}

	// Insert to a path that already exists — should fail for insert-only dir
	// The file was created above, so trying again should be denied (insert only)
	resp, _ = doReq(t, http.MethodPut,
		contentURL("http://127.0.0.1:1922", "root", "insertdir/newfile.txt"), "",
		[]byte("modified"))
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("modify in insert dir: expected 403, got %d", resp.StatusCode)
	}
}

func TestLookupCallbackRefererEnforcement(t *testing.T) {
	port := 1923
	coreVhost := fmt.Sprintf("http://127.0.0.1:%d", port)
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("root"),
		WithHttpModule(port),
		WithAuthModuleVhost(coreVhost),
	)
	defer cleanup()
	server.Start()
	defer server.Stop()

	bt := boolTrue()
	vol := server.FindVolumeByName("root")
	client := &http.Client{}
	fileURL := contentURL(coreVhost, "root", "refererprotected/file.txt")

	ensureVolumeParentDirs(vol, "refererprotected/.grits")

	t.Run("absolute referer grant", func(t *testing.T) {
		access := AccessConfig{Allow: []Grant{{All: bt, Referer: "https://allowed.example.com", Permission: PermRead}}}
		raw, _ := json.Marshal(access)
		WriteVolumeFile(server, "root", "refererprotected/.grits/access.json", raw, grits.BackendPrincipal)
		WriteVolumeFile(server, "root", "refererprotected/file.txt", []byte("content"), grits.BackendPrincipal)

		// Matching referer
		req, _ := http.NewRequest("GET", fileURL, nil)
		req.Header.Set("Referer", "https://allowed.example.com")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("absolute match request: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("absolute match: expected 200, got %d", resp.StatusCode)
		}

		// Wrong referer
		req2, _ := http.NewRequest("GET", fileURL, nil)
		req2.Header.Set("Referer", "https://evil.com")
		resp2, _ := client.Do(req2)
		resp2.Body.Close()
		if resp2.StatusCode != http.StatusForbidden {
			t.Errorf("absolute wrong: expected 403, got %d", resp2.StatusCode)
		}

		// No referer header (direct navigation)
		req3, _ := http.NewRequest("GET", fileURL, nil)
		resp3, _ := client.Do(req3)
		resp3.Body.Close()
		if resp3.StatusCode != http.StatusOK {
			t.Errorf("absolute direct nav: expected 200, got %d", resp3.StatusCode)
		}

		// Origin without Referer — always rejected
		req4, _ := http.NewRequest("GET", fileURL, nil)
		req4.Header.Set("Origin", "https://allowed.example.com")
		resp4, _ := client.Do(req4)
		resp4.Body.Close()
		if resp4.StatusCode != http.StatusForbidden {
			t.Errorf("absolute origin-only: expected 403, got %d", resp4.StatusCode)
		}
	})

	t.Run("relative referer grant", func(t *testing.T) {
		// Grant read only from the core vhost's gimbal app path.
		// Resolves to coreVhost + "/grits/v1/content/root/lib/gimbal/"
		access := AccessConfig{Allow: []Grant{{All: bt, Referer: "/grits/v1/content/root/lib/gimbal/", Permission: PermRead}}}
		raw, _ := json.Marshal(access)
		WriteVolumeFile(server, "root", "refererprotected/.grits/access.json", raw, grits.BackendPrincipal)
		WriteVolumeFile(server, "root", "refererprotected/file.txt", []byte("content"), grits.BackendPrincipal)

		// Matching resolved referer
		matchReferer := coreVhost + "/grits/v1/content/root/lib/gimbal/"
		req, _ := http.NewRequest("GET", fileURL, nil)
		req.Header.Set("Referer", matchReferer)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("relative match request: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("relative match: expected 200, got %d", resp.StatusCode)
		}

		// Wrong path on same host
		req2, _ := http.NewRequest("GET", fileURL, nil)
		req2.Header.Set("Referer", coreVhost+"/some/other/path")
		resp2, _ := client.Do(req2)
		resp2.Body.Close()
		if resp2.StatusCode != http.StatusForbidden {
			t.Errorf("relative wrong path: expected 403, got %d", resp2.StatusCode)
		}

		// Different host entirely
		req3, _ := http.NewRequest("GET", fileURL, nil)
		req3.Header.Set("Referer", "https://evil.com/grits/v1/content/root/lib/gimbal/")
		resp3, _ := client.Do(req3)
		resp3.Body.Close()
		if resp3.StatusCode != http.StatusForbidden {
			t.Errorf("relative foreign host: expected 403, got %d", resp3.StatusCode)
		}

		// No referer header (direct navigation)
		req4, _ := http.NewRequest("GET", fileURL, nil)
		resp4, _ := client.Do(req4)
		resp4.Body.Close()
		if resp4.StatusCode != http.StatusOK {
			t.Errorf("relative direct nav: expected 200, got %d", resp4.StatusCode)
		}

		// Origin without Referer — always rejected
		req5, _ := http.NewRequest("GET", fileURL, nil)
		req5.Header.Set("Origin", matchReferer)
		resp5, _ := client.Do(req5)
		resp5.Body.Close()
		if resp5.StatusCode != http.StatusForbidden {
			t.Errorf("relative origin-only: expected 403, got %d", resp5.StatusCode)
		}
	})
}

////
// adduser command tests

func TestAdduserCreatesHomeDir(t *testing.T) {
	server, cleanup := SetupTestServer(t,
		WithLocalVolume("root"),
	)
	defer cleanup()
	server.Start()
	defer server.Stop()

	// Run adduser command
	resp := server.ExecuteCommand([]string{"adduser", "testuser__", "testpassword"})
	if resp.Status != 0 {
		t.Fatalf("adduser failed: %s", resp.Output)
	}

	// Verify user was added to users.jsonl
	lines, err := ReadJSONL(server, "root", "sys/etc/users.jsonl", grits.BackendPrincipal)
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
	cfg, err := authModReadAccessConfig(t, server, server.FindVolumeByName("root"), "home/testuser__")
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
