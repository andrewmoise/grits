package gritsd

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"grits/internal/grits"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

type AuthModuleConfig struct {
	// CoreVhost is the URL of the main admin vhost (e.g. "https://gimbal.example.com").
	// Grants with origin "/" get resolved to this origin.
	CoreVhost string `json:"coreVhost"`

	// SessionMaxAge is the sliding expiry window for session tokens.
	// Defaults to 30 minutes. After this long without any API request,
	// the user must log in again.
	SessionMaxAge time.Duration `json:"sessionMaxAge,omitempty"`
}

type AuthModule struct {
	Config        *AuthModuleConfig
	Server        *Server
	authSecret    []byte
	sessionMaxAge time.Duration
}

const (
	usersFilePath        = "sys/etc/users.jsonl"
	rootVolume           = "root"
	authCookie           = "grits-auth-user"
	defaultSessionMaxAge = 30 * time.Minute
)

func NewAuthModule(server *Server, config *AuthModuleConfig) (*AuthModule, error) {
	if config.CoreVhost == "" {
		return nil, fmt.Errorf("auth: coreVhost is required")
	}

	// Load or generate the secret key for HMAC session tokens.
	secret, err := loadOrGenerateSecret(server)
	if err != nil {
		return nil, fmt.Errorf("auth: %v", err)
	}

	maxAge := config.SessionMaxAge
	if maxAge <= 0 {
		maxAge = defaultSessionMaxAge
	}

	m := &AuthModule{
		Config:        config,
		Server:        server,
		authSecret:    secret,
		sessionMaxAge: maxAge,
	}

	// Hook into HTTP module to register auth endpoints and store a reference
	// for cookie verification in the request middleware.
	server.AddModuleHook(func(module Module) {
		httpModule, ok := module.(*HTTPModule)
		if !ok {
			return
		}

		httpModule.SetAuthModule(m)

		httpModule.Mux.HandleFunc("/grits/v1/auth/login",
			httpModule.requestMiddleware(m.handleLogin))
		httpModule.Mux.HandleFunc("/grits/v1/auth/logout",
			httpModule.requestMiddleware(m.handleLogout))
	})

	return m, nil
}

// loadOrGenerateSecret reads the HMAC secret from var/auth-secret, creating
// it with 32 random bytes if the file doesn't exist.
func loadOrGenerateSecret(srv *Server) ([]byte, error) {
	path := filepath.Join(srv.Config.ServerDir, "var", "auth-secret")
	data, err := os.ReadFile(path)
	if err == nil {
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading auth-secret: %w", err)
	}
	// Generate a new secret.
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generating auth-secret: %w", err)
	}
	if err := os.WriteFile(path, secret, 0600); err != nil {
		return nil, fmt.Errorf("writing auth-secret: %w", err)
	}
	return secret, nil
}

func (m *AuthModule) Start() error { return nil }
func (m *AuthModule) Stop() error  { return nil }

func (m *AuthModule) GetModuleName() string { return "auth" }
func (*AuthModule) GetDependencies() []*Dependency {
	return []*Dependency{
		{ModuleType: "http", Type: DependOptional},
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

		token, err := m.hmacToken(body.Username)
		if err != nil {
			log.Printf("[auth] creating session token: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "token": token})
		return
	}

	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
}

func (m *AuthModule) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// Argon2idEncode hashes a password using argon2id and returns an encoded
// string in the standard format:
//
//	$argon2id$v=19$m=<memory>,t=<time>,p=<threads>$<base64_salt>$<base64_hash>
func Argon2idEncode(password string) (string, error) {
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

/////
// HMAC session token helpers

// hmacToken creates a signed session token for the given username.
// Format: base64(timestamp:username:hmac)
func (m *AuthModule) hmacToken(username string) (string, error) {
	now := time.Now().Unix()
	plain := fmt.Sprintf("%d:%s", now, username)
	mac := hmac.New(sha256.New, m.authSecret)
	mac.Write([]byte(plain))
	sig := mac.Sum(nil)
	token := fmt.Sprintf("%s:%s", plain, base64.RawStdEncoding.EncodeToString(sig))
	return base64.RawStdEncoding.EncodeToString([]byte(token)), nil
}

// verifyHMACToken parses and validates a session token. Returns the username
// if valid, or "" if the token is stale, forged, or malformed.
func (m *AuthModule) verifyHMACToken(token string) string {
	if m == nil || len(m.authSecret) == 0 {
		return ""
	}
	raw, err := base64.RawStdEncoding.DecodeString(token)
	if err != nil {
		return ""
	}
	parts := strings.SplitN(string(raw), ":", 3)
	if len(parts) != 3 {
		return ""
	}

	ts, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return ""
	}
	username := parts[1]
	providedSig, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return ""
	}

	// Recompute HMAC.
	plain := fmt.Sprintf("%d:%s", ts, username)
	mac := hmac.New(sha256.New, m.authSecret)
	mac.Write([]byte(plain))
	expectedSig := mac.Sum(nil)

	if subtle.ConstantTimeCompare(providedSig, expectedSig) != 1 {
		return ""
	}

	// Check expiry.
	if time.Now().Unix()-ts > int64(m.sessionMaxAge.Seconds()) {
		return ""
	}

	return username
}

/////
// Permission types
// Permissions are stored in .grits/access.json files within the volume
// and are resolved by walking up the tree from the target path.

type Permission string

const (
	PermRead       Permission = "read"
	PermInsert     Permission = "insert"
	PermReadInsert Permission = "read+insert"
	PermReadWrite  Permission = "read+write"
	PermOwner      Permission = "owner"
)

func CanRead(p Permission) bool {
	return p == PermRead || p == PermReadInsert || p == PermReadWrite || p == PermOwner
}

func CanInsert(p Permission) bool {
	return p == PermInsert || p == PermReadInsert || p == PermReadWrite || p == PermOwner
}

func CanWrite(p Permission) bool {
	return p == PermReadWrite || p == PermOwner
}

func CanOwn(p Permission) bool {
	return p == PermOwner
}

// mergePerm returns the most permissive combination of two permissions.
func mergePerm(a, b Permission) Permission {
	read := CanRead(a) || CanRead(b)
	insert := CanInsert(a) || CanInsert(b)
	write := CanWrite(a) || CanWrite(b)
	own := CanOwn(a) || CanOwn(b)

	switch {
	case own:
		return PermOwner
	case write:
		return PermReadWrite
	case read && insert:
		return PermReadInsert
	case insert:
		return PermInsert
	case read:
		return PermRead
	default:
		return ""
	}
}

// Grant represents a single permission grant for a user/origin.
//
// The three matching tiers are:
//   - User:    matches a specific authenticated username
//   - Auth:    matches any authenticated user (non-empty User)
//   - All:    matches any principal (including unauthenticated)
//
// Origin is a cross-cutting constraint on top of user/auth/all:
//
//	""  → grant is inert (never matches)
//	"*" → any origin (no constraint)
//	"/" → resolves to coreVhost origin
//	otherwise → must match principal.Origin exactly (bare hostnames
//	            like "gimbal.example.com" are resolved to "https://...")
//
// When multiple grants match, the most permissive permission wins.
type Grant struct {
	User       string     `json:"user,omitempty"` // specific username
	Auth       *bool      `json:"auth,omitempty"` // any authenticated user
	All        *bool      `json:"all,omitempty"`  // any principal including unauthenticated
	Origin     string     `json:"origin"`          // ""=inert; "*"=any; URL or bare hostname
	Permission Permission `json:"permission"`
}

// AccessConfig is the schema for .grits/access.json files.
type AccessConfig struct {
	Allow []Grant `json:"allow"`
}

// parentPath returns the parent directory of a path, or "" for root-level paths.
func parentPath(path string) string {
	path = strings.TrimRight(path, "/")
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[:idx]
	}
	return ""
}

// resolveOrigin normalizes a grant's origin string.
//
//	""              → inert (pass through)
//	"*"             → any origin (pass through)
//	"/"             → coreVhost's origin
//	http(s)://…     → absolute URL, use as-is
//	anything else   → bare hostname: prepend "https://"
func (m *AuthModule) resolveOrigin(origin string) string {
	switch {
	case origin == "" || origin == "*":
		return origin
	case origin == "/":
		return strings.TrimRight(m.Config.CoreVhost, "/")
	case strings.HasPrefix(origin, "http://") || strings.HasPrefix(origin, "https://"):
		return origin
	default:
		return "https://" + origin
	}
}

// readAccessConfig reads and parses .grits/access.json from the given directory
// path within the given volume. Returns nil without error if the file doesn't exist.
func (m *AuthModule) readAccessConfig(vol Volume, dirPath string) (*AccessConfig, error) {
	accessPath := dirPath + "/.grits/access.json"
	if dirPath == "" {
		accessPath = ".grits/access.json"
	}

	// Use BackendPrincipal to avoid recursion into this callback.
	data, err := ReadVolumeFile(m.Server, vol.GetVolumeName(), accessPath, grits.BackendPrincipal)
	if err != nil {
		if errors.Is(err, grits.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %q: %w", accessPath, err)
	}

	var cfg AccessConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %q: %w", accessPath, err)
	}

	// Resolve shorthand origins (bare hostnames, "/" → coreVhost).
	for i := range cfg.Allow {
		cfg.Allow[i].Origin = m.resolveOrigin(cfg.Allow[i].Origin)
	}

	return &cfg, nil
}

// readAccessConfigFromRoot reads .grits/access.json using LookupFromRoot against
// a pinned root node, avoiding re-entrant Lookup calls and their TOCTOU race.
func (m *AuthModule) readAccessConfigFromRoot(vol Volume, rootNode grits.FileNode, dirPath string) (*AccessConfig, error) {
	accessPath := dirPath + "/.grits/access.json"
	if dirPath == "" {
		accessPath = ".grits/access.json"
	}

	resp, err := vol.lookupFromRoot(accessPath, rootNode)
	if err != nil {
		vol.FatalIfBlobMissing(err)
		return nil, fmt.Errorf("reading %q: %w", accessPath, err)
	}
	leaf := resp.Leaf()
	if leaf == nil || leaf.Error == "not_found" {
		return nil, nil
	}
	if leaf.Error != "" {
		return nil, nil
	}

	node, err := vol.GetFileNode(leaf.Addr)
	if err != nil {
		vol.FatalIfBlobMissing(err)
		return nil, fmt.Errorf("loading node for %q: %w", accessPath, err)
	}
	defer node.Release()

	blob, err := node.ExportedBlob()
	if err != nil {
		vol.FatalIfBlobMissing(err)
		return nil, fmt.Errorf("getting blob for %q: %w", accessPath, err)
	}

	data, err := blob.Read(0, blob.GetSize())
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", accessPath, err)
	}

	var cfg AccessConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %q: %w", accessPath, err)
	}

	for i := range cfg.Allow {
		cfg.Allow[i].Origin = m.resolveOrigin(cfg.Allow[i].Origin)
	}

	return &cfg, nil
}

// grantMatchesPrincipal checks if a grant applies to the given principal.
//
// Matching priority (first wins):
//  1. Specific username
//  2. Any authenticated user (auth: true)
//  3. Anyone including unauthenticated (all: true)
//
// Origin is a cross-cutting constraint on top of user/auth/all:
//
//	""  → grant is inert (never matches)
//	"*" → any origin (no constraint)
//	otherwise → must exactly match principal.Origin.
//
// If principal.Origin is empty (direct navigation), the origin
// check is passed — the user is navigating directly.
func grantMatchesPrincipal(g Grant, principal *grits.Principal) bool {
	if g.Origin == "" {
		return false
	}

	switch {
	case g.User != "":
		if g.User != principal.User {
			return false
		}
	case g.Auth != nil && *g.Auth:
		if principal.User == "" {
			return false
		}
	case g.All != nil && *g.All:
		// matches anyone
	default:
		// No user/auth/all tier — origin alone can serve as the
		// matching criterion (but only if it names a specific origin;
		// "*" alone with no tier is inert since there's nothing to match).
		if g.Origin == "*" {
			return false
		}
	}

	if g.Origin == "*" {
		return true
	}

	// Direct navigation — user at the keyboard, always passes.
	if principal.Origin == "" {
		return true
	}

	return g.Origin == principal.Origin
}

// resolvePermission walks up the tree from dirPath to root, collects all
// grants that match the principal from .grits/access.json files, and merges
// them into a single effective Permission.
//
// Inheritance rules:
//   - read applies downward (any ancestor with read grants read to descendants)
//   - read+write applies downward as owner (write at an ancestor lets you
//     replace any descendant subtree including its .grits, granting effective
//     ownership over descendants)
//   - owner applies downward unchanged
//   - insert does NOT apply downward (only the exact directory with the grant)
//
// If no access.json is found anywhere, the default is deny (empty Permission).
//
// resolvePermissionAtRoot resolves .grits/access.json
// files against a pinned root node, avoiding TOCTOU between path resolution and
// permission checking. If rootNode is nil, falls back to the old behavior.
// files against a pinned root node, avoiding TOCTOU between path resolution and
// permission checking. If rootNode is nil, falls back to the old behavior.
func (m *AuthModule) resolvePermissionAtRoot(vol Volume, rootNode grits.FileNode, dirPath string, principal *grits.Principal) Permission {
	dirPath = strings.TrimRight(dirPath, "/")

	// Collect all matching grants from the path up to root.
	var inheritedGrants []Grant
	var localGrants []Grant // used for insert (non-inherited)

	segments := strings.Split(dirPath, "/")
	acc := ""
	for i, seg := range segments {
		if seg == "" {
			continue
		}
		if acc == "" {
			acc = seg
		} else {
			acc = acc + "/" + seg
		}

		cfg, err := m.readAccessConfigAtRoot(vol, rootNode, acc)
		if err != nil {
			grits.DebugLog(grits.DebugAuth, "Auth [permission]: error reading %q: %v", acc+"/.grits/access.json", err)
			continue
		}
		if cfg == nil {
			continue
		}

		for _, g := range cfg.Allow {
			if !grantMatchesPrincipal(g, principal) {
				continue
			}
			// Grants at the target directory are local (for insert).
			// Grants at ancestor directories are inherited.
			if i == len(segments)-1 {
				localGrants = append(localGrants, g)
			} else {
				inheritedGrants = append(inheritedGrants, g)
			}
		}
	}

	// Also check root
	rootCfg, err := m.readAccessConfigAtRoot(vol, rootNode, "")
	if err == nil && rootCfg != nil {
		for _, g := range rootCfg.Allow {
			if grantMatchesPrincipal(g, principal) {
				inheritedGrants = append(inheritedGrants, g)
			}
		}
	}

	// Merge inherited grants. Permission transformations:
	//   - Insert → not inherited (only applies at the exact directory)
	//   - read+insert → read (only the read part is inherited)
	//   - read+write → owner (write at an ancestor lets you replace any
	//     descendant including its .grits, granting effective ownership)
	//   - owner, read → inherited as-is
	merged := Permission("")
	for _, g := range inheritedGrants {
		inherited := g.Permission
		switch inherited {
		case PermInsert:
			continue
		case PermReadInsert:
			inherited = PermRead
		case PermReadWrite:
			inherited = PermOwner
		}
		merged = mergePerm(merged, inherited)
	}

	// Merge local (directory-level) grants. Insert applies only at the
	// exact directory that contains the access.json.
	for _, g := range localGrants {
		merged = mergePerm(merged, g.Permission)
	}

	// If the path is under .grits/, enforce special rules:
	//   - read+write can READ .grits but not write it
	//   - owner can do everything
	//   - read-only can also read .grits
	if strings.Contains(dirPath, "/.grits") || strings.HasPrefix(dirPath, ".grits") {
		if CanOwn(merged) {
			return merged
		}
		if CanWrite(merged) || CanRead(merged) {
			return PermRead // .grits is read-only without owner
		}
		return ""
	}

	return merged
}

// hasPermissionAtRoot checks whether the principal has the required permission at
// the given path within the volume, resolving .grits/access.json files against a
// pinned root node. BackendPrincipal always passes. If rootNode is nil,
// falls back to resolving via ReadVolumeFile (TOCTOU possible).
func (m *AuthModule) hasPermissionAtRoot(vol Volume, rootNode grits.FileNode, path string, principal *grits.Principal, required Permission) bool {
	if principal == grits.BackendPrincipal {
		return true
	}
	effective := m.resolvePermissionAtRoot(vol, rootNode, path, principal)
	switch required {
	case PermRead:
		return CanRead(effective)
	case PermInsert:
		return CanInsert(effective)
	case PermReadWrite:
		return CanWrite(effective)
	case PermOwner:
		return CanOwn(effective)
	}
	return false
}

// readAccessConfigAtRoot dispatches to readAccessConfigFromRoot when rootNode is
// non-nil, otherwise falls back to the old readAccessConfig (via ReadVolumeFile).
func (m *AuthModule) readAccessConfigAtRoot(vol Volume, rootNode grits.FileNode, dirPath string) (*AccessConfig, error) {
	if rootNode != nil {
		return m.readAccessConfigFromRoot(vol, rootNode, dirPath)
	}
	return m.readAccessConfig(vol, dirPath)
}

// MakeLookupCallback returns a LookupCallback that enforces filesystem-based
// permissions by reading .grits/access.json files along the path. The volume
// is captured so the callback reads access.json from the correct volume.
func (m *AuthModule) MakeLookupCallback(vol Volume) grits.LookupCallback {
	return func(resp *grits.LookupResponse, req *grits.LookupRequest, root grits.FileNode) (*grits.LookupResponse, error) {
		if resp == nil {
			return nil, nil
		}

		grits.DebugLog(grits.DebugAuth, "Auth [lookup]: checking %d paths", len(resp.Paths))

		result := make([]*grits.PathNodePair, 0, len(resp.Paths))

		for _, pair := range resp.Paths {
			if !m.hasPermissionAtRoot(vol, root, pair.Path, req.Principal, PermRead) {
				grits.DebugLog(grits.DebugAuth, "Auth [lookup]: access denied for %q", pair.Path)
				result = append(result, &grits.PathNodePair{
					Path:  pair.Path,
					Error: "access_denied",
				})
			} else {
				grits.DebugLog(grits.DebugAuth, "Auth [lookup]: allowing %q", pair.Path)
				result = append(result, pair)
			}
		}

		return &grits.LookupResponse{
			Paths:        result,
			SerialNumber: resp.SerialNumber,
		}, nil
	}
}

// MakeLinkCallback returns a LinkCallback that enforces filesystem-based
// permissions by reading .grits/access.json files along the path. The volume
// is captured so the callback reads access.json from the correct volume.
// writeMtx IS held during this callback, so ns.rootAddr == oldRoot.
func (m *AuthModule) MakeLinkCallback(vol Volume) grits.LinkCallback {
	return func(oldRoot, newRoot grits.FileNode, requests []*grits.LinkRequest, principal *grits.Principal) error {
		grits.DebugLog(grits.DebugAuth, "Auth [link]: checking %d requests", len(requests))

		for _, req := range requests {
			reqPath := strings.TrimRight(req.Path, "/")
			parent := parentPath(reqPath)

			if m.hasPermissionAtRoot(vol, oldRoot, parent, principal, PermReadWrite) {
				grits.DebugLog(grits.DebugAuth, "Auth [link]: ALLOW %q (has write)", reqPath)
				continue
			}

			if m.hasPermissionAtRoot(vol, oldRoot, parent, principal, PermInsert) {
				// Insert only allows creating new files (paths that don't exist yet).
				// Check existence against oldRoot — the file isn't in newRoot yet
				// from the perspective of the committed tree.
				insertResp, err := vol.lookupFromRoot(reqPath, oldRoot)
				exists := err == nil && insertResp != nil && insertResp.Leaf() != nil && insertResp.Leaf().Error == ""
				if !exists {
					// File doesn't exist — this is a genuine insert.
					grits.DebugLog(grits.DebugAuth, "Auth [link]: ALLOW %q (insert)", reqPath)
					continue
				}
				// File exists — insert doesn't grant modification rights.
				grits.DebugLog(grits.DebugAuth, "Auth [link]: DENY %q (insert but file exists)", reqPath)
				return &grits.ErrAccessDenied{Path: reqPath}
			}

			grits.DebugLog(grits.DebugAuth, "Auth [link]: DENY %q (no permission)", reqPath)
			return &grits.ErrAccessDenied{Path: reqPath}
		}

		return nil
	}
}
