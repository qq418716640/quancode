# QuanCode 开源准备路线图

> 目标：在持续功能开发的同时，逐步完成开源发布准备。预计投入 20% 编码时间，P0+P1 约 6-8 周。
>
> 当前状态更新：2026-03-28。P0/P1/P2 全部完成，v0.1.0 已发布，Homebrew tap 已接线。剩余 P3 锦上添花。

## P0：公开前必须完成（安全/法律）

- [x] 审计 git 历史，清除个人信息（代理地址、.claude/ session 数据）
  - 已使用 orphan branch 创建 fresh repo，历史中无真名/公司邮箱/敏感数据
- [x] 加强 .gitignore（已覆盖 `.DS_Store`、二进制、`.gocache/`、`.claude/`、`*.jsonl`）
- [x] 添加 LICENSE
  - 当前仓库已切为 Apache-2.0，与原路线图对齐
- [x] 提供 quancode.example.yaml，不含个人环境假设（代理、路径）
- [x] 第三方 CLI 兼容性声明（非隶属关系）

## P1：宣传前必须完成（可信度）

- [x] README.md
  - 已覆盖一句话价值主张、安装方式（`go install`）、快速开始、支持的 CLI、实验性声明
  - 仍可补强：delegate 端到端截图、兼容性矩阵、故障排查
- [x] 核心路径测试
  - [x] config 加载 + 回填
  - [x] prompt 注入 + 文件恢复
  - [x] router 选择逻辑
  - [x] delegate JSON/text 输出
  - [x] changed_files 检测
  - [x] MergeEnv 替换语义
- [x] CI（GitHub Actions）
  - 当前已覆盖 `go test` + `go vet` + `go build`
  - [x] 补 `go build`
  - [x] 扩为 Linux + macOS 双平台
- [x] 示例配置 + AgentConfig schema 文档
- [x] 版本号（semver）+ CHANGELOG.md
  - [x] 对外可见的版本来源（如 tag、`--version` 或发布版本约定）
  - [x] CHANGELOG.md
- [x] 手动冒烟测试清单（Claude Code + Codex 的 start/delegate 路径）

## P2：发布首月内（社区）

- [x] CONTRIBUTING.md（如何添加新 CLI 配置、跑测试、验证委托行为）
- [x] Issue 模板（环境信息、CLI 版本、配置、复现步骤）
- [x] Homebrew tap 或打包分发
  - [x] 基础 Goreleaser 发布配置
  - [x] 发布工作流（tag 触发 Goreleaser）
  - [x] Homebrew formula 发布脚手架
  - [x] 外部 tap 仓库接线（qq418716640/homebrew-tap）+ v0.1.0 首轮验证通过
- [x] 兼容性矩阵（Go、Claude Code、Codex、OS 版本）
  - [x] 初版 CLI 兼容性矩阵
  - [x] Go / OS 维度补全
- [x] 隐私声明（不收集遥测数据）

## P3：锦上添花

- [ ] Demo GIF / 短视频演示
- [ ] 文档站
- [ ] 插件/agent 模板脚手架

## 时间线

| 周 | 目标 |
|---|------|
| 1-2 | P0：历史清理、LICENSE、.gitignore、示例配置 |
| 3 | README + 安装文档 |
| 4-6 | 核心测试（config、prompt、router、delegate、runner） |
| 7-8 | CI + 版本管理 + 文档打磨 |

## 当前缺口汇总

- ~~安全与发布清理：fresh repo、历史脱敏~~ ✅ 已完成
- ~~发布管理：tap 仓库接线与 Homebrew 首轮验证~~ ✅ 已完成
- 测试：更多 start/delegate 集成路径

## 定位建议

- 核心叙事：`quancode` 是编程 CLI 的委托安全编排层，不是另一个编程助手
- 目标用户：已经在用多个 terminal coding agent、感受到切换摩擦的开发者
- 差异化：config 驱动的适配器模型、工作区安全（文件恢复、worktree 隔离）、跨 agent 使用追踪
- 纪律：不要过早扩展为"通用 AI agent 平台"，保持窄而可信

## 发布策略

发布时使用干净的 fresh repo（不推送当前包含个人数据的 git 历史），这比逐条审计历史更安全。
