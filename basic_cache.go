package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// basic-token cache — a primed anonymous token is server-registered by
// value, so the same string keeps working across process restarts. We
// stash it at $XDG_CACHE_HOME/psyduck-ifunny/basic-token (falling back to
// ~/.cache/... when XDG_CACHE_HOME is unset) so a "generate-cache" auth
// mode skips the ~15s prime handshake after the first run.
//
// The cache holds no expiry: iFunny doesn't expose one, and a stale token
// surfaces as an auth failure downstream. To invalidate, delete the file.

// basicCachePath returns the cache file location, resolving $XDG_CACHE_HOME
// per the XDG Base Directory Specification with a ~/.cache fallback.
func basicCachePath() (string, error) {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "psyduck-ifunny", "basic-token"), nil
}

// loadCachedBasic returns (token, true, nil) on a cache hit, ("", false,
// nil) on a clean miss (file does not exist or contents are empty), and
// (_, _, err) on I/O trouble.
func loadCachedBasic() (string, bool, error) {
	path, err := basicCachePath()
	if err != nil {
		return "", false, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return "", false, nil
	}
	return token, true, nil
}

// storeCachedBasic writes a freshly-primed token to the cache. The parent
// directory is created 0700, the file 0600 — the token grants read access
// to iFunny's public API as an anonymous device, so leaking it is low-
// stakes but still not free.
func storeCachedBasic(token string) error {
	path, err := basicCachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(token), 0o600)
}
