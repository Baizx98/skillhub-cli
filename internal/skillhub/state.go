package skillhub

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	RepoURL string `json:"repo_url"`
}

type Install struct {
	Agent  string `json:"agent"`
	Scope  string `json:"scope"`
	Skill  string `json:"skill"`
	Link   string `json:"link"`
	Target string `json:"target"`
}

type installState struct {
	Installs []Install `json:"installs"`
}

type historyEntry struct {
	Time   string `json:"time"`
	Action string `json:"action"`
	Object string `json:"object,omitempty"`
	Commit string `json:"commit,omitempty"`
	Result string `json:"result"`
	Error  string `json:"error,omitempty"`
}

func (a *App) path(parts ...string) string {
	return filepath.Join(append([]string{a.Home}, parts...)...)
}

func (a *App) ensureHome() error { return os.MkdirAll(a.Home, 0700) }

func readJSON(path string, out any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("invalid %s: %w", path, err)
	}
	return nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomic(path, data)
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".skillhub-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// Windows cannot rename over an existing file. The state file is small; a
	// failed replace leaves the old file intact on Unix, and the temp on Windows.
	if err = os.Rename(f.Name(), path); err == nil {
		return nil
	}
	if _, statErr := os.Stat(path); statErr == nil {
		backup := path + ".bak"
		if moveErr := os.Rename(path, backup); moveErr != nil {
			return err
		}
		if moveErr := os.Rename(f.Name(), path); moveErr != nil {
			_ = os.Rename(backup, path)
			return moveErr
		}
		_ = os.Remove(backup)
		return nil
	}
	return err
}

func (a *App) config() (Config, error) {
	var c Config
	err := readJSON(a.path("config.json"), &c)
	return c, err
}

func (a *App) installs() (installState, error) {
	var s installState
	err := readJSON(a.path("installs.json"), &s)
	return s, err
}

func (a *App) saveInstalls(s installState) error {
	return writeJSON(a.path("installs.json"), s)
}

func (a *App) history(action, object, commit string, operationErr error) {
	if a.ensureHome() != nil {
		return
	}
	e := historyEntry{Time: time.Now().UTC().Format(time.RFC3339), Action: action, Object: object, Commit: commit, Result: "ok"}
	if operationErr != nil {
		e.Result, e.Error = "error", operationErr.Error()
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	f, err := os.OpenFile(a.path("history.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if os.PathSeparator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func under(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func copyFile(dst string, src io.Reader, mode os.FileMode) error {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, src)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
