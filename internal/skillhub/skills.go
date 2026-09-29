package skillhub

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var skillName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func validName(name string) bool { return len(name) <= 64 && skillName.MatchString(name) }

func validateSkill(path string) error {
	name := filepath.Base(path)
	if !validName(name) {
		return fmt.Errorf("invalid skill directory name %q", name)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("skill directory missing: %s: %w", path, err)
	}
	st, err := os.Stat(resolved)
	if err != nil || !st.IsDir() {
		return fmt.Errorf("skill directory missing: %s", path)
	}
	data, err := os.ReadFile(filepath.Join(resolved, "SKILL.md"))
	if err != nil {
		return fmt.Errorf("%s requires SKILL.md: %w", name, err)
	}
	meta, err := frontmatter(data)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if meta["name"] != name {
		return fmt.Errorf("%s: frontmatter name must match the directory name", name)
	}
	if meta["description"] == "" {
		return fmt.Errorf("%s: frontmatter description is required", name)
	}
	return filepath.WalkDir(resolved, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("nested symlink is not allowed: %s", p)
		}
		if d.Name() == ".git" {
			return fmt.Errorf("nested .git is not allowed: %s", p)
		}
		if !d.IsDir() && !d.Type().IsRegular() {
			return fmt.Errorf("unsupported file type: %s", p)
		}
		return nil
	})
}

// Only two scalar keys are needed. Avoid interpreting YAML values or executing
// content; Codex remains the authority for full skill metadata validation.
func frontmatter(data []byte) (map[string]string, error) {
	s := bufio.NewScanner(strings.NewReader(string(data)))
	s.Buffer(make([]byte, 4096), 1024*1024)
	if !s.Scan() || strings.TrimSpace(s.Text()) != "---" {
		return nil, errors.New("SKILL.md needs YAML frontmatter")
	}
	meta := make(map[string]string)
	var folded string
	for s.Scan() {
		line := s.Text()
		if strings.TrimSpace(line) == "---" {
			return meta, nil
		}
		if folded != "" {
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
				meta[folded] += " " + strings.TrimSpace(line)
				continue
			}
			folded = ""
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key != "name" && key != "description" {
			continue
		}
		if value == "|" || value == ">" || value == "|-" || value == ">-" {
			folded = key
			meta[key] = ""
		} else {
			meta[key] = strings.Trim(value, `"'`)
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("SKILL.md frontmatter is not closed")
}

func listSkills(repo string) ([]string, error) {
	root := filepath.Join(repo, "skills")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil, fmt.Errorf("skills/ contains non-directory %s", entry.Name())
		}
		if err := validateSkill(filepath.Join(root, entry.Name())); err != nil {
			return nil, err
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func copySkill(src, dst string) error {
	if err := validateSkill(src); err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(src)
	if err != nil {
		return err
	}
	return filepath.WalkDir(resolved, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(resolved, path)
		if err != nil {
			return err
		}
		to := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(to, 0755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported file: %s", path)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			_ = in.Close()
			return err
		}
		copyErr := copyFile(to, in, info.Mode())
		closeErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func skillFiles(src string) ([]string, error) {
	var files []string
	resolved, err := filepath.EvalSymlinks(src)
	if err != nil {
		return nil, err
	}
	err = filepath.WalkDir(resolved, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(resolved, path)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	return files, err
}

func appendLine(w io.Writer, fields ...string) {
	fmt.Fprintln(w, strings.Join(fields, "\t"))
}
