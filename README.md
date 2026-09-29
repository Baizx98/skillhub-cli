# skillhub

`skillhub` 从个人 GitHub 仓库管理 Codex skills。首版支持 Windows、Linux、macOS，使用本机 Git 和目录 symlink；安装后的 skill 始终指向本机克隆，因此 `repo sync` 后会读取仓库的新版本。

## 安装

从本项目的 GitHub Releases 下载对应系统的 `skillhub`（Windows 为 `skillhub.exe`），校验 `SHA256SUMS` 后放到 `PATH` 中。也可以安装 Go 1.25+ 后在本项目运行：

```sh
go install ./cmd/skillhub
```

`go install` 会把可执行文件放到 `GOBIN`，未设置时放到 `$(go env GOPATH)/bin`；该目录还必须在 `PATH` 中。在 Linux/macOS 的默认 Go 配置下，可先在当前终端执行：

```sh
export PATH="$(go env GOPATH)/bin:$PATH"
skillhub --version
```

需要长期生效时，把 `export PATH=...` 一行加入所用 shell 的启动文件（例如 `~/.zshrc`）。Windows 用户可用 `go env GOBIN` / `go env GOPATH` 找到安装目录，再将它加入用户级 `Path` 环境变量。若 `GOBIN` 已设置，应将 `GOBIN` 对应目录加入 `PATH`。

运行时需有 Git。Windows 创建目录 symlink 需要开启“开发者模式”或使用具备符号链接权限的账户；权限不足时，`skillhub` 会报错，不会改为复制安装。

## 准备个人仓库

仓库中每个 `skills/<name>/` 都需要 `SKILL.md`，其中的 `name` 必须与目录名一致：

```text
skills/
└── code-review/
    ├── SKILL.md
    └── references/
```

```markdown
---
name: code-review
description: Review code changes and identify concrete bugs.
---

Review the changed code and report actionable findings.
```

仓库也可以先为空；发布第一个本地 skill 时会创建 `skills/` 并推送首次提交。仓库地址接受 `https://github.com/owner/repo.git` 或 `git@github.com:owner/repo.git`。私有仓库沿用本机 Git 的认证设置。

## 常用命令

```sh
skillhub repo set git@github.com:you/my-skills.git
skillhub list
skillhub install code-review
skillhub install code-review --global
skillhub repo sync
skillhub installed
skillhub scan
skillhub publish /path/to/existing-skill
skillhub remove code-review
```

在 Git 项目内运行 `install` 或 `remove` 时，默认作用于该项目根目录的 `.agents/skills/`；在 Git 项目外，默认作用于用户级 `~/.agents/skills/`。`--global` 强制使用用户级目录，`--project <dir>` 指定一个已有的项目目录。首版只支持 Codex；默认会检测 PATH 中的 `codex` 或 Codex 主目录，`--agent codex` 可显式选择。

`scan` 查看当前 Git 项目、用户级 `.agents/skills/` 和 Codex 主目录下 `skills/` 中的已有 skill。`publish` 接受扫描结果中的具体路径，显示文件列表并要求确认；脚本调用可加 `--yes`。同名仓库 skill 不会被覆盖。推送失败后，本地提交会保留，可用 `skillhub repo push` 重试。

## 本地目录与注意事项

`SKILLHUB_HOME` 优先于默认的 `~/.skillhub`；它必须是绝对路径。该目录存放 Git clone (`repo/`)、`config.json`、`installs.json` 和 `history.jsonl`。`CODEX_HOME` 优先于 `~/.codex`，用于检测 Codex 和扫描已有 skill；安装目标仍采用 Codex 的 `.agents/skills/` 目录。移动 `SKILLHUB_HOME` 后运行 `skillhub repair` 重建受管理链接。

项目中的 symlink 指向本机的 `SKILLHUB_HOME`，不适合提交到共享项目仓库。工具不会修改项目 `.gitignore`，也不会覆盖或删除不归自己管理的文件。`repo sync` 只接受 Git fast-forward；远端删除已安装 skill 时，会停止同步并提示先移除相关链接。

更多设计与验收范围见 [PLAN.md](PLAN.md)。Codex 的目录及 symlink 支持以 [OpenAI Docs](https://learn.chatgpt.com/docs/build-skills) 为准。
