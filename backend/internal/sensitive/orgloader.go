package sensitive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// OrgCredentialsFile is the on-disk format for ~/.pux/credentials/<org>.json.
// Each top-level key is a domain; nested keys are secret names.
//
// Example:
//
//	{
//	  "version": 1,
//	  "secrets": {
//	    "openrouter": {"api_key": "sk-or-v1-..."},
//	    "surrealdb":  {"password": "..."}
//	  }
//	}
type OrgCredentialsFile struct {
	Version int                               `json:"version"`
	Secrets map[string]map[string]string      `json:"secrets"`
}

// LoadOrgCredentials loads credentials for an org from disk and populates the
// given store. The store is modified in place. Returns the count of secrets loaded.
//
// orgName accepts either a bare org name ("deep-research-engine") or an absolute
// org path ("/home/.../.pux/orgs/deep-research-engine") — callers in the prompt
// pipeline pass whichever they have. Paths are reduced to their basename so the
// file lookup hits ~/.pux/credentials/<basename>.json.
//
// Resolution order:
//  1. $PUX_CREDENTIALS_PATH (if set, takes precedence)
//  2. ~/.pux/credentials/<orgName>.json
//
// Missing file is not an error — returns 0. Malformed file IS an error.
func LoadOrgCredentials(store *Store, orgName string) (int, error) {
	if store == nil {
		return 0, fmt.Errorf("nil store")
	}
	if orgName == "" {
		return 0, nil
	}

	// Accept absolute org paths (CLI resolves --org to a path before sending).
	// Reduce to basename so "/home/.../.pux/orgs/foo" → "foo".
	if filepath.IsAbs(orgName) {
		orgName = filepath.Base(orgName)
	}

	path := resolveCredentialsPath(orgName)
	if path == "" {
		return 0, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read credentials %s: %w", path, err)
	}

	var file OrgCredentialsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return 0, fmt.Errorf("parse credentials %s: %w", path, err)
	}

	count := 0
	for domain, keys := range file.Secrets {
		for key, val := range keys {
			if val == "" {
				continue
			}
			store.Set(domain, key, val)
			count++
		}
	}
	return count, nil
}

// resolveCredentialsPath finds the credentials file for an org.
// Returns empty string if no path is configured.
func resolveCredentialsPath(orgName string) string {
	// Explicit override — useful for tests
	if envPath := os.Getenv("PUX_CREDENTIALS_PATH"); envPath != "" {
		return envPath
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pux", "credentials", orgName+".json")
}

// CredentialsPathFor returns the on-disk path where an org's credentials live.
// Useful for tools that need to tell the user where to put the file.
func CredentialsPathFor(orgName string) string {
	return resolveCredentialsPath(orgName)
}
