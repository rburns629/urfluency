package cli

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// settings are what `urfluency init` saves: the addresses the user confirmed as
// theirs and the ones they said are not. They stay on this machine, readable
// only by the user, and are never uploaded.
type settings struct {
	Emails  []string `json:"emails"`
	NotMine []string `json:"not_mine"`
}

// settingsPath is <user config dir>/urfluency/settings.json, or the same file in
// URFLUENCY_CONFIG_DIR when set.
func settingsPath() (string, error) {
	dir := os.Getenv("URFLUENCY_CONFIG_DIR")
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(base, "urfluency")
	}
	return filepath.Join(dir, "settings.json"), nil
}

// loadSettings returns the saved settings, or empty settings if none are saved.
func loadSettings() (settings, error) {
	var s settings
	path, err := settingsPath()
	if err != nil {
		return s, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(data, &s)
}

// save writes the settings atomically with owner-only permissions and returns
// where they were written.
func (s settings) save() (string, error) {
	path, err := settingsPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	s.Emails, s.NotMine = sortedSet(s.Emails), sortedSet(s.NotMine)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "settings-*.json")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return path, os.Rename(tmp.Name(), path)
}

func sortedSet(values []string) []string {
	out := append([]string{}, values...)
	slices.Sort(out)
	return slices.Compact(out)
}
