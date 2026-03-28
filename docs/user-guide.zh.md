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

- 扫描 `PATH` 中已安装的已知 coding CLI，例如 `claude`、`codex`、`aider`、`opencode`
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
- 每个 agent 的成功率、失败数、超时数、平均耗时、总耗时、改动文件数、审批次数
- 如果这段时间发生过 approval request，还会显示 approval summary

如果还没有 ledger 数据，`stats` 会提示你先运行 `quancode delegate`。

### `quancode quota`

查看当前 quota：

```bash
quancode quota
```

给某个 agent 设置 quota：

```bash
quancode quota --set-agent claude --unit hours --limit 5 --reset-mode rolling_hours --rolling-hours 5 --notes "Claude Max"
quancode quota --set-agent codex --unit calls --limit 200 --reset-mode weekly --reset-day 1 --notes "Codex Pro"
```

支持的 unit：

- `calls`
- `minutes`
- `hours`

支持的 reset mode：

- `monthly`
- `weekly`
- `rolling_hours`

quota 视图会展示当前周期内的使用量和剩余额度。

### `quancode version`

查看当前安装版本：

```bash
quancode version
```

## 6. 审批流程

有些 delegated task 在继续执行前会请求 approval。

这时 QuanCode 会打印 request id 和类似下面的命令：

```bash
quancode approve req_123456 --allow --approval-dir /path/to/approval-dir
```

批准：

```bash
quancode approve req_123456 --allow --approval-dir /path/to/approval-dir
```

拒绝：

```bash
quancode approve req_123456 --deny --approval-dir /path/to/approval-dir --reason "do not push from this machine"
```

如果不传 `--approval-dir`，`approve` 会回退到环境变量 `QUANCODE_APPROVAL_DIR`。

注意：

- 当前 approval request 的超时时间是 120 秒
- 如果在这个时间内没有响应，QuanCode 会自动写入一条 deny 决策
- 所以看到 approval 提示后，最好尽快在另一个终端里处理

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
- delegate 是否卡在 approval 等待中

### 基于文件的 prompt 注入没有恢复干净

这类情况应该很少见。如果发生了：

- 检查受影响的文件，比如 `AGENTS.md`
- 和你预期的内容或最近提交对比
- 在弄清差异之前，不要反复重跑 `quancode start`

### `stats` 看起来不对或者为空

`quancode stats` 读取的是 `~/.config/quancode/logs` 下的本地 JSONL ledger。

如果你刚清空过这个目录，统计会从新的空白基线重新开始。
