package skillhub

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type candidate struct {
	Path   string
	Name   string
	Status string
	Reason string
}

func (a *App) discover() ([]candidate, error) {
	roots := []string{filepath.Join(a.UserHome, ".agents", "skills"), filepath.Join(a.CodexHome, "skills")}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
	if data, err := cmd.Output(); err == nil {
		project := strings.TrimSpace(string(data))
		if !under(project, a.Home) {
			roots = append(roots, filepath.Join(project, ".agents", "skills"), filepath.Join(project, ".codex", "skills"))
		}
	}
	s, err := a.installs()
	if err != nil {
		return nil, err
	}
	var found []candidate
	seen := make(map[string]bool)
	for _, root := range roots {
		if seen[root] {
			continue
		}
		seen[root] = true
		entries, err := os.ReadDir(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			path := filepath.Join(root, entry.Name())
			c := candidate{Path: path, Name: entry.Name(), Status: "publishable"}
			if err := validateSkill(path); err != nil {
				c.Status, c.Reason = "invalid", err.Error()
			}
			if findInstall(s, path) >= 0 {
				c.Status = "managed"
			}
			found = append(found, c)
		}
	}
	return found, nil
}

func (a *App) scan() error {
	items, err := a.discover()
	if err != nil {
		return err
	}
	for _, item := range items {
		appendLine(a.Out, item.Status, item.Name, item.Path, item.Reason)
	}
	if len(items) == 0 {
		fmt.Fprintln(a.Out, "No local skills found")
	}
	return nil
}

func (a *App) publish(raw string, yes bool) error {
	source, err := filepath.Abs(raw)
	if err != nil {
		return err
	}
	items, err := a.discover()
	if err != nil {
		return err
	}
	var selected *candidate
	for i := range items {
		if samePath(items[i].Path, source) {
			selected = &items[i]
			break
		}
	}
	if selected == nil {
		return fmt.Errorf("%s is not a skill found by skillhub scan", source)
	}
	if selected.Status != "publishable" {
		return fmt.Errorf("%s is %s: %s", source, selected.Status, selected.Reason)
	}
	if err := a.repoSync(); err != nil {
		return err
	}
	repo, err := a.repo()
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	if under(resolved, repo) {
		return errors.New("skill already comes from the managed repository")
	}
	if err := validateSkill(source); err != nil {
		return err
	}
	name := selected.Name
	dest := filepath.Join(repo, "skills", name)
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("repository skill already exists: %s", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	files, err := skillFiles(resolved)
	if err != nil {
		return err
	}
	fmt.Fprintln(a.Out, "Publish", source, "to", dest)
	for _, file := range files {
		fmt.Fprintln(a.Out, " +", file)
	}
	if !yes {
		fmt.Fprint(a.Out, "Continue? [y/N] ")
		line, err := bufio.NewReader(a.In).ReadString('\n')
		if err != nil && len(line) == 0 {
			return errors.New("publish canceled")
		}
		if strings.TrimSpace(strings.ToLower(line)) != "y" {
			return errors.New("publish canceled")
		}
	}
	if err := os.MkdirAll(filepath.Join(repo, "skills"), 0755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Join(repo, "skills"), ".skillhub-publish-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := copySkill(source, filepath.Join(tmp, name)); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(tmp, name), dest); err != nil {
		return err
	}
	rel := filepath.ToSlash(filepath.Join("skills", name))
	if _, err := a.git(repo, "add", "--", rel); err != nil {
		_ = os.RemoveAll(dest)
		return err
	}
	if _, err := a.git(repo, "commit", "-m", "Add skill "+name); err != nil {
		_, _ = a.git(repo, "reset", "--", rel)
		_ = os.RemoveAll(dest)
		return err
	}
	if _, err := a.git(repo, "push", "-u", "origin", "HEAD"); err != nil {
		return fmt.Errorf("skill committed locally but push failed; run skillhub repo push after resolving the remote: %w", err)
	}
	head, _ := a.head()
	fmt.Fprintln(a.Out, "Published", name, "at", head)
	return nil
}
