package skillhub

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type App struct {
	Home      string
	UserHome  string
	CodexHome string
	Out       io.Writer
	Err       io.Writer
	In        io.Reader
}

func New(out, errOut io.Writer, in io.Reader) (*App, error) {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	home := os.Getenv("SKILLHUB_HOME")
	if home == "" {
		home = filepath.Join(userHome, ".skillhub")
	} else if !filepath.IsAbs(home) {
		return nil, fmt.Errorf("SKILLHUB_HOME must be an absolute path: %q", home)
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(userHome, ".codex")
	} else if !filepath.IsAbs(codexHome) {
		return nil, fmt.Errorf("CODEX_HOME must be an absolute path: %q", codexHome)
	}
	return &App{Home: filepath.Clean(home), UserHome: userHome, CodexHome: filepath.Clean(codexHome), Out: out, Err: errOut, In: in}, nil
}

type options struct {
	global  bool
	project string
	agent   string
	yes     bool
	args    []string
}

func parseOptions(args []string) (options, error) {
	var o options
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--global":
			o.global = true
		case "--yes":
			o.yes = true
		case "--project", "--agent":
			if i+1 >= len(args) {
				return o, fmt.Errorf("%s needs a value", args[i])
			}
			i++
			if args[i-1] == "--project" {
				o.project = args[i]
			} else {
				o.agent = args[i]
			}
		default:
			if strings.HasPrefix(args[i], "-") {
				return o, fmt.Errorf("unknown option %s", args[i])
			}
			o.args = append(o.args, args[i])
		}
	}
	if o.global && o.project != "" {
		return o, errors.New("--global and --project cannot be combined")
	}
	return o, nil
}

func (a *App) Run(argv []string, version string) error {
	if len(argv) == 0 || argv[0] == "help" || argv[0] == "--help" {
		fmt.Fprint(a.Out, help)
		return nil
	}
	if argv[0] == "--version" || argv[0] == "version" {
		fmt.Fprintln(a.Out, "skillhub", version)
		return nil
	}
	command := argv[0]
	args := argv[1:]
	if command == "repo" {
		if len(args) == 0 {
			return errors.New("expected repo set, sync, or push")
		}
		command = "repo " + args[0]
		args = args[1:]
	}
	o, err := parseOptions(args)
	if err != nil {
		return err
	}
	if o.yes && command != "publish" {
		return errors.New("--yes is only valid for publish")
	}
	mutating := command == "repo set" || command == "repo sync" || command == "repo push" || command == "install" || command == "remove" || command == "repair" || command == "publish"
	if mutating {
		if err := a.ensureHome(); err != nil {
			return err
		}
	}
	var commit string
	switch command {
	case "repo set":
		err = exact(o, 1, false)
		if err == nil {
			err = a.repoSet(o.args[0])
		}
	case "repo sync":
		err = exact(o, 0, false)
		if err == nil {
			err = a.repoSync()
		}
	case "repo push":
		err = exact(o, 0, false)
		if err == nil {
			err = a.repoPush()
		}
	case "list":
		err = exact(o, 0, false)
		if err == nil {
			err = a.list()
		}
	case "install", "remove":
		err = exact(o, 1, true)
		if err == nil {
			err = a.selectAgent(o.agent)
		}
		if err == nil {
			if command == "install" {
				err = a.install(o.args[0], o)
			} else {
				err = a.remove(o.args[0], o)
			}
		}
	case "installed":
		err = exact(o, 0, false)
		if err == nil {
			err = a.installed()
		}
	case "repair":
		err = exact(o, 0, false)
		if err == nil {
			err = a.repair()
		}
	case "scan":
		err = exact(o, 0, false)
		if err == nil {
			err = a.scan()
		}
	case "publish":
		err = exact(o, 1, false)
		if err == nil {
			err = a.publish(o.args[0], o.yes)
		}
	default:
		return fmt.Errorf("unknown command %q; run skillhub help", command)
	}
	if mutating {
		if head, headErr := a.head(); headErr == nil {
			commit = head
		}
		object := strings.Join(o.args, " ")
		a.history(command, object, commit, err)
	}
	return err
}

func exact(o options, count int, scopes bool) error {
	if len(o.args) != count {
		return fmt.Errorf("expected %d argument(s), got %d", count, len(o.args))
	}
	if !scopes && (o.global || o.project != "" || o.agent != "") {
		return errors.New("scope and agent flags are not valid for this command")
	}
	return nil
}

func (a *App) selectAgent(agent string) error {
	if agent != "" && agent != "codex" {
		return fmt.Errorf("unsupported agent %q (v1 supports codex)", agent)
	}
	if agent == "codex" {
		return nil
	}
	if _, err := exec.LookPath("codex"); err == nil {
		return nil
	}
	if st, err := os.Stat(a.CodexHome); err == nil && st.IsDir() {
		return nil
	}
	return fmt.Errorf("no supported installed agent found: codex is not in PATH and Codex home %s does not exist; use --agent codex to select it explicitly", a.CodexHome)
}

const help = `skillhub manages personal Codex skills from one GitHub repository.

  skillhub repo set <github-url>       Clone the skill repository
  skillhub repo sync                   Fast-forward the local clone
  skillhub repo push                   Retry pending local commits
  skillhub list                        List repository skills
  skillhub install <name> [--global | --project DIR] [--agent codex]
  skillhub remove <name>  [--global | --project DIR] [--agent codex]
  skillhub installed                   List managed symlinks
  skillhub repair                      Repair managed symlinks after moving SKILLHUB_HOME
  skillhub scan                        Discover existing local skills
  skillhub publish <skill-path> [--yes] Commit and push one discovered skill
  skillhub --version

Install/remove default to the current Git project's root, or user scope
outside a Git project. SKILLHUB_HOME defaults to ~/.skillhub.
`
