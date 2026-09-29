package skillhub

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var githubSSH = regexp.MustCompile(`^git@github\.com:[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(?:\.git)?$`)
var repoPart = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func validRepoURL(raw string) bool {
	if githubSSH.MatchString(raw) {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Hostname() != "github.com" || u.Port() != "" {
		return false
	}
	if u.Scheme != "https" {
		return false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	return len(parts) == 2 && repoPart.MatchString(parts[0]) && repoPart.MatchString(parts[1])
}

func (a *App) git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	data, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(data)))
	}
	return strings.TrimSpace(string(data)), nil
}

func (a *App) repo() (string, error) {
	c, err := a.config()
	if err != nil {
		return "", err
	}
	if c.RepoURL == "" {
		return "", errors.New("repository not configured; run skillhub repo set <url>")
	}
	path := a.path("repo")
	if st, err := os.Stat(filepath.Join(path, ".git")); err != nil || !st.IsDir() {
		return "", fmt.Errorf("repository clone missing at %s", path)
	}
	return path, nil
}

func (a *App) head() (string, error) {
	repo, err := a.repo()
	if err != nil {
		return "", err
	}
	return a.git(repo, "rev-parse", "HEAD")
}

func (a *App) repoSet(raw string) error {
	if !validRepoURL(raw) {
		return errors.New("repository must be a github.com HTTPS or git@github.com SSH URL without embedded credentials")
	}
	c, err := a.config()
	if err != nil {
		return err
	}
	if c.RepoURL == raw {
		if _, err := a.repo(); err == nil {
			fmt.Fprintln(a.Out, "Repository already configured:", a.path("repo"))
			return nil
		}
	}
	if c.RepoURL != "" && c.RepoURL != raw {
		return errors.New("a different repository is already configured; v1 does not change repositories in place")
	}
	tmp, err := os.MkdirTemp(a.Home, ".clone-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	clone := filepath.Join(tmp, "repo")
	if _, err := a.git("", "clone", "--", raw, clone); err != nil {
		return err
	}
	names, err := listSkills(clone)
	if err != nil {
		return err
	}
	s, err := a.installs()
	if err != nil {
		return err
	}
	available := make(map[string]bool, len(names))
	for _, name := range names {
		available[name] = true
	}
	for _, item := range s.Installs {
		if !available[item.Skill] {
			return fmt.Errorf("repository is missing installed skill %s", item.Skill)
		}
	}
	if _, err := a.git(clone, "rev-parse", "--verify", "HEAD"); err == nil {
		if err := a.validateRef(clone, "HEAD"); err != nil {
			return err
		}
	}
	target := a.path("repo")
	backup := a.path(".repo-backup")
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(clone, target); err != nil {
		_ = os.Rename(backup, target)
		return err
	}
	if err := writeJSON(a.path("config.json"), Config{RepoURL: raw}); err != nil {
		_ = os.RemoveAll(target)
		_ = os.Rename(backup, target)
		return err
	}
	_ = os.RemoveAll(backup)
	fmt.Fprintln(a.Out, "Cloned", raw, "to", target)
	return nil
}

func (a *App) cleanRepo(repo string) error {
	status, err := a.git(repo, "status", "--porcelain")
	if err != nil {
		return err
	}
	if status != "" {
		return errors.New("repository has local changes; resolve them before syncing or publishing")
	}
	return nil
}

func (a *App) repoSync() error {
	repo, err := a.repo()
	if err != nil {
		return err
	}
	if err := a.cleanRepo(repo); err != nil {
		return err
	}
	if _, err := a.git(repo, "fetch", "origin"); err != nil {
		return err
	}
	if _, err := a.git(repo, "rev-parse", "--verify", "HEAD"); err != nil {
		branch, branchErr := a.git(repo, "symbolic-ref", "--short", "HEAD")
		if branchErr != nil {
			return branchErr
		}
		remoteRef := "origin/" + branch
		if _, refErr := a.git(repo, "rev-parse", "--verify", remoteRef); refErr != nil {
			fmt.Fprintln(a.Out, "Repository has no commits yet; publish a skill to create the first commit")
			return nil
		}
		if err := a.validateRef(repo, remoteRef); err != nil {
			return err
		}
		if _, err := a.git(repo, "checkout", "-B", branch, remoteRef); err != nil {
			return err
		}
		if _, err := listSkills(repo); err != nil {
			return err
		}
		head, _ := a.head()
		fmt.Fprintln(a.Out, "Synced", head)
		return nil
	}
	upstream, err := a.git(repo, "rev-parse", "--abbrev-ref", "@{upstream}")
	if err != nil {
		return errors.New("repository branch has no upstream")
	}
	if err := a.validateRef(repo, upstream); err != nil {
		return err
	}
	s, err := a.installs()
	if err != nil {
		return err
	}
	for _, item := range s.Installs {
		if _, err := a.git(repo, "cat-file", "-e", upstream+":skills/"+item.Skill+"/SKILL.md"); err != nil {
			return fmt.Errorf("remote removed installed skill %s; remove its links before syncing", item.Skill)
		}
	}
	if _, err := a.git(repo, "merge", "--ff-only", upstream); err != nil {
		return err
	}
	if _, err := listSkills(repo); err != nil {
		return err
	}
	head, _ := a.head()
	fmt.Fprintln(a.Out, "Synced", head)
	return nil
}

// Inspect the fetched tree before checkout so a bad remote commit cannot make
// symlinked installations point to missing or invalid skill files.
func (a *App) validateRef(repo, ref string) error {
	cmd := exec.Command("git", "ls-tree", "-r", "-z", ref, "--", "skills")
	cmd.Dir = repo
	data, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("inspect remote skills: %w", err)
	}
	names := make(map[string]bool)
	for _, raw := range strings.Split(string(data), "\x00") {
		if raw == "" {
			continue
		}
		metadata, path, ok := strings.Cut(raw, "\t")
		if !ok {
			return errors.New("invalid remote tree entry")
		}
		parts := strings.Split(path, "/")
		if len(parts) < 3 || parts[0] != "skills" || !validName(parts[1]) {
			return fmt.Errorf("invalid remote skill path: %s", path)
		}
		if !strings.HasPrefix(metadata, "100644 blob ") && !strings.HasPrefix(metadata, "100755 blob ") {
			return fmt.Errorf("remote skill contains symlink or submodule: %s", path)
		}
		names[parts[1]] = true
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		data, err := a.git(repo, "show", ref+":skills/"+name+"/SKILL.md")
		if err != nil {
			return fmt.Errorf("remote skill %s has no SKILL.md", name)
		}
		meta, err := frontmatter([]byte(data))
		if err != nil {
			return fmt.Errorf("remote skill %s: %w", name, err)
		}
		if meta["name"] != name || meta["description"] == "" {
			return fmt.Errorf("remote skill %s has invalid name or description", name)
		}
	}
	return nil
}

func (a *App) repoPush() error {
	repo, err := a.repo()
	if err != nil {
		return err
	}
	if err := a.cleanRepo(repo); err != nil {
		return err
	}
	if _, err := a.git(repo, "push", "-u", "origin", "HEAD"); err != nil {
		return err
	}
	fmt.Fprintln(a.Out, "Pushed repository")
	return nil
}

func (a *App) list() error {
	repo, err := a.repo()
	if err != nil {
		return err
	}
	skills, err := listSkills(repo)
	if err != nil {
		return err
	}
	head, _ := a.head()
	fmt.Fprintln(a.Out, "commit", head)
	for _, name := range skills {
		fmt.Fprintln(a.Out, name)
	}
	return nil
}
