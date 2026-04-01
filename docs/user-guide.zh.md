# User Guide

[English](user-guide.md)

这份文档面向日常使用 QuanCode 的用户，按真实工作流讲每个命令该怎么用。

README 继续负责项目介绍和快速开始；这里负责更完整的命令说明、示例和排障入口。

## 1. 初始化

### `quancode init`

第一次安装后，先运行：

```bash
quancode init
```

它会做这些事：

- 扫描 `PATH` 中已安装的已知 coding CLI，例如 `claude`、`codex`、`qodercli`
- 让你选择哪个 CLI 作为默认 primary agent
- 把配置写入 `~/.config/quancode/quancode.yaml`

如果配置文件已经存在，`init` 会先询问是否覆盖。

### `quancode doctor`

在 `init` 之后，或者你改了配置/环境之后，运行：

```bash
quancode doctor
```

它会检查：

- 配置文件是否存在
- 配置能否正确加载和校验
- primary agent 命令是否在 `PATH` 中
- 各个已启用 agent 命令是否可执行
- `quancode` 自己是否在 `PATH` 中

如果有失败项，`doctor` 会打印下一步建议。它也可能针对你当前 shell 打印一条 shell completion 启用提示。

### Shell Completion

QuanCode 已经内置了通过 Cobra 生成 shell completion 的能力：

```bash
quancode completion zsh
quancode completion bash
quancode completion fish
```

快速启用示例：

```bash
# zsh
echo 'source <(quancode completion zsh)' >> ~/.zshrc

# bash
echo 'source <(quancode completion bash)' >> ~/.bashrc

# fish
quancode completion fish > ~/.config/fish/completions/quancode.fish
```

添加后重新开一个 shell，或者在当前会话里手动 source 对应脚本。

如果你之后通过 Homebrew 安装 QuanCode，在 tap 接线完成后，生成的 formula 会自动安装 shell completion。

## 2. 启动主 Agent

### `quancode start`

启动配置中的默认 primary agent：

```bash
quancode start
```

临时指定 primary agent：

```bash
quancode start --primary codex
quancode start --primary claude
```

`start` 运行时会发生的事情：

- QuanCode 读取配置
- 构建 delegation 指令，只列出启用的非 primary agents
- 按该 primary CLI 的 prompt 模式注入这些指令
- 启动 primary CLI

如果 primary 使用基于文件的 prompt 注入，比如 `AGENTS.md`，QuanCode 会在本次会话中托管它，并在 primary 退出后恢复原始内容。

## 3. 委派任务

### `quancode delegate`

用 `delegate` 执行 one-shot 子 agent 任务：

```bash
quancode delegate "write unit tests for config loading"
```

指定目标 agent：

```bash
quancode delegate --agent claude "review this patch for regressions"
quancode delegate --agent codex "refactor this helper and update tests"
quancode delegate --agent qoder "review this code for issues"
```

指定工作目录：

```bash
quancode delegate --workdir /path/to/repo "explain the runner package"
```

选择输出格式：

```bash
quancode delegate --format text "summarize the repo structure"
quancode delegate --format json "review this change"
```

`text` 模式更适合终端直接看，`json` 模式更适合脚本和自动化。

### 隔离模式

QuanCode 支持三种 delegation 模式：

- `inplace`：直接在当前工作树里运行
- `worktree`：在临时 git worktree 中运行，完成后把 patch 应用回主工作树
- `patch`：在临时 git worktree 中运行，只返回 patch，不直接改主工作树

示例：

```bash
quancode delegate --isolation inplace "fix this lint issue"
quancode delegate --isolation worktree "implement the helper and update tests"
quancode delegate --isolation patch "rewrite the README opening paragraph"
```

选择建议：

- `inplace`：适合只读任务或低风险修改
- `worktree`：适合希望更安全执行，但仍想自动把结果带回主工作树
- `patch`：适合你想先审 patch 再决定是否应用

`worktree` 和 `patch` 要求目标目录是一个 git 仓库。

### 并行委派

使用 `--isolation patch` 模式可以同时运行多个 delegate。每个 delegate 在独立的 git worktree 中工作，patch 不会自动应用。

```bash
# 并行运行两个 delegate（从脚本或支持并发调用的 agent 中发起）
quancode delegate --agent codex --isolation patch --format json "实现 pkg/foo 中的功能 X"
quancode delegate --agent codex --isolation patch --format json "给 pkg/bar 写测试"
```

JSON 结果中包含 `patch` 字段（unified diff 格式）。使用 `apply-patch` 命令应用：

```bash
quancode apply-patch --workdir /path/to/repo --file /tmp/patch-feature.diff
```

或通过 stdin 传入：

```bash
echo "$PATCH" | quancode apply-patch --workdir /path/to/repo
```

`apply-patch` 在应用前会打印受影响文件的摘要，方便你审核。逐个应用 patch，每个应用后都确认一下。

并行任务尽量按文件边界切分，避免 patch 冲突。

### 异步委派

使用 `--async` 在后台运行委派任务：

```bash
quancode delegate --async --agent codex --isolation worktree "implement feature X"
```

立即返回 `job_id`，任务在后台独立进程中执行。

规则：
- `--async` 必须搭配 `--isolation worktree` 或 `--isolation patch`
- `--async` 不支持 `--verify` / `--verify-strict`
- `--timeout <seconds>` 设置单任务超时（不超过 agent 配置的 `timeout_secs`），同步委派同样适用

通过 `quancode job` 管理异步任务：

```bash
quancode job list [--workdir /path/to/repo]   # 列出任务（最新优先）
quancode job status <job_id>                   # 查看状态
quancode job result <job_id>                   # 获取结果（仅终态任务）
quancode job logs <job_id> [--tail 50]         # 查看输出
quancode job cancel <job_id>                   # 取消运行中的任务
quancode job clean [--ttl 168h]                # 清理过期任务文件
```

对于 `--isolation patch` 的异步任务，用以下命令应用 patch：

```bash
quancode apply-patch --id <delegation_id>
```

`delegation_id` 可从 `quancode job result --format json` 的输出中获取。

### 一个端到端示例

```bash
quancode delegate --agent codex --isolation worktree --format text "write tests for router selection"
```

典型流程：

- QuanCode 在选定的执行模式中启动子 agent
- 子 agent 完成 one-shot 任务
- QuanCode 返回结果并把记录写入本地 ledger
- `changed_files` 和耗时数据会被保留下来，后续可用于统计和配额

## 4. 路由

### `quancode route`

如果你想先看自动路由会选谁，可以运行：

```bash
quancode route "review this Go patch"
quancode route "implement a new command and update docs"
```

输出会包含：

- 原始任务文本
- 被选中的 agent
- 选择原因

当前路由是基于关键词和优先级，不是 LLM 规划器。

## 5. 可观测性

### `quancode agents`

查看当前已启用 agent 以及它们是否可用：

```bash
quancode agents
```

输出包含 agent 名称、可用状态、命令、strengths 和 description。

### `quancode stats`

查看近期 delegation 统计：

```bash
quancode stats
quancode stats --days 7
```

它会显示：

- 当前时间窗口内的总调用数
- 每个 agent 的成功率、失败数、超时数、平均耗时、总耗时、改动文件数
如果还没有 ledger 数据，`stats` 会提示你先运行 `quancode delegate`。

### Statusline

`quancode init` 会自动配置 Claude Code 的 statusline。配置完成后，statusline 会显示：

- QuanCode 会话标识和当前模型
- 上下文窗口使用百分比
- 5 小时和 7 天的 rate limit 消耗
- 当前会话的累计费用

除了运行 `quancode init` 之外不需要额外配置。

### `quancode version`

查看当前安装版本：

```bash
quancode version
```

## 6. 自动降级（Auto-Fallback）

当委派任务因为 **超时** 或 **限速** 错误而失败时，QuanCode 会按照路由优先级自动选择下一个可用 agent 重试任务。这就是自动降级。

关键行为：

- 降级 **仅在** 超时或限速错误时触发。
- 普通任务失败（例如 agent 运行了但输出不正确，或以错误码退出）**不会** 触发降级。
- 下一个 agent 按照与正常路由相同的优先级规则选出。
- QuanCode 最多尝试 **3 次**（原始调用 + 最多 2 次降级）。
- 在 `inplace` 隔离模式下，如果失败的 agent 已经修改了工作树中的文件，降级会被 **阻止**。这是为了防止第二个 agent 在不完整或有问题的修改基础上继续工作。

要在某次委派中完全禁用降级：

```bash
quancode delegate --no-fallback "migrate the database schema"
```

## 7. 配置 Recipes

完整字段说明见 [`agent-config-schema.md`](agent-config-schema.md)（英文）。

### 从示例配置开始

```bash
cp quancode.example.yaml ~/.config/quancode/quancode.yaml
```

然后按你的机器环境修改 primary agent 和启用的 agents。

### 添加一个自定义 agent

在 `agents` 下新增一个条目：

```yaml
agents:
  mycli:
    name: My CLI
    command: mycli
    enabled: true
    priority: 25
    strengths: ["review", "tests"]
    delegate_args: ["run"]
```

QuanCode 的 adapter 是配置驱动的。只要是现有 schema 能描述的 CLI 形态，你不需要改 Go 代码。

### 给单个 agent 设置环境变量

如果某个 agent 需要额外环境变量，使用 `env` 字段：

```yaml
agents:
  codex:
    command: codex
    enabled: true
    env:
      HTTPS_PROXY: http://127.0.0.1:7890
```

### 调整超时

通过 `timeout_secs` 调整单个 agent 的超时时间：

```yaml
agents:
  claude:
    command: claude
    enabled: true
    timeout_secs: 600
```

## 8. 故障排查

### `doctor` 报配置或命令缺失

运行：

```bash
quancode doctor
```

先修复第一条失败项，再看后面的错误。

### Delegation 超时

重点检查：

- 目标 CLI 是否已经安装并登录
- 任务是否太大，不适合 one-shot delegate
- 当前 agent 的 timeout 是否太低

### 基于文件的 prompt 注入没有恢复干净

这类情况应该很少见。如果发生了：

- 检查受影响的文件，比如 `AGENTS.md`
- 和你预期的内容或最近提交对比
- 在弄清差异之前，不要反复重跑 `quancode start`

### 从第三方桌面端 delegate 给 Claude 报 "Not logged in"

Claude Code 的认证信息存储在 macOS Keychain 中。第三方桌面端应用（Codex Desktop、Qoder Desktop 等）可能无法访问 Keychain，导致 `claude auth status` 返回 `loggedIn: false`，即使终端里的 Claude Code 登录正常。

这是平台限制，不是 QuanCode 的问题。变通方案：

- 从 Codex/Qoder Desktop 只 delegate 给 codex 或 qoder，不 delegate 给 claude
- 从 Claude Code 终端或 Claude Desktop 调用所有 agent 都正常
- 也可以在 claude agent 的 `env` 配置中设置 `ANTHROPIC_API_KEY` 绕过 Keychain 认证（走 API 计费，非订阅额度）

### `stats` 看起来不对或者为空

`quancode stats` 读取的是 `~/.config/quancode/logs` 下的本地 JSONL ledger。

如果你刚清空过这个目录，统计会从新的空白基线重新开始。

## 9. `/quancode` Skill

QuanCode 附带了一个 Claude Code skill，让 Claude Desktop（Code 模式）和 Dispatch 可以在对话中直接编排子 agent 委派，无需离开当前会话。

### 安装

将 skill 目录复制或软链接到 Claude Code 的 skills 文件夹：

```bash
# 软链接
ln -s /path/to/QuanCode/skills/quancode ~/.claude/skills/quancode

# 或者复制
cp -r /path/to/QuanCode/skills/quancode ~/.claude/skills/quancode
```

安装完成后，Claude Code 会识别 `/quancode` 斜杠命令，并可以通过 `quancode delegate` 将编码任务路由到任何已启用的 QuanCode agent。

### 使用方式

该 skill 适用于：

- **Claude Desktop Code 模式** — 在对话中调用 `/quancode` 来委派一个有明确边界的编码任务。
- **Dispatch** — 作为多 agent 工作流的一部分，由 Claude Code 充当编排器，QuanCode agents 负责具体实现。
