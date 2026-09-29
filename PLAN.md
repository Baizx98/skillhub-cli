# skillhub：首版实现方案

## 目标与范围

`skillhub` 管理一个用户指定的 GitHub skill 仓库：克隆到本机、将其中的 skill 以目录 symlink 安装给 Codex、扫描已有 skill，并把选中的本地 skill 提交和推送到该仓库。支持 Windows、Linux、macOS。首版只实现 Codex；命令的默认 Agent 语义是“所有已安装且受支持的 Agent”，因此首版通常就是 Codex，后续加 Agent 不必改命令习惯。

不做自动发布、后台同步、多个托管仓库、skill 依赖或图形界面。私有仓库使用本机 Git 的 SSH/凭据设置，`skillhub` 不保存令牌。

## Go 与 Python 的取舍

| 维度 | Go | Python |
| --- | --- | --- |
| 跨平台安装 | 各平台发布一个可执行文件；用户仍需 Git | `pipx` 安装简单，但要求匹配版本的 Python；独立打包另需工具和逐平台构建 |
| 文件、Git 操作 | 标准库足够，调用系统 Git | 标准库同样足够，原型开发更快 |
| 后续维护 | 编译和类型检查适合路径、状态和错误分支较多的 CLI | 开发门槛较低，但运行环境更分散 |

**选择 Go。**这里的主要目标是让三种操作系统的用户直接下载 `skillhub` 使用；Python 的开发速度优势不足以抵消运行环境与分发差异。symlink 的 Windows 权限限制属于操作系统限制，换语言也无法消除。[Go 构建说明](https://go.dev/doc/tutorial/compile-install)、[Python symlink 文档](https://docs.python.org/3/library/os.html#os.symlink)、[Microsoft symlink 文档](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-createsymboliclinkw)。当前开发机尚无 Go 工具链，编码阶段需先安装。

## 仓库与本地状态

```text
用户的 GitHub 仓库/                 SKILLHUB_HOME（默认 ~/.skillhub）/
└── skills/                           ├── repo/          # 唯一仓库的完整 Git clone
    ├── code-review/                  ├── config.json    # 仓库 URL 等设置
    │   └── SKILL.md                  ├── installs.json  # 受管理的 symlink 清单
    └── paper-writing/               └── history.jsonl  # 操作时间、对象、结果、提交 SHA
        └── SKILL.md
```

启动时先读取 `SKILLHUB_HOME`：若设置，要求它是绝对路径，并使用该目录；否则使用当前用户的 `~/.skillhub`。配置、仓库、安装清单、历史及临时文件全部保存在其中；项目内只创建指向 `repo/skills/<name>` 的 symlink。`SKILLHUB_HOME` 搬迁后绝对 symlink 会失效，需提供 `skillhub repair` 重建受管理链接；仓库 clone 不能被自动删除或移动。

仓库中的每个 `skills/<name>/` 必须有 `SKILL.md`，其中应包含 Codex 所需的 `name`、`description`。首版校验这些字段和目录名；名称限制为小写字母、数字和连字符，且 `name` 须与目录名一致。拒绝仓库 skill 内的 symlink、子模块及路径逃逸，避免上传或安装时把仓库外文件带入。CLI 不执行 skill 中的脚本。

## 作用域与 Agent 选择

- 无作用域参数时，从当前目录向上查找 Git 工作区；找到则使用 Git 根目录的 `.agents/skills/<name>`。若不在 Git 项目中，默认安装到当前用户的 `~/.agents/skills/<name>`。非 Git 项目可用 `--project <dir>` 明确指定项目根目录。
- 若自动检测到的项目位于 `SKILLHUB_HOME` 内，则报错并要求显式指定目标，避免把 skill 链接装回托管仓库。
- `--global` 总是安装到当前用户的 `~/.agents/skills/<name>`；`--project <dir>` 总是安装到指定的现有项目目录。二者互斥，不写入系统管理员目录。
- 不传 `--agent` 时，对所有检测到的受支持 Agent 执行。首版只有 `codex`，检测条件为 `codex` 可执行文件在 PATH 中，或用户已有 `~/.codex` 目录；`--agent codex` 可显式选择。没有检测结果时报告原因，不静默成功。
- 后续增加 Agent 时，只需扩展探测与目标目录映射。首版不会为其他 Agent 创建任何目录或链接。

Codex 官方文档列出项目级与用户级 `.agents/skills`，并确认支持指向 skill 文件夹的 symlink：[OpenAI Docs：Build skills](https://learn.chatgpt.com/docs/build-skills)。项目中的 symlink 指向用户主目录，通常不可移植；命令输出提醒不要把该链接提交到项目仓库，CLI 不擅自改 `.gitignore`。

## 命令设计

```text
skillhub repo set <github-https-or-ssh-url>  # 首次克隆到 SKILLHUB_HOME/repo
skillhub repo sync                           # fetch + fast-forward；已安装链接随工作树更新
skillhub repo push                           # 重试推送本地已提交的发布内容
skillhub list                                # 仓库中的 skill 与提交 SHA
skillhub install <name>                      # 在项目内默认装到本项目，否则装到全局
skillhub install <name> --global             # 明确装到当前用户全局
skillhub install <name> --project <dir>      # 明确装到指定项目
skillhub install <name> --agent codex        # 明确选择 Codex
skillhub installed                           # 已管理的链接及其状态
skillhub remove <name> [--global | --project <dir>]
skillhub repair                              # 核对并修复受管理的链接
skillhub scan                                # 列出本项目和用户目录下已有的 Codex skill
skillhub publish <path-to-skill>             # 将扫描到的单个 skill 提交并推送
skillhub --version
```

`scan` 只读取当前项目（若存在）和用户级 `.agents/skills/`，同时读取已有的 `.codex/skills/` 作为迁移来源；不扫描管理员或系统内置 skill。结果显示真实路径、名称及“已管理/可发布/无效”状态。`publish` 要求显式给出 `scan` 列出的具体路径，不自动批量上传；对已指向本仓库的链接跳过，对同名仓库 skill 报冲突，不暗中覆盖。上传前显示将新增的文件列表，确认后复制到 `repo/skills/<name>`，执行 `git add`、提交、推送；不提交扫描目录中的其他文件。

## 安全与同步规则

- 使用系统 Git 和参数数组，不通过 shell 拼接命令；仅接受 `github.com` 的 HTTPS 或 SSH 仓库地址，拒绝 URL 内嵌凭据。`repo set` 仅用于首次配置，同一仓库可重复执行；已有安装项时拒绝改成另一仓库，以免链接悄然指向新内容。
- 所有安装项使用**指向目录的绝对 symlink**。创建前核对来源存在；若目标已有普通目录、文件或非本工具管理的链接，报冲突且不覆盖。移除或修复前用 `Lstat` 核对链接仍指向清单记录的目标，只删除或替换该链接本身。
- `repo sync` 要求仓库工作树干净，使用 `fetch` 和 fast-forward，不做 `reset --hard`。更新前检查远端仍包含已安装的 skill；否则报出会失效的链接并中止。symlink 指向稳定的仓库路径，因此同步成功后立即呈现新内容，无需重装。
- `publish` 先同步、检查同名冲突与待复制内容，再复制、提交、推送。推送失败时保留本地提交，记入历史并提示 `repo push`；不自动改写历史或强推。只有显式 `publish` 才会向远端写入。
- Windows 的目录 symlink 需要开发者模式或相应权限；创建失败时给出设置提示，不悄悄降级为复制或 junction。跨盘链接使用绝对路径；Windows CI 验证实际可创建链接的环境，权限不足的错误路径通过隔离测试验证。

## 实施顺序与验收

1. Go CLI、`SKILLHUB_HOME`、配置/历史、仓库克隆与同步、skill 格式校验。
2. 项目探测、Codex 探测、symlink 安装/移除/修复、清单保护。
3. 扫描、单 skill 发布、Git 提交/推送失败恢复、README 与发布包。

用临时 Git 仓库及隔离的 `SKILLHUB_HOME` 做端到端测试：项目内默认安装、非项目默认全局、`--global` 覆盖默认作用域、显式 Agent、链接冲突、断链修复、同步更新、远端删除保护、扫描既有 skill、同名发布冲突和推送失败重试。CI 在 Windows、Linux、macOS 的 amd64 上运行测试，再构建常见 arm64 包；发布包提供 SHA256 校验。性能无需专门优化，正确性重点是路径选择、symlink 所有权和 Git 失败后的可恢复性。
