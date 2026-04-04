# 使用指南

[English](user-guide.md)

QuanCode 是一个多 agent 编排层。**你负责指挥，AI 负责执行。** 绝大部分 QuanCode 命令由 AI agent 自主调用——你只需要学会两个命令就能开始使用。

## 工作原理

```
你（自然语言）→ 主 Agent（AI）→ quancode delegate/route/job/... → 子 Agents
```

1. 你运行 `quancode start` 启动主 AI agent（比如 Claude Code）
2. 你用自然语言描述你想做什么
3. 主 agent 自主决定何时以及如何把任务委派给其他 agent，调用 `quancode delegate`、`quancode pipeline` 等
4. QuanCode 负责路由、降级、隔离、验证、结果收集——全程透明

**你永远不需要自己调用 `quancode delegate`。** AI 替你做。

## 你需要学的命令

### `quancode init` — 一次性初始化

```bash
quancode init
```

安装后运行一次即可。它会扫描 `PATH` 中的已知 coding CLI（Claude Code、Codex、Qoder、Gemini、Copilot 等），让你选默认主 agent，然后写入 `~/.config/quancode/quancode.yaml`。

### `quancode start` — 启动会话

```bash
quancode start
```

这是你每天都会用的命令。它启动主 AI agent，并注入多 agent 委派能力。启动之后，直接跟 AI 对话就行。

临时切换主 agent：

```bash
quancode start --primary codex
```

**就这些。** 这两个命令覆盖了 95% 的日常使用。下面的内容是 AI 在底层做了什么的参考文档，以及给高级用户的可选工具。

---

## AI 自主完成的事情

当你给主 agent 一个任务时，它可以自主使用以下所有 QuanCode 能力。你不需要记住这些——AI 已经知道怎么用。

### 任务委派（`quancode delegate`）

AI 会把任务路由给最合适的子 agent：

- **自动路由**：根据任务关键词和优先级选择最佳 agent
- **定向委派**：在合适的时候指定具体 agent
- **上下文注入**：自动附加 `CLAUDE.md`、`AGENTS.md` 和相关文件
- **隔离模式**：`inplace`（直接修改）、`worktree`（安全沙箱 + 自动应用）、`patch`（沙箱 + 手动应用）
- **验证**：委派成功后运行测试命令验证结果
- **并行委派**：把独立任务拆分给多个 agent 并发执行
- **异步委派**：用 `--async` 在后台运行长任务

### 降级与恢复

- **自动降级**：如果 agent 超时或触发限速，QuanCode 自动选下一个可用 agent 重试（最多 3 次）
- **投机并行**：可选配置，让两个 agent 同时竞速——先成功的胜出，另一个的结果保留供参考
- **隔离过滤**：不支持所需隔离模式的 agent 会被自动跳过

### 流水线（`quancode pipeline`）

对于多阶段任务（分析 → 实现 → 测试），AI 可以运行流水线，每个阶段的输出流向下一个阶段。各阶段可以有独立的降级策略、验证命令和失败策略。

### 任务管理（`quancode job`）

后台任务的完整生命周期管理：启动、监控、获取结果、取消、清理。

## 高级用户可选命令

这些命令用于调试、监控或手动干预。不使用它们也能正常工作。

### 健康检查

```bash
quancode doctor       # 校验配置、agent 和 PATH
```

### 可观测性

```bash
quancode agents       # 列出已启用 agent 及可用状态
quancode stats        # 委派统计（成功率、耗时等）
quancode dashboard    # Web 监控界面（预览）
quancode version      # 查看当前版本
```

### Statusline

`quancode init` 会自动配置 Claude Code 的 statusline，显示上下文窗口使用率、rate limit 和会话费用。无需额外配置。

### Dashboard（预览）

```bash
quancode dashboard                # 默认端口 8377
quancode dashboard --port 9000    # 自定义端口
quancode dashboard --open         # 自动打开浏览器
```

浏览器界面，含委派历史、异步任务状态、pipeline 可视化和 SSE 实时更新。仅监听 `127.0.0.1`，只读，无需认证。前端资源已内嵌，无需联网。

### 手动委派

少数情况下你可能想从终端直接委派：

```bash
quancode delegate "write unit tests for config loading"
quancode delegate --agent codex --isolation worktree "refactor the helper"
quancode delegate --async --isolation worktree "implement feature X"
```

完整参数见下方参考部分。

### Shell Completion

```bash
quancode completion zsh   # 或 bash、fish
# 快速启用：
echo 'source <(quancode completion zsh)' >> ~/.zshrc
```

## 参考手册

以下内容详细记录所有参数和行为。主要用于理解 AI 在做什么、编写自定义配置或排障。

### 委派参数

| 参数 | 说明 |
|---|---|
| `--agent <name>` | 指定目标 agent |
| `--isolation <mode>` | `inplace`、`worktree` 或 `patch` |
| `--format <fmt>` | `text` 或 `json` |
| `--workdir <path>` | 覆盖工作目录 |
| `--async` | 后台运行（需搭配 worktree/patch） |
| `--timeout <secs>` | 单任务超时 |
| `--no-fallback` | 禁用自动降级 |
| `--verify <cmd>` | 成功后运行验证命令 |
| `--verify-strict <cmd>` | 验证失败则委派失败 |
| `--verify-timeout <secs>` | 验证超时（默认 120s） |
| `--context-files <path>` | 附加上下文文件（可重复） |
| `--context-diff <type>` | 附加 `staged` 或 `working` diff |
| `--context-max-size <bytes>` | 覆盖上下文预算（默认 32KB） |
| `--no-context` | 禁用自动上下文注入 |
| `--dry-run` | 预览完整 prompt，不执行 |

### 任务管理

```bash
quancode job list [--workdir .]        # 列出任务（最新优先）
quancode job status <job_id>           # 查看状态
quancode job result <job_id>           # 获取结果
quancode job logs <job_id> [--tail N]  # 查看输出
quancode job cancel <job_id>           # 取消运行中的任务
quancode job clean [--ttl 168h]        # 清理过期任务
```

对于 patch 模式异步任务：`quancode apply-patch --id <delegation_id>`

### 路由预览

```bash
quancode route "review this Go patch"
```

显示会选择哪个 agent 以及原因。用于理解路由决策。

### 上下文注入规则

- 自动注入文件：`CLAUDE.md` 和 `AGENTS.md`（如存在）
- 默认总预算 32 KB，单文件上限 16 KB
- `--context-diff` 接受 `staged` 或 `working`
- `--no-context` 禁用所有自动注入

### 隔离模式详解

| 模式 | 行为 | 需要 Git |
|---|---|---|
| `inplace` | 直接在工作树中运行 | 否 |
| `worktree` | 临时 git worktree，自动应用 patch | 是 |
| `patch` | 临时 git worktree，只返回 patch | 是 |

经验法则：`inplace` 适合只读/低风险，`worktree` 适合安全执行 + 自动应用，`patch` 适合人工审查。

### 验证规则

- `--verify` 记录结果但不阻塞；`--verify-strict` 验证失败则委派失败
- 两者互斥；仅在 agent 成功后运行
- `worktree` 模式下，在 patch 应用前运行
- 验证失败不触发降级

### 自动降级详解

- 仅在超时或限速时触发（普通失败和验证失败不触发）
- 最多 3 次尝试（原始 + 2 次重试）
- `inplace` 模式下，如果失败 agent 已修改文件则阻止降级
- 单次禁用：`--no-fallback`

## 配置

完整字段说明见 [`agent-config-schema.md`](agent-config-schema.md)（英文）。

### 从示例配置开始

```bash
cp quancode.example.yaml ~/.config/quancode/quancode.yaml
```

### 添加自定义 agent

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

QuanCode 的 adapter 是配置驱动的——不需要改 Go 代码。

### 给单个 agent 设置环境变量

```yaml
agents:
  codex:
    env:
      HTTPS_PROXY: http://127.0.0.1:7890
```

### 调整超时

```yaml
agents:
  claude:
    timeout_secs: 600
```

## 故障排查

### `doctor` 报失败

运行 `quancode doctor`，先修复第一条失败项。

### Delegation 超时

检查：目标 CLI 是否已安装并登录？任务是否太大？超时是否太低？

### 文件 prompt 注入未恢复

少见。检查受影响文件（如 `AGENTS.md`），与上次提交对比，弄清差异后再重跑 `quancode start`。

### 第三方桌面端报 "Not logged in"

Claude Code 认证用 macOS Keychain，第三方桌面端可能无法访问。变通方案：
- 从 Codex/Qoder Desktop 只 delegate 给 codex 或 qoder
- 从 Claude Code 终端或 Claude Desktop 则一切正常
- 或在 claude agent 的 `env` 配置中设置 `ANTHROPIC_API_KEY` 绕过 Keychain

### `stats` 为空

`quancode stats` 读取 `~/.config/quancode/logs/`。清空过该目录则统计从零开始。

## `/quancode` Skill

QuanCode 附带 Claude Code skill，让 Claude Desktop（Code 模式）和 Dispatch 在对话中直接编排子 agent 委派。

### 安装

```bash
ln -s /path/to/QuanCode/skills/quancode ~/.claude/skills/quancode
```

### 使用方式

- **Claude Desktop Code 模式** — 在对话中调用 `/quancode` 委派编码任务。
- **Dispatch** — 作为多 agent 工作流的一部分，Claude Code 编排，QuanCode agents 执行。
