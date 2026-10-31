# AGENTS — 开发约束（微信公众号图片下载器 (wxmp)）

> 本文件约束**所有**参与本项目的对话与子代理。开工前必读；与任务描述冲突时以本文件为准，除非用户显式豁免。

## 1. 环境与技术栈

- 运行环境：Linux amd64（本项目开发机），Go 1.22+（当前 1.27），Node 18+（仅测试油猴脚本），JDK 17+（`/opt/jdk21`），Android SDK（`/opt/android-sdk`，build-tools 34.0.0）。
- 依赖策略：**禁止新增第三方 Go / Java / JS 运行时依赖**。Go 只用标准库；Android 只用 `android.jar`；前端手写原生 HTML/CSS/JS；油猴脚本只用 Tampermonkey API 与浏览器 API。确需新增必须在 STRATEGY.md 记录理由。
- 数据来源：`https://mp.weixin.qq.com/s/...` 公开文章；离线回归样本放 `testdata/`（大文件用 `.gitignore` 排除，仅本地验证）。
- 无 Docker 的开发机上仍必须保证 `docker compose config` 语义正确，并提供 `Dockerfile.source` 兜底。

## 2. 目录与文件约定

- Go 源码：`cmd/wxmp/`、`internal/wx/`、`internal/webui/`（含 `static/`，用 `embed` 打进二进制）。
- 测试：Go `*_test.go` 与 `scripts/` 下的验证脚本；不要放进 `internal/` 之外的临时文件。
- Android：`android/src/`（Java）、`android/res/`、`android/build-apk.sh`；构建产物在 `android/build/`，最终 APK 复制到 `dist/`。
- 油猴：`userscript/wxmp.user.js`（单文件，同时兼容 Node `require` 导出纯函数以便测试）。
- 交付物：`dist/`（exe、apk、zip、SHA256SUMS.txt）；截图 `docs/`。
- 文档：STRATEGY.md、AGENTS.md、TEST_REPORT.md、HANDOFF.md 位于项目根目录，**不要移动**；plan/tests 区块由 project-flow 维护，不要手改。

## 3. 代码质量门禁

- 改动后必须能通过：`gofmt -l`（无输出）、`go vet ./...`、`go test ./...`、`go build ./...`。
- WebUI 改动后：`node --check` 不适用于 HTML，但必须启动服务用 curl 走完 `/api/config → /api/parse → /api/download → /api/jobs/{id}`。
- Android：`bash android/build-apk.sh` 成功，`apksigner verify` 通过；Java 核心类必须能在桌面 JVM 上跑 `SelfTest`。
- 油猴：`node --check userscript/wxmp.user.js`，并用 Node 载入对 `testdata/` 样本做 URL 提取断言。
- 不允许留死代码、注释掉的旧实现、调试输出（`fmt.Println` 只允许出现在 CLI 的用户可见输出）。

## 4. 禁止事项

- 禁止修改 STRATEGY.md 中的规则来迁就实现（规则变更必须由用户确认）。
- 禁止伪造验证结果；没有跑过的测试不许写进 TEST_REPORT.md。
- 禁止删除或弱化既有测试来让流水线变绿。
- 禁止把 Cookie、密钥、账户信息写入仓库或日志（Cookie 只存内存 / localStorage）。
- 禁止实现绕过登录、付费、删文恢复等能力；只下载公开可见内容。
- 禁止把 `/download` 以外的宿主机路径硬编码进镜像；路径必须来自参数或环境变量。

## 5. 验证要求

- 每次改动至少一条验证记录，使用 `project_test` 写入 TEST_REPORT.md。
- 记录必须包含：做了什么、怎么验证（命令/方法）、结果、证据（输出摘要或数字）。
- 结论为 pass 的步骤，其**最近一条**验证记录必须是 pass，才允许 `project_steps(action=complete)`。

## 6. 文档维护义务

- STRATEGY.md：业务规则、方向、步骤计划（plan 区块由插件维护，其余人工维护）。
- AGENTS.md：本约束文件，发现约束失效或缺失时立刻更新。
- TEST_REPORT.md：每次验证追加一条，禁止只写"通过"而不写证据。
- 三份文档与代码不一致时，以文档为准并修正代码；或先改文档再改代码。

## 7. 交接协议

- 收到"交接"或插件提示 context level=high/critical 时，**立即停止开发**。
- 顺序：`project_check(fix=true)` 校验并修复文档 → `project_handoff` 生成 HANDOFF.md。
- 交接文档必须能让新对话在没有旧上下文的情况下继续工作。
