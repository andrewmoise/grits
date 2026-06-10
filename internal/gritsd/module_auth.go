package gritsd

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"grits/internal/grits"
	"log"
	"net/http"
	"strings"

	"golang.org/x/crypto/argon2"
)

type AuthModuleConfig struct{}

type AuthModule struct {
	Config *AuthModuleConfig
	Server *Server
}

const (
	usersFilePath = "sys/etc/users.jsonl"
	rootVolume    = "root"
	authCookie    = "grits-auth-user"
)

func NewAuthModule(server *Server, config *AuthModuleConfig) (*AuthModule, error) {
	m := &AuthModule{
		Config: config,
		Server: server,
	}

	server.AddModuleHook(func(module Module) {
		httpModule, ok := module.(*HTTPModule)
		if !ok {
			return
		}

		httpModule.Mux.HandleFunc("/grits/v1/auth/login",
			httpModule.requestMiddleware(m.handleLogin))
		httpModule.Mux.HandleFunc("/grits/v1/auth/logout",
			httpModule.requestMiddleware(m.handleLogout))
	})

	return m, nil
}

func (m *AuthModule) Start() error { return nil }
func (m *AuthModule) Stop() error  { return nil }

func (m *AuthModule) GetModuleName() string { return "auth" }
func (*AuthModule) GetDependencies() []*Dependency {
	return []*Dependency{
		{ModuleType: "http", Type: DependRequired},
	}
}
func (m *AuthModule) GetConfig() any { return m.Config }

func (m *AuthModule) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if !Validate("username", body.Username) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid username format"})
		return
	}

	lines, err := ReadJSONL(m.Server, rootVolume, usersFilePath, grits.BackendPrincipal)
	if err != nil {
		if errors.Is(err, grits.ErrNotExist) {
			// No users file yet — no users exist.
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
			return
		}
		log.Printf("[auth] reading users file: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	for _, line := range lines {
		var record struct {
			Username string `json:"username"`
			PwdHash  string `json:"pwdHash"`
		}
		if err := json.Unmarshal(line, &record); err != nil {
			continue
		}
		if record.Username != body.Username {
			continue
		}

		if !verifyArgon2id(body.Password, record.PwdHash) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     authCookie,
			Value:    body.Username,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
}

func (m *AuthModule) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     authCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// argon2idEncode hashes a password using argon2id and returns an encoded
// string in the standard format:
//
//	$argon2id$v=19$m=<memory>,t=<time>,p=<threads>$<base64_salt>$<base64_hash>
func argon2idEncode(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating salt: %w", err)
	}

	time := uint32(1)
	memory := uint32(64 * 1024)
	threads := uint8(4)
	keyLen := uint32(32)

	hash := argon2.IDKey([]byte(password), salt, time, memory, threads, keyLen)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		memory, time, threads, b64Salt, b64Hash), nil
}

// verifyArgon2id checks a password against an argon2id-encoded hash string.
func verifyArgon2id(password, encodedHash string) bool {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return false
	}
	if parts[1] != "argon2id" {
		return false
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != 19 {
		return false
	}

	var memory uint32
	var time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}

	keyLen := uint32(len(expectedHash))
	computedHash := argon2.IDKey([]byte(password), salt, time, memory, threads, keyLen)

	return subtle.ConstantTimeCompare(computedHash, expectedHash) == 1
}
