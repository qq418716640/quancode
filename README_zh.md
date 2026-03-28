# QuanCode

[English](README.md)

QuanCode 是一个面向终端编程智能体的 CLI 编排层。它负责启动一个 AI coding CLI 作为主交互入口，并让它把明确、边界清晰的任务委派给其他 coding CLI。

它是一个编排层，不是一个 agent 本身。

适合这样的场景：你希望在同一个终端工作流里，按需把任务交给最合适的 coding CLI，而不是频繁手动切换不同工具。

> **状态：early alpha**  
> 核心流程已经能在已验证配置上跑通，但 agent 兼容矩阵和 prompt 注入行为仍在变化中。请预期会有一些粗糙边角。

## 安装

前置条件：

- Go 1.22+
- 至少安装并完成登录一个受支持的 coding CLI

通过源码安装：

```bash
go install github.com/qq418716640/quancode@latest
```

查看已安装版本：

```bash
quancode version
```

其他安装方式：

- 即将提供：

  ```bash
  brew tap qq418716640/tap
  brew install quancode
  ```

  `brew tap` 只会增加 QuanCode 的 formula 源，不会替换你现有的 Homebrew 源。
- 本地构建：`git clone https://github.com/qq418716640/quancode.git && cd quancode && go build -o quancode .`

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
- 支持原地执行、git worktree 隔离和 patch-only 三种委派模式
- 以 JSONL 记录 delegation 调用，并支持可选的按 agent 配额限制

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
- Aider
- OpenCode

QuanCode 的适配方式是配置驱动的，而不是把每个命令路径硬编码在代码里。不同 CLI 可能使用不同的 prompt 注入模式，例如 CLI 参数、环境变量或 `AGENTS.md` 这样的托管文件。

不同 adapter 的覆盖度并不完全一致。当前 Claude Code 的验证最多，其他内置 adapter 的测试和 smoke coverage 更少。

QuanCode 是一个独立项目。具体兼容性会随 CLI 版本变化。

兼容性预期和非目标见 [`docs/compatibility.md`](docs/compatibility.md)（英文）。

当前 adapter 可信度的保守状态表见 [`docs/compatibility-matrix.md`](docs/compatibility-matrix.md)（英文）。

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
- `cmd/delegate.go`: sub-agent 执行
- `agent/agent.go`: 通用 agent 适配器
- `prompt/injection.go`: primary prompt 构造
- `router/router.go`: agent 选择
- `runner/`: 执行与隔离辅助
- `ledger/`: 日志与配额

## 路线图

近期重点：

- 继续补 startup、delegation、isolation 相关自动化覆盖
- 通过 Goreleaser 产出 release 二进制并补齐版本化安装路径

后续：

- 更正式地整理每个 agent 的兼容性状态；当前兼容性仍然是 best-effort

## 文档

- User guide: [`docs/user-guide.zh.md`](docs/user-guide.zh.md)
- Release notes: [`CHANGELOG.md`](CHANGELOG.md)
- Manual smoke tests: [`docs/manual-smoke-tests.md`](docs/manual-smoke-tests.md)（英文）
- Contribution guide: [`CONTRIBUTING.md`](CONTRIBUTING.md)（英文）
- Privacy notes: [`docs/privacy.md`](docs/privacy.md)（英文）
- Release process: [`docs/releasing.md`](docs/releasing.md)（英文）

## License

Apache-2.0。见 [LICENSE](LICENSE)。
