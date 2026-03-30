# QuanCode

[English](README.md)

QuanCode 是一个轻量级的终端编程智能体编排层。它负责启动一个 AI coding CLI 作为主交互入口，并让它把明确、边界清晰的任务委派给其他 coding CLI。

它是一个编排层，不是一个 agent 本身。

- **轻量** — 单个 Go 二进制，约 4k 行生产代码，零运行时依赖。不需要守护进程、服务端或框架。
- **通用** — 适配任何接受 prompt 并返回文本的 coding CLI。新增 agent 只需改 YAML 配置，不用写代码。
- **自主可控** — 一切在你的本地 shell 中运行。配置、日志、提示词、进程生命周期全部由你掌控。没有托管服务，没有厂商锁定。

## 安装

前置条件：至少安装并完成登录一个受支持的 coding CLI。

```bash
brew tap qq418716640/tap
brew install quancode
```

Linux 用户如果没有 Homebrew，可以从 [GitHub Releases](https://github.com/qq418716640/quancode/releases) 下载二进制。

## 快速开始

1. 检测已安装的 CLI 并生成配置：

```bash
quancode init
```

2. 检查当前环境：

```bash
quancode doctor
```

3. 启动一个主 agent：

```bash
quancode start
quancode start --primary codex
```

## 它能做什么

- 启动一个 primary coding CLI，并通过 CLI 参数、环境变量或托管文件注入 delegation 指令
- 把 one-shot 任务委派给其他 coding CLI，并返回 text 或 JSON 输出
- 按关键词匹配和静态优先级做路由，不做基于 LLM 的自动路由
- 超时或限速时自动降级到下一个可用 agent（`--no-fallback` 可禁用）
- 支持原地执行、git worktree 隔离和 patch-only 三种委派模式
- 以 JSONL 记录每次 delegation，用于统计和审计
- 自动配置 Claude Code statusline，显示会话标识、配额和费用
- 提供 `/quancode` skill，支持从 Claude Desktop 和手机 Dispatch 编排任务

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

如果你想从一个不带本机代理或机器路径假设的模板开始，可以直接参考 [`quancode.example.yaml`](quancode.example.yaml)。

字段级配置说明见 [`docs/agent-config-schema.md`](docs/agent-config-schema.md)（英文）。

## 使用说明

更完整的命令教程、隔离模式说明和排障入口见 [`docs/user-guide.zh.md`](docs/user-guide.zh.md)。

## 支持的 Agent

当前内置默认适配覆盖：

- Claude Code
- Codex CLI
- Qoder CLI

适配方式是配置驱动的，不同 CLI 可能使用不同的 prompt 注入模式（CLI 参数、环境变量或 `AGENTS.md` 托管文件）。添加新 CLI 只需配置，不需要写 Go 代码。Tab 补全支持 `--primary` 和 `--agent` 等 flag 的值自动补全。

兼容性预期和非目标见 [`docs/compatibility.md`](docs/compatibility.md)（英文）。

当前 adapter 可信度的保守状态表见 [`docs/compatibility.md`](docs/compatibility.md)（英文）。

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

Release 构建可以通过 Go ldflags 覆盖默认版本字符串。最终以 release tag 作为版本真值来源。

项目主要入口：

- `cmd/start.go`: primary 启动
- `cmd/delegate.go`: sub-agent 执行入口
- `cmd/apply_patch.go`: patch 应用（并行委派场景）
- `cmd/delegate_attempt.go`: 单次委派执行与审批轮询
- `cmd/fallback.go`: 自动降级判定
- `agent/agent.go`: 通用 agent 适配器
- `prompt/injection.go`: primary prompt 构造
- `router/router.go`: agent 选择
- `runner/`: 执行与隔离辅助
- `ledger/`: 日志与配额

## 路线图

- 继续扩展 agent 兼容性验证
- 探索更多桌面端 skill 集成（Cowork 等）

## 文档

- 用户指南: [`docs/user-guide.zh.md`](docs/user-guide.zh.md)
- 配置参考: [`docs/agent-config-schema.md`](docs/agent-config-schema.md)（英文）
- 兼容性: [`docs/compatibility.md`](docs/compatibility.md)（英文）
- 隐私说明: [`docs/privacy.md`](docs/privacy.md)（英文）
- 贡献指南: [`CONTRIBUTING.md`](CONTRIBUTING.md)（英文）
- 更新日志: [`CHANGELOG.md`](CHANGELOG.md)

## License

Apache-2.0。见 [LICENSE](LICENSE)。
