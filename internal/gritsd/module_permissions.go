package gritsd

import (
	"grits/internal/grits"
	"log"
	"strings"
)

/////
// Permission types

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

/////
// access.json schema — defined here for future use, not enforced yet.

type Grant struct {
	User       string     `json:"user,omitempty"`
	Origin     string     `json:"origin,omitempty"`
	Permission Permission `json:"permission"`
}

type AccessConfig struct {
	Grants []Grant `json:"grants"`
}

/////
// Whitelist helpers

// pathCoveredBy returns true if path is equal to or a descendant of any
// entry in the whitelist. An empty whitelist covers everything.
func pathCoveredBy(path string, whitelist []string) bool {
	if len(whitelist) == 0 {
		return true
	}
	for _, entry := range whitelist {
		if path == entry || strings.HasPrefix(path, entry+"/") {
			return true
		}
	}
	return false
}

/////
// Module config and struct

type PermissionsModuleConfig struct {
	// ReadWhitelist restricts which paths are visible in lookup responses.
	// Empty means all paths are readable.
	// Paths not covered are pruned silently from multi-path responses;
	// direct requests to uncovered paths return access_denied.
	ReadWhitelist []string `json:"readWhitelist,omitempty"`

	// WriteWhitelist restricts which paths may be written via link operations.
	// Empty means all paths are writable.
	// Attempts to write outside the whitelist return ErrAccessDenied.
	WriteWhitelist []string `json:"writeWhitelist,omitempty"`
}

type PermissionsModule struct {
	Config *PermissionsModuleConfig
	Server *Server
}

func NewPermissionsModule(server *Server, config *PermissionsModuleConfig) (*PermissionsModule, error) {
	m := &PermissionsModule{
		Config: config,
		Server: server,
	}

	server.AddModuleHook(func(module Module) {
		vol, ok := module.(*LocalVolume)
		if !ok {
			return
		}
		log.Printf("Permissions: attaching to volume %q", vol.GetVolumeName())
		vol.SetPermissionsModule(m)
	})

	return m, nil
}

func (*PermissionsModule) GetModuleName() string          { return "permissions" }
func (*PermissionsModule) GetDependencies() []*Dependency { return nil }
func (m *PermissionsModule) GetConfig() any               { return m.Config }
func (*PermissionsModule) Start() error                   { return nil }
func (*PermissionsModule) Stop() error                    { return nil }

/////
// LookupCallback — enforce ReadWhitelist

func (m *PermissionsModule) MakeLookupCallback(ns *grits.NameStore) grits.LookupCallback {
	return func(resp *grits.LookupResponse, principal *grits.Principal) (*grits.LookupResponse, error) {
		if resp == nil {
			return nil, nil
		}
		if principal == grits.BackendPrincipal {
			return resp, nil
		}

		log.Printf("Permissions [lookup]: checking %d paths", len(resp.Paths))

		result := make([]*grits.PathNodePair, 0, len(resp.Paths))

		for _, pair := range resp.Paths {
			denied := !pathCoveredBy(pair.Path, m.Config.ReadWhitelist)
			if denied {
				// Replace this entry with access_denied, preserving the path.
				// Don't change the number of entries or their paths.
				log.Printf("Permissions [lookup]: access denied for %q", pair.Path)
				result = append(result, &grits.PathNodePair{
					Path:  pair.Path,
					Error: "access_denied",
				})
			} else {
				log.Printf("Permissions [lookup]: allowing %q", pair.Path)
				result = append(result, pair)
			}
		}

		return &grits.LookupResponse{
			Paths:        result,
			SerialNumber: resp.SerialNumber,
		}, nil
	}
}

/////
// LinkCallback — enforce WriteWhitelist. Root is never writable.

func (m *PermissionsModule) MakeLinkCallback(ns *grits.NameStore) grits.LinkCallback {
	return func(oldRoot, newRoot grits.FileNode, requests []*grits.LinkRequest, principal *grits.Principal) error {
		log.Printf("Permissions [link]: checking %d requests", len(requests))

		if principal == grits.BackendPrincipal {
			return nil
		}

		for _, req := range requests {
			path := strings.TrimRight(req.Path, "/")

			if !pathCoveredBy(path, m.Config.WriteWhitelist) {
				log.Printf("Permissions [link]: DENY %q (not in WriteWhitelist)", path)
				return &grits.ErrAccessDenied{Path: path}
			}

			log.Printf("Permissions [link]: ALLOW %q", path)
		}

		return nil
	}
}
