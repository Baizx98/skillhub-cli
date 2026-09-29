# SkillHub CLI：第一版实现方案

## 目标与边界

用户维护一个 Git 仓库，CLI 将仓库中的 skill 安装到本机 Agent 的用户级目录，或指定项目的目录。第一版支持 Windows、Linux、macOS，以及 Codex、Claude Code、Cursor、Gemini CLI。CLI 只管理由自己安装的文件，不修改其他来源的 skill。

第一版只配置一个仓库和其默认分支；不提供仓库发布、skill 创建、依赖解析、自动定时同步、Agent 自动探测或图形界面。私有仓库复用本机 Git 已有的 SSH/凭据配置，CLI 不保存令牌。

## 仓库格式

```text
my-skills/
└── skills/
    ├── code-review/
    │   ├── SKILL.md
    │   └── references/
    └── paper-writing/
        ├── SKILL.md
        └── scripts/
```

- `skills/` 下每个一级目录是一个 skill；子目录整体复制。`SKILL.md` 必须存在。建议遵循 Agent Skills 格式，在 YAML 元数据中写明与目录一致的 `name` 和清晰的 `description`；首版只检查文件存在，让目标 Agent 验证内容。
- 第一版名称限制为小写字母、数字和连字符，拒绝重复名称、路径穿越及 skill 内的符号链接。仓库根目录的其他文件不会被安装。
- CLI 不执行仓库中的脚本。安装前显示仓库地址、skill 名称、目标路径；用户应信任其自行配置的来源。

## 安装目标

`--scope global` 使用当前用户主目录；`--scope project` 使用 `--project` 指定的现有目录，默认当前工作目录。这里的“全局”指当前用户的所有本地项目，不需要管理员权限。所有路径通过平台 API 拼接，不手写 `/` 或假定 Windows 盘符。

| Agent 参数 | 用户级 | 项目级 |
| --- | --- | --- |
| `codex` | `~/.agents/skills/<name>/` | `<project>/.agents/skills/<name>/` |
| `claude` | `~/.claude/skills/<name>/` | `<project>/.claude/skills/<name>/` |
| `cursor` | `~/.cursor/skills/<name>/` | `<project>/.cursor/skills/<name>/` |
| `gemini` | `~/.gemini/skills/<name>/` | `<project>/.gemini/skills/<name>/` |

为每个 Agent 使用自己的目录，避免同名 skill 在共享目录中发生来源或优先级冲突。上表仅针对本机 Agent；云端会话是否读取这些文件由各 Agent 的同步设置决定。

## 命令界面

```text
skillhub repo set <https-or-ssh-git-url>    # 配置仓库并拉取默认分支
skillhub repo sync                          # 拉取最新快照；不自动修改已安装 skill
skillhub list                               # 列出仓库 skill 及当前快照提交
skillhub install <name> --agent codex --scope global
skillhub install <name> --agent claude --scope project --project <path>
skillhub installed                          # 列出 CLI 管理的安装项及目标路径
skillhub remove <name> --agent claude --scope project --project <path>
skillhub --version
```

`--agent` 必填，接受上述四个值；安装多个 Agent 时重复调用命令，保持单次操作和错误处理简单。`repo sync` 与 `install` 分开，用户可以先看更新内容再安装。再次执行 `install` 可更新同一个受管理安装项；默认只有目标内容与上次安装记录一致时才覆盖。若目标是用户自行修改的内容，命令报错并提示先备份；`--force` 仅允许覆盖**同一个受管理安装项**，不能覆盖未受管理的同名目录。`remove` 同样拒绝删除已修改的内容，且绝不删除未受管理目录。Claude Code 的保留名称在安装到 `claude` 时拒绝。

## 本地状态与实现

- 采用 Go 标准库开发，调用系统 `git` 命令克隆仓库；发布单文件可执行程序，用户需预装 Git。Go 的 `os.UserConfigDir`、`os.UserCacheDir`、`os.UserHomeDir` 负责跨平台目录定位。
- 配置保存仓库 URL；缓存保存克隆快照与提交 SHA；安装清单保存 Agent、scope、绝对目标路径、skill 名、来源提交和安装内容摘要。配置和缓存位于各平台的用户配置/缓存目录下，不放进目标项目。
- `repo set` 和 `repo sync` 先克隆到临时目录，验证 skill 后再替换缓存；失败保留旧配置与快照。`install` 先复制到目标父目录下的临时目录，核对后再切换。任何失败都应保留或恢复原安装项。
- 调用 `git` 时使用参数数组而非 shell；限制仓库 URL 为 HTTPS 或 Git SSH 格式。复制时拒绝符号链接，并确认所有源文件仍位于 skill 目录内。错误消息包含可处理的原因与路径。
- 代码先分成 `cmd/skillhub`（命令入口）和 `internal/` 下的仓库、skill、安装目标及状态模块；不引入插件系统或大型配置框架。

## 实施顺序与验收

1. 仓库格式校验、配置和原子拉取；用公开与本地临时 Git 仓库验证 `set/sync/list`。
2. 四种 Agent 路径映射、复制安装、安装清单和安全移除；覆盖用户级/项目级、重装、冲突和修改后保护。
3. README 示例、帮助输出、版本号及 GitHub Actions 多平台构建：Windows、Linux、macOS 的 amd64；交叉编译 Windows/Linux/macOS arm64。按版本标签发布压缩包和 SHA256 校验文件，Windows 包含 `.exe`。

验收以真实临时 Git 仓库和临时 HOME/项目目录做端到端测试：首次安装、仓库更新后重装、离线使用缓存、未受管理目录冲突、用户修改保护、错误 URL、缺失 `SKILL.md`、符号链接拒绝。三个系统的 CI 分别运行测试和构建。首版不声称 Agent 会自动重载已安装 skill；必要时按 Agent 自身机制刷新或重新启动。

## 方案取舍

预计收益：单个仓库即可把相同 skill 分发到四类 Agent，单文件程序和复制安装降低跨平台维护成本。主要风险是 Agent 以后修改目录约定，以及复制安装后源仓库和目标目录产生版本差异；路径集中在映射表中，并在安装清单记录提交与摘要，便于后续增加 `update` 或 `status`。最小验证是上述跨平台 CI 加一次每种 Agent 目标目录的手动发现检查。

## 目录依据

- [OpenAI Docs：Codex 本地 skill 目录](https://learn.chatgpt.com/docs/build-skills)
- [Claude Code：Skills](https://code.claude.com/docs/en/skills)
- [Cursor：Skills](https://prod.cursor.com/help/customization/skills)
- [Gemini CLI：Managing Agent Skills](https://geminicli.com/docs/cli/using-agent-skills/)
