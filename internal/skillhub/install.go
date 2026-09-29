package skillhub

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func (a *App) destination(o options, name string) (string, string, error) {
	root, scope := a.UserHome, "global"
	if o.project != "" {
		abs, err := filepath.Abs(o.project)
		if err != nil {
			return "", "", err
		}
		st, err := os.Stat(abs)
		if err != nil || !st.IsDir() {
			return "", "", fmt.Errorf("project directory does not exist: %s", abs)
		}
		root, scope = abs, "project"
	} else if !o.global {
		cwd, err := os.Getwd()
		if err != nil {
			return "", "", err
		}
		cmd := exec.Command("git", "rev-parse", "--show-toplevel")
		cmd.Dir = cwd
		if output, err := cmd.Output(); err == nil {
			root, scope = filepath.Clean(strings.TrimSpace(string(output))), "project"
		}
	}
	if scope == "project" && under(root, a.Home) {
		return "", "", errors.New("project is inside SKILLHUB_HOME; choose another project or --global")
	}
	return filepath.Join(root, ".agents", "skills", name), scope, nil
}

func findInstall(s installState, link string) int {
	for i, item := range s.Installs {
		if samePath(item.Link, link) {
			return i
		}
	}
	return -1
}

func linkTarget(link string) (string, error) {
	st, err := os.Lstat(link)
	if err != nil {
		return "", err
	}
	if st.Mode()&os.ModeSymlink == 0 {
		return "", fmt.Errorf("%s is not a symlink", link)
	}
	target, err := os.Readlink(link)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}
	return filepath.Clean(target), nil
}

func symlink(target, link string) error {
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		return err
	}
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			return fmt.Errorf("create directory symlink %s: %w (enable Windows Developer Mode or use an account with symlink permission)", link, err)
		}
		return fmt.Errorf("create symlink %s: %w", link, err)
	}
	return nil
}

func (a *App) install(name string, o options) error {
	if !validName(name) {
		return fmt.Errorf("invalid skill name %q", name)
	}
	repo, err := a.repo()
	if err != nil {
		return err
	}
	target := filepath.Join(repo, "skills", name)
	if err := validateSkill(target); err != nil {
		return err
	}
	link, scope, err := a.destination(o, name)
	if err != nil {
		return err
	}
	s, err := a.installs()
	if err != nil {
		return err
	}
	index := findInstall(s, link)
	if existing, err := linkTarget(link); err == nil {
		if index < 0 || !samePath(existing, s.Installs[index].Target) {
			return fmt.Errorf("target already exists and is not a managed symlink: %s", link)
		}
		if !samePath(existing, target) {
			return fmt.Errorf("managed link points to the old SKILLHUB_HOME; run skillhub repair: %s", link)
		}
		fmt.Fprintln(a.Out, "Already installed:", link)
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("target already exists: %s", link)
	}
	if err := symlink(target, link); err != nil {
		return err
	}
	entry := Install{Agent: "codex", Scope: scope, Skill: name, Link: link, Target: target}
	if index >= 0 {
		s.Installs[index] = entry
	} else {
		s.Installs = append(s.Installs, entry)
	}
	if err := a.saveInstalls(s); err != nil {
		_ = os.Remove(link)
		return err
	}
	fmt.Fprintln(a.Out, "Installed:", link, "->", target)
	if scope == "project" {
		fmt.Fprintln(a.Out, "This absolute symlink is machine-specific; do not commit it to the project repository.")
	}
	return nil
}

func (a *App) remove(name string, o options) error {
	if !validName(name) {
		return fmt.Errorf("invalid skill name %q", name)
	}
	link, _, err := a.destination(o, name)
	if err != nil {
		return err
	}
	s, err := a.installs()
	if err != nil {
		return err
	}
	i := findInstall(s, link)
	if i < 0 {
		return fmt.Errorf("not managed by skillhub: %s", link)
	}
	existing, err := linkTarget(link)
	if err != nil {
		return fmt.Errorf("managed link is missing or replaced; refusing to remove %s", link)
	}
	if !samePath(existing, s.Installs[i].Target) {
		return fmt.Errorf("managed link was changed; refusing to remove %s", link)
	}
	if err := os.Remove(link); err != nil {
		return err
	}
	old := s.Installs[i]
	s.Installs = append(s.Installs[:i], s.Installs[i+1:]...)
	if err := a.saveInstalls(s); err != nil {
		_ = symlink(old.Target, link)
		return err
	}
	fmt.Fprintln(a.Out, "Removed:", link)
	return nil
}

func (a *App) installed() error {
	s, err := a.installs()
	if err != nil {
		return err
	}
	for _, item := range s.Installs {
		status := "ok"
		target, err := linkTarget(item.Link)
		if err != nil || !samePath(target, item.Target) {
			status = "changed-or-missing"
		}
		if _, err := os.Stat(item.Target); err != nil {
			status = "broken-source"
		}
		appendLine(a.Out, item.Agent, item.Scope, item.Skill, status, item.Link, "->", item.Target)
	}
	return nil
}

func (a *App) repair() error {
	repo, err := a.repo()
	if err != nil {
		return err
	}
	s, err := a.installs()
	if err != nil {
		return err
	}
	var repaired int
	for i, item := range s.Installs {
		target := filepath.Join(repo, "skills", item.Skill)
		if err := validateSkill(target); err != nil {
			return err
		}
		if item.Scope == "project" {
			project := filepath.Dir(filepath.Dir(filepath.Dir(item.Link)))
			if st, err := os.Stat(project); err != nil || !st.IsDir() {
				return fmt.Errorf("project directory for managed link no longer exists: %s", project)
			}
		}
		current, err := linkTarget(item.Link)
		if err == nil && samePath(current, target) {
			if !samePath(item.Target, target) {
				s.Installs[i].Target = target
				if err := a.saveInstalls(s); err != nil {
					return err
				}
			}
			continue
		}
		if err == nil {
			if !samePath(current, item.Target) {
				return fmt.Errorf("link changed by user: %s", item.Link)
			}
			if err := os.Remove(item.Link); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("refusing to replace non-link %s", item.Link)
		}
		if err := symlink(target, item.Link); err != nil {
			if current != "" {
				_ = symlink(current, item.Link)
			}
			return err
		}
		s.Installs[i].Target = target
		if err := a.saveInstalls(s); err != nil {
			_ = os.Remove(item.Link)
			if current != "" {
				_ = symlink(current, item.Link)
			}
			s.Installs[i] = item
			return err
		}
		repaired++
	}
	fmt.Fprintf(a.Out, "Repaired %d link(s)\n", repaired)
	return nil
}
