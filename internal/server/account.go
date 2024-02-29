package server

import (
	"encoding/json"
	"fmt"
	"grits/internal/grits"
	"os"
)

func (s *Server) CreateAccounts() error {
	rootStore, err := grits.EmptyNameStore(s.BlobStore)
	if err != nil {
		return fmt.Errorf("failed to create root store: %v", err)
	}

	s.AccountStores["root"] = rootStore
	return nil
}

func (s *Server) LoadAccounts() error {
	s.AccountLock.Lock()
	defer s.AccountLock.Unlock()

	data, err := os.ReadFile(s.Config.ServerPath("var/account_roots.json"))
	if err != nil {
		if os.IsNotExist(err) {
			// If the file doesn't exist, it's not an error; start with an empty revision.
			return s.CreateAccounts()
		}
		return fmt.Errorf("failed to read account roots file: %v", err)
	}

	accountRoots := make(map[string]string)
	if err := json.Unmarshal(data, &accountRoots); err != nil {
		return fmt.Errorf("failed to unmarshal account roots: %v", err)
	}

	for account, rootAddrStr := range accountRoots {
		rootAddr, err := grits.NewTypedFileAddrFromString(rootAddrStr)
		if err != nil {
			return fmt.Errorf("failed to parse root address for %s: %v", account, err)
		}

		ns, err := grits.DeserializeNameStore(s.BlobStore, rootAddr)
		if err != nil {
			return fmt.Errorf("failed to deserialize name store for %s: %v", account, err)
		}
		s.AccountStores[account] = ns
	}

	return nil

}

func (s *Server) SaveAccounts() error {
	s.AccountLock.Lock()
	defer s.AccountLock.Unlock()

	accountRoots := make(map[string]string)
	for account, ns := range s.AccountStores {
		// Ensure thread-safe access to each NameStore's root
		accountRoots[account] = ns.GetRoot()
	}

	data, err := json.MarshalIndent(accountRoots, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal account roots: %v", err)
	}

	tempFileName := s.Config.ServerPath("var/account_roots.json.tmp")
	finalFileName := s.Config.ServerPath("var/account_roots.json")

	if err := os.WriteFile(tempFileName, data, 0644); err != nil {
		return fmt.Errorf("failed to write NameStore to temp file: %w", err)
	}

	if err := os.Rename(tempFileName, finalFileName); err != nil {
		return fmt.Errorf("failed to replace old NameStore file: %w", err)
	}

	return nil
}
