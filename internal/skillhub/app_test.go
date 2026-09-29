package skillhub

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, data)
	}
	return strings.TrimSpace(string(data))
}

func put(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0644); err != nil {
		t.Fatal(err)
	}
}

func skillText(name, body string) string {
	return "---\nname: " + name + "\ndescription: Test skill.\n---\n" + body + "\n"
}

func setup(t *testing.T) (*App, string, string, *bytes.Buffer) {
	t.Helper()
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	seed := filepath.Join(base, "seed")
	project := filepath.Join(base, "project")
	home := filepath.Join(base, "state")
	user := filepath.Join(base, "user")
	codex := filepath.Join(base, "custom-codex")
	for _, p := range []string{seed, project, home, user, codex} {
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
	}
	gitTest(t, base, "init", "--bare", remote)
	gitTest(t, seed, "init", "-b", "main")
	gitTest(t, seed, "config", "user.name", "Skillhub Test")
	gitTest(t, seed, "config", "user.email", "skillhub@example.test")
	put(t, filepath.Join(seed, "skills", "review", "SKILL.md"), skillText("review", "v1"))
	gitTest(t, seed, "add", ".")
	gitTest(t, seed, "commit", "-m", "seed")
	gitTest(t, seed, "remote", "add", "origin", remote)
	gitTest(t, seed, "push", "-u", "origin", "main")
	gitTest(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")
	gitTest(t, project, "init", "-b", "main")
	gitTest(t, home, "clone", remote, filepath.Join(home, "repo"))
	gitTest(t, filepath.Join(home, "repo"), "config", "user.name", "Skillhub Test")
	gitTest(t, filepath.Join(home, "repo"), "config", "user.email", "skillhub@example.test")
	put(t, filepath.Join(home, "config.json"), `{"repo_url":"https://github.com/example/skills.git"}`)
	var out bytes.Buffer
	a := &App{Home: home, UserHome: user, CodexHome: codex, Out: &out, Err: &out, In: strings.NewReader("y\n")}
	return a, seed, project, &out
}

func TestInstallSyncScanPublish(t *testing.T) {
	requireSymlink(t)
	a, seed, project, out := setup(t)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if err := a.Run([]string{"install", "review"}, "test"); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(project, ".agents", "skills", "review")
	if target, err := linkTarget(link); err != nil || target != filepath.Join(a.Home, "repo", "skills", "review") {
		t.Fatalf("link: %s, %v", target, err)
	}
	if err := a.Run([]string{"install", "review", "--global"}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := linkTarget(filepath.Join(a.UserHome, ".agents", "skills", "review")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(seed, "skills", "review", "SKILL.md"), skillText("review", "v2"))
	gitTest(t, seed, "add", ".")
	gitTest(t, seed, "commit", "-m", "update")
	gitTest(t, seed, "push")
	if err := a.Run([]string{"repo", "sync"}, "test"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(link, "SKILL.md"))
	if err != nil || !strings.Contains(string(data), "v2") {
		t.Fatalf("sync: %s, %v", data, err)
	}
	legacy := filepath.Join(a.CodexHome, "skills", "new-skill")
	put(t, filepath.Join(legacy, "SKILL.md"), skillText("new-skill", "published"))
	if err := a.Run([]string{"scan"}, "test"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), legacy) {
		t.Fatal("CODEX_HOME skill not scanned")
	}
	if err := a.Run([]string{"publish", legacy, "--yes"}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a.Home, "repo", "skills", "new-skill", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if err := a.Run([]string{"publish", legacy, "--yes"}, "test"); err == nil {
		t.Fatal("duplicate publish accepted")
	}
	if err := a.Run([]string{"remove", "review"}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("link remains: %v", err)
	}
	if err := a.Run([]string{"remove", "review", "--global"}, "test"); err != nil {
		t.Fatal(err)
	}
}

func TestConflictAndCodexHomeDetection(t *testing.T) {
	a, _, project, _ := setup(t)
	if err := a.selectAgent(""); err != nil {
		t.Fatal("CODEX_HOME detection failed:", err)
	}
	link := filepath.Join(project, ".agents", "skills", "review")
	put(t, filepath.Join(link, "SKILL.md"), skillText("review", "user-owned"))
	if err := a.install("review", options{project: project}); err == nil {
		t.Fatal("unmanaged directory overwritten")
	}
	if err := a.remove("review", options{project: project}); err == nil {
		t.Fatal("unmanaged directory removed")
	}
}

func TestURLAndHomeValidation(t *testing.T) {
	for _, value := range []string{"https://github.com/u/r", "git@github.com:u/r.git"} {
		if !validRepoURL(value) {
			t.Fatalf("rejected %s", value)
		}
	}
	for _, value := range []string{"https://user:pass@github.com/u/r", "https://evil.example/u/r", "-bad"} {
		if validRepoURL(value) {
			t.Fatalf("accepted %s", value)
		}
	}
	t.Setenv("SKILLHUB_HOME", "relative")
	if _, err := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader("")); err == nil {
		t.Fatal("accepted relative home")
	}
}

func TestEnvironmentHomes(t *testing.T) {
	base := t.TempDir()
	skillhubHome := filepath.Join(base, "skillhub")
	codexHome := filepath.Join(base, "codex-state")
	if err := os.MkdirAll(codexHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SKILLHUB_HOME", skillhubHome)
	t.Setenv("CODEX_HOME", codexHome)
	var out bytes.Buffer
	a, err := New(&out, &out, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if a.Home != skillhubHome || a.CodexHome != codexHome {
		t.Fatalf("wrong homes: %#v", a)
	}
	if err := a.selectAgent(""); err != nil {
		t.Fatal("CODEX_HOME not used for detection:", err)
	}
	put(t, filepath.Join(codexHome, "skills", "existing", "SKILL.md"), skillText("existing", "body"))
	items, err := a.discover()
	if err != nil {
		t.Fatal(err)
	}
	var seen bool
	for _, item := range items {
		if item.Name == "existing" && item.Status == "publishable" {
			seen = true
		}
	}
	if !seen {
		t.Fatal("CODEX_HOME not used for scanning")
	}
}

func TestDefaultScopeOutsideGitProject(t *testing.T) {
	a, _, _, _ := setup(t)
	outside := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(outside); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	link, scope, err := a.destination(options{}, "review")
	if err != nil {
		t.Fatal(err)
	}
	if scope != "global" || link != filepath.Join(a.UserHome, ".agents", "skills", "review") {
		t.Fatalf("unexpected default target: %s %s", scope, link)
	}
}

func TestSyncProtectsInstalledLinksAndRepair(t *testing.T) {
	requireSymlink(t)
	a, seed, project, _ := setup(t)
	if err := a.install("review", options{project: project}); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(project, ".agents", "skills", "review")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := a.repair(); err != nil {
		t.Fatal(err)
	}
	if _, err := linkTarget(link); err != nil {
		t.Fatal("repair failed:", err)
	}
	gitTest(t, seed, "rm", "-r", "skills/review")
	gitTest(t, seed, "commit", "-m", "remove skill")
	gitTest(t, seed, "push")
	before := gitTest(t, filepath.Join(a.Home, "repo"), "rev-parse", "HEAD")
	if err := a.repoSync(); err == nil {
		t.Fatal("accepted removal of installed skill")
	}
	after := gitTest(t, filepath.Join(a.Home, "repo"), "rev-parse", "HEAD")
	if before != after {
		t.Fatal("sync changed checkout despite installed skill deletion")
	}
}

func TestRepairAfterMovingSkillhubHome(t *testing.T) {
	requireSymlink(t)
	a, _, project, _ := setup(t)
	if err := a.install("review", options{project: project}); err != nil {
		t.Fatal(err)
	}
	oldHome := a.Home
	newHome := filepath.Join(filepath.Dir(oldHome), "moved-state")
	if err := os.Rename(oldHome, newHome); err != nil {
		t.Fatal(err)
	}
	a.Home = newHome
	if err := a.repair(); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(project, ".agents", "skills", "review")
	if target, err := linkTarget(link); err != nil || target != filepath.Join(newHome, "repo", "skills", "review") {
		t.Fatalf("wrong repaired target: %s %v", target, err)
	}
	if err := a.remove("review", options{project: project}); err != nil {
		t.Fatal("repair did not persist manifest:", err)
	}
}

func TestSyncRejectsInvalidRemoteBeforeCheckout(t *testing.T) {
	a, seed, _, _ := setup(t)
	put(t, filepath.Join(seed, "skills", "review", "extra-link"), "placeholder")
	gitTest(t, seed, "add", ".")
	gitTest(t, seed, "commit", "-m", "valid update")
	gitTest(t, seed, "push")
	if err := a.repoSync(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(seed, "skills", "review", "extra-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("SKILL.md", filepath.Join(seed, "skills", "review", "extra-link")); err != nil {
		t.Skip("symlink unavailable:", err)
	}
	gitTest(t, seed, "add", ".")
	gitTest(t, seed, "commit", "-m", "bad symlink")
	gitTest(t, seed, "push")
	before := gitTest(t, filepath.Join(a.Home, "repo"), "rev-parse", "HEAD")
	if err := a.repoSync(); err == nil {
		t.Fatal("accepted symlink in remote skill")
	}
	if got := gitTest(t, filepath.Join(a.Home, "repo"), "rev-parse", "HEAD"); got != before {
		t.Fatal("invalid remote changed checkout")
	}
}

func TestEmptyRepoSetAndFirstPublish(t *testing.T) {
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	gitTest(t, base, "init", "--bare", "--initial-branch=main", remote)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url.file://"+remote+".insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "https://github.com/example/skills.git")
	home := filepath.Join(base, "state")
	codex := filepath.Join(base, "codex")
	put(t, filepath.Join(codex, "skills", "fresh", "SKILL.md"), skillText("fresh", "first"))
	var out bytes.Buffer
	a := &App{Home: home, UserHome: filepath.Join(base, "user"), CodexHome: codex, Out: &out, Err: &out, In: strings.NewReader("y\n")}
	if err := a.Run([]string{"repo", "set", "https://github.com/example/skills.git"}, "test"); err != nil {
		t.Fatal(err)
	}
	gitTest(t, filepath.Join(home, "repo"), "config", "user.name", "Skillhub Test")
	gitTest(t, filepath.Join(home, "repo"), "config", "user.email", "skillhub@example.test")
	if err := a.publish(filepath.Join(codex, "skills", "fresh"), true); err != nil {
		t.Fatal(err)
	}
	if got := gitTest(t, remote, "rev-parse", "main"); got == "" {
		t.Fatal("first commit not pushed")
	}
}

func requireSymlink(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(source, link); err != nil {
		t.Skip("directory symlink creation unavailable:", err)
	}
}

func TestChangedManagedLinkIsProtected(t *testing.T) {
	requireSymlink(t)
	a, _, project, _ := setup(t)
	if err := a.install("review", options{project: project}); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(project, ".agents", "skills", "review")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(project, link); err != nil {
		t.Fatal(err)
	}
	if err := a.remove("review", options{project: project}); err == nil {
		t.Fatal("removed modified symlink")
	}
	if err := a.repair(); err == nil {
		t.Fatal("repaired over modified symlink")
	}
}

func TestPublishPushFailureKeepsCommitForRetry(t *testing.T) {
	a, seed, _, _ := setup(t)
	remote := gitTest(t, seed, "remote", "get-url", "origin")
	hook := filepath.Join(remote, "hooks", "pre-receive")
	put(t, hook, "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(hook, 0755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(a.CodexHome, "skills", "new-skill")
	put(t, filepath.Join(source, "SKILL.md"), skillText("new-skill", "hello"))
	before := gitTest(t, remote, "rev-parse", "main")
	if err := a.publish(source, true); err == nil || !strings.Contains(err.Error(), "repo push") {
		t.Fatalf("expected retry guidance, got %v", err)
	}
	local := gitTest(t, filepath.Join(a.Home, "repo"), "rev-parse", "HEAD")
	if local == before {
		t.Fatal("local commit lost after rejected push")
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	if err := a.repoPush(); err != nil {
		t.Fatal(err)
	}
	if got := gitTest(t, remote, "rev-parse", "main"); got != local {
		t.Fatal("retry did not publish local commit")
	}
}

func TestEmptyCloneSyncsRemoteFirstCommit(t *testing.T) {
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	gitTest(t, base, "init", "--bare", "--initial-branch=main", remote)
	clone := filepath.Join(base, "clone")
	gitTest(t, base, "clone", remote, clone)
	gitTest(t, clone, "config", "user.name", "Skillhub Test")
	gitTest(t, clone, "config", "user.email", "skillhub@example.test")
	a := &App{Home: base, UserHome: base, CodexHome: filepath.Join(base, "codex"), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, In: strings.NewReader("")}
	put(t, filepath.Join(base, "config.json"), `{"repo_url":"https://github.com/example/skills.git"}`)
	if err := os.Rename(clone, filepath.Join(base, "repo")); err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(base, "seed")
	if err := os.Mkdir(seed, 0755); err != nil {
		t.Fatal(err)
	}
	gitTest(t, seed, "init", "-b", "main")
	gitTest(t, seed, "config", "user.name", "Skillhub Test")
	gitTest(t, seed, "config", "user.email", "skillhub@example.test")
	put(t, filepath.Join(seed, "skills", "first", "SKILL.md"), skillText("first", "body"))
	gitTest(t, seed, "add", ".")
	gitTest(t, seed, "commit", "-m", "first")
	gitTest(t, seed, "remote", "add", "origin", remote)
	gitTest(t, seed, "push", "-u", "origin", "main")
	if err := a.repoSync(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "repo", "skills", "first", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}
