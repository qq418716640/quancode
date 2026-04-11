# QuanCode

**编排你的终端编程智能体。**

<p align="center">
  <img src="docs/dashboard-demo.png" alt="QuanCode Dashboard — 538 次委派，五个 agent，一个本地仪表盘" width="800">
</p>
<p align="center"><em>538 次委派，五个 agent，一个本地仪表盘 —— 全部在你的 shell 中运行。</em></p>

[English](README.md)

QuanCode 启动一个 AI coding CLI 作为主 agent，让它把边界清晰的任务委派给其他 agent —— Claude Code、Codex、Gemini、Copilot、Qoder。路由、git worktree 隔离、验证、降级全部在本地完成。单个 Go 二进制。无守护进程、无托管服务、无厂商锁定。

它是一个编排层，不是一个 agent 本身。

- **轻量** — 单个 Go 二进制，零运行时依赖。不需要守护进程、服务端或框架。
- **通用** — 适配任何接受 prompt 并返回文本的 coding CLI。新增 agent 只需改 YAML 配置，不用写代码。
- **自主可控** — 一切在你的本地 shell 中运行。配置、日志、提示词、进程生命周期全部由你掌控。没有托管服务，没有厂商锁定。

> **状态：beta**
> 核心委派、隔离、降级和校验流程已稳定。Agent 适配器覆盖度因 CLI 而异。

## 安装

前置条件：至少安装并完成登录一个受支持的 coding CLI。

```bash
brew tap qq418716640/tap
brew install quancode
```

Linux 用户如果没有 Homebrew，可以从 [GitHub Releases](https://github.com/qq418716640/quancode/releases) 下载二进制。

验证安装：

```bash
quancode version
```

## 工作原理

```
你（自然语言）
    |
主 Agent（AI）
    |
quancode delegate/route/pipeline/...
    |
子 Agent（其他 CLI）
```

你用自然语言描述需求，主 AI agent 自主决定何时以及如何委派任务给其他 agent——路由、隔离、校验、降级——全部透明处理。

## 快速开始——两个命令上手

### 1. 初始化（一次性）

```bash
quancode init
```

扫描你的 PATH 找出已安装的 coding CLI，让你选择默认的主 agent，然后写入 `~/.config/quancode/quancode.yaml`。

### 2. 开启会话（每天都用）

```bash
quancode start
```

启动你的主 AI agent，并注入多 agent 委派能力。从这里开始，用自然语言和 AI 交互就行。AI 知道什么时候该委派任务。

为单个会话指定不同的主 agent：

```bash
quancode start --primary codex
```

**就这样。** 这两个命令涵盖 95% 的日常工作。

## AI 自主做的事

启动会话后，你永远不需要自己调用 `quancode delegate`。AI 知道如何：

- **路由任务** — 基于关键词和优先级选择最适合的子 agent
- **注入上下文** — 自动附加项目文件如 `CLAUDE.md` 和 `AGENTS.md`
- **安全隔离** — 在 git worktree 或 patch-only 模式运行
- **自动降级** — 如果某个 agent 超时或限速，自动尝试下一个
- **校验结果** — 委派后运行测试命令来验证
- **运行流水线** — 链接多阶段任务，每阶段都支持校验和降级
- **后台异步** — 执行长期任务并完整管理生命周期

详细说明见[用户指南](docs/user-guide.zh.md)。

## 配置

配置查找顺序：

1. `--config <path>`
2. `./quancode.yaml`
3. `~/.config/quancode/quancode.yaml`
4. 内置默认值

最小示例：

```yaml
default_primary: claude

agents:
  claude:
    name: Claude Code
    command: claude
    enabled: true
    primary_args: ["--append-system-prompt"]

  codex:
    name: Codex CLI
    command: codex
    enabled: true
    prompt_mode: file
    prompt_file: AGENTS.md
    delegate_args: ["exec", "--full-auto", "--ephemeral"]
    output_flag: --output-last-message
```

完整模板参考 [`quancode.example.yaml`](quancode.example.yaml)。

字段级配置说明见 [`docs/agent-config-schema.md`](docs/agent-config-schema.md)。

## 支持的 Agent

当前内置默认适配覆盖：

- Claude Code — 架构设计、复杂推理、多文件编辑
- Codex CLI — 快速编辑、代码生成、测试编写
- GitHub Copilot CLI — 多模型支持、深度仓库上下文
- Gemini CLI — 大上下文窗口、多模态
- Qoder CLI — 代码分析、调试、MCP 集成

适配方式是配置驱动的，不同 CLI 可能使用不同的 prompt 注入模式（CLI 参数、环境变量或 `AGENTS.md` 托管文件）。添加新 CLI 只需配置，不需要写 Go 代码。

提供 `/quancode` skill，可在 Claude Desktop、Cowork 和 Dispatch 中使用多 agent 委派。

兼容性预期见 [`docs/compatibility.md`](docs/compatibility.md)。

## 高级用户工具

**健康检查：**
```bash
quancode doctor       # 验证配置、agent 和 PATH
```

**可观测性：**
```bash
quancode agents       # 列出启用的 agent
quancode stats        # 委派统计信息
quancode dashboard    # web UI 监控面板（预览）
```

**手动委派（罕见——AI 通常替你做）：**
```bash
quancode delegate "为配置加载写单元测试"
quancode delegate --agent codex --isolation worktree "重构辅助函数"
```

完整命令参考见[用户指南](docs/user-guide.zh.md)。

## 安全说明

- 如果不使用隔离模式，被委派的 agent 会直接在你的工作目录里运行
- `--isolation worktree` 和 `--isolation patch` 需要当前目录是一个 git 仓库
- 基于文件的 prompt 注入由 QuanCode 托管，primary 退出后应恢复原始文件内容
- 提交前请检查 sub-agent 产生的修改

## 开发

标准检查命令：

```bash
go test ./...
go vet ./...
```

Release 构建可以通过 Go ldflags 覆盖默认版本字符串。

项目主要入口：

- `cmd/start.go`: primary 启动
- `cmd/delegate.go`: sub-agent 执行入口
- `cmd/apply_patch.go`: patch 应用（并行委派场景）
- `agent/agent.go`: 通用 agent 适配器
- `prompt/injection.go`: primary prompt 构造
- `router/router.go`: agent 选择
- `runner/`: 执行与隔离辅助
- `ledger/`: 日志与统计
- `cmd/job*.go`: 异步任务管理命令
- `job/`: 持久化任务状态与生命周期

## 文档

- 用户指南: [`docs/user-guide.zh.md`](docs/user-guide.zh.md)
- 配置参考: [`docs/agent-config-schema.md`](docs/agent-config-schema.md)
- 兼容性: [`docs/compatibility.md`](docs/compatibility.md)
- 隐私说明: [`docs/privacy.md`](docs/privacy.md)
- 贡献指南: [`CONTRIBUTING.md`](CONTRIBUTING.md)
- 更新日志: [`CHANGELOG.md`](CHANGELOG.md)

## License

Apache-2.0。见 [LICENSE](LICENSE)。
