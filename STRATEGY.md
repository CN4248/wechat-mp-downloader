# STRATEGY — 微信公众号图片下载器 (wxmp)

> 本文件由 project-flow 维护，是**业务规则**与**方向取舍**的唯一事实来源。
> 规则先改这里，再改实现；任何实现都必须能从本文件推导出来。

## 1. 项目目标

- 目标：输入一个或多个微信公众号文章链接，解析文章内**所有图片与动图（GIF）**，并下载**原图**（微信 CDN 的 `/0` 版本，而非 `/640` 压缩图）。
- 交付形态：
  1. WebUI（Go 单二进制内嵌，默认端口 **2178**）；
  2. `docker-compose` 容器（`network_mode: bridge`，端口 2178，宿主机下载目录 `/mnt/media/wechat` 挂载到容器 `/download`）；
  3. Windows `.exe`（amd64，免安装）；
  4. Android `.apk`（minSdk 26，免 Gradle 构建）；
  5. Tampermonkey 用户脚本（文章页内一键下载）。
- 明确不做（范围外）：绕过登录/付费/删文恢复；视频号与腾讯视频外链下载；正文文字转 PDF/Word；批量爬取公众号历史列表。

## 2. 可行方向

<!-- project-flow:begin plan -->
_由 project-flow 维护 · 更新于 2026-10-22 10:46 · 状态：开发中_

### 2.1 候选方向

| # | 方向 | 概要 | 优点 | 缺点 |
| --- | --- | --- | --- | --- |
| D1 | A. Go 单二进制 + 内嵌 WebUI + 预编译多平台 | 核心解析/下载用 Go stdlib 实现，WebUI 用 embed；同一份 Go 代码交叉编译 Linux/Windows，Docker 直接拷贝预编译静态二进制实现秒级构建；Android 用 Java 复刻核心并免 Gradle 打包 APK。 | 构建快、无外部依赖、与参考项目 qqchannel-downloader 架构一致、便于 cron/CLI | 解析器与 Android 端需要两套实现，需保持行为一致 |
| D2 | B. 纯 Python/Node 后端 + React 前端 | 用 FastAPI/Express 做后端，前端单独打包，Docker 内 pip/npm install。 | 开发快、库多 | 镜像构建慢、依赖体积大、Windows exe 需 PyInstaller、APK 需 WebView 壳，与要求“构建镜像要快”冲突 |

### 2.2 选定方向与理由

**A. Go 单二进制 + 内嵌 WebUI + 预编译多平台**

用户明确要求“构建镜像要快”且要同时交付 .exe/.apk/docker；Go 静态二进制可直接 COPY 进 alpine，镜像构建 5 秒级；与参考项目 qqchannel-downloader 同语言同架构，便于“该有的都要有”；Android 端按参考项目用 aapt2+javac+d8 免 Gradle 打包。

### 2.3 步骤计划

| 步骤 | 内容 | 任务 | 验收标准 | 负责人 | 状态 |
| --- | --- | --- | --- | --- | --- |
| S9 | 合集批量下载 + 合集命名规则 + 双版本油猴多选 | 1) Go/CLI/WebUI 合集解析与批量下载; 2) Android APK 合集解析与批量下载; 3) 独立版油猴合集多选模式; 4) 推送版油猴文章/合集多选推送; 5) 版本 1.2.0 重建与文档 | 合集 106 篇可解析；CLI/WebUI/APK 至少 2 篇 10 张下载成功；两个油猴脚本多选批量可用且独立/推送互不依赖 | 主对话 | 已完成 |
<!-- project-flow:end plan -->
## 3. 业务规则

### 3.1 输入与链接

- 单篇：`https://mp.weixin.qq.com/s/<slug>`、`/s?__biz=...`，自动忽略 `#rd`、`chksm` 等跟踪参数。
- 合集：`https://mp.weixin.qq.com/mp/appmsgalbum?__biz=...&action=getalbum&album_id=...`，用官方 JSON 分页接口翻页。

- 支持形式：
  - `https://mp.weixin.qq.com/s/<短id>`
  - `https://mp.weixin.qq.com/s?__biz=...&mid=...&idx=...&sn=...`
  - 以上链接带 `#rd`、`&chksm=...`、`&scene=...` 等参数。
  - 粘贴整段分享文案时，自动提取其中第一条 `mp.weixin.qq.com` 链接。
- 单次最多 **30** 条链接；WebUI 每行一条；命令行可传多个位置参数。
- 仅接受 `mp.weixin.qq.com` / `weixin.qq.com` 域名，其他域名直接报错。

### 3.2 解析规则

- `title`：优先 `var msg_title = '...'`，回退 `og:title`，再回退 `html > title` 去掉“微信公众平台”后缀。
- `post_id`：优先文章 URL 的短链 id（`/s/<slug>`，如 `tC1hauHuVvqRusojhhXe5g`），回退 `appmsgid`，再回退 `mid`。`<mid>` 仍保留数字 id。
- `account`：`var nickname`；`author`：`var author`。
- `publish_time`：`var ct = "<unix>"`，按 `Asia/Shanghai` 格式化。
- `biz` / `sn` / `mid` / `idx`：页面 JS 变量，缺失留空。
- 图片清单来源与顺序：
  1. `#js_content` 内 `` 的文档顺序（`data-src` 优先，回退 `src`）；
  2. 若文章有 `picture_page_info_list`（图片消息/图集），把其中**未被前面命中**的条目按数组顺序追加；
  3. 封面 `msg_cdn_url` 仅当用户勾选“包含封面”且未重复时追加到末尾。
- 去重键：CDN path 的**完整文件名**（去掉末尾尺寸段，忽略 `mmbiz_jpg`/`sz_mmbiz_jpg` 等目录前缀与 query），大小写不敏感；这样正文图与 `picture_page_info_list` 的同一张图不会重复计数。严禁只取前缀：不同图片可能共享前 33 个字符。
- 过滤：`data:`、`svg`、`mmbiz.qlogo.cn` 头像、宽高均 < 50px 的占位图/分割线不下载。
- 动图判定：`data-type="gif"` 或 URL 含 `wx_fmt=gif` / `mmbiz_gif`。动图必须保留动画，不得转成静态图或 webp。
- 视频（`mp-common-videosnap` / `mpvideo`）：列为“实验性视频”，尽力解析播放地址；失败只记 warning，不影响图片任务。

### 3.3 原图规则

- 微信 CDN 路径最后一段是尺寸（`/0`、`/300`、`/640`、`/750`、`/1080` 等）：把**最后一段纯数字**替换为 `0` 即原图。
- query 中 `wx_fmt` 改为图片真实格式（`png/jpeg/gif/webp`），删除会强制转码的 `tp` 参数，其余参数保留。
- 预览图使用 `/640` 版本，经 `/api/thumb` 代理（带 `Referer: https://mp.weixin.qq.com/`），避免防盗链与流量浪费。
- 若原图请求失败，回退到文章里给出的压缩图并在结果中标记 `warning`，不得静默失败。

### 3.4 命名规则（核心需求）

- 默认模板：`<title>-<post_id>-<order>.<ext>`
- 变量（左为中文别名，右为英文 token，二者等价）：

| 中文 | token | 含义 |
| --- | --- | --- |
| `<文章标题>` | `<title>` | 文章标题（已去除非法字符） |
| `<文章id>` | `<post_id>` | 文章短链 id（`/s/<slug>`；缺失时回退 appmsgid/mid） |
| `<图片序号>` | `<order>` | 图片序号，从 1 开始，不补零 |
| `<扩展名>` | `<ext>` | 扩展名，不含点：png/jpg/gif/webp |
| `<公众号>` | `<account>` | 公众号名称 |
| `<作者>` | `<author>` | 作者 |
| `<日期>` | `<date>` | 发布时间 `YYYY-MM-DD` |
| `<时间>` | `<time>` | 发布时间 `HHMMSS` |
| `<年>` `<月>` `<日>` | `<year>` `<month>` `<day>` | 发布时间的年/月/日 |
| `<类型>` | `<kind>` | `image` / `gif` / `video` |
| `<宽>` `<高>` | `<width>` `<height>` | 原图宽高（未知为 0） |
| `<序号2>` | `<order2>` | 两位补零序号 `01` |
| `<biz>` `<sn>` `<mid>` `<idx>` | 同名 | 页面元数据 |

- **斜杠建子目录**：模板中的 `/`（兼容 `\`）是目录分隔符，不是文件名字符。
  - 例：`<文章标题>/<文章id>-<图片序号>.<扩展名>` → `/download/<文章标题>/<文章id>-<图片序号>.<扩展名>`。
  - 每一段单独做非法字符清洗（`<>:"|?*`、控制字符、段内 `/`）。
  - 空段、`.`、`..` 一律替换为 `_`，禁止路径逃逸出下载根目录。
- 未包含 `<ext>`/`<扩展名>` 时，自动在末尾补 `.<ext>`；包含则不补。
- 文件名冲突（同目录同名）：追加 `-2`、`-3`…；已存在且未勾选“强制覆盖”时跳过并计入“跳过”。
- 单段最长 120 字符，总相对路径最长 240 字符；Windows 保留名（CON/PRN/AUX/NUL/COM1-9/LPT1-9）前加 `_`。

### 3.4.1 合集命名规则（单独设置）

- 默认模板：`<collection_title>/<collection_order>-<title>/<post_id>-<order>.<ext>`
- 单篇变量全部可用；新增：
  - `<collection_title>` / `<合集标题>`：合集标题
  - `<collection_id>` / `<合集ID>`：链接里的 `album_id`
  - `<collection_order>` / `<合集内文章序号>`：合集内序号，1 = 最早，越大越新
- `/` 同样建子目录；模板在 CLI（`-collection-name`）、WebUI、APK 中各自独立设置。
- 油猴脚本合集多选**沿用单篇规则**，不套用合集模板。

### 3.5 下载规则

- 默认并发 **4**（范围 1–16），失败重试 **3** 次，指数退避 0.7s/1.4s。
- WebUI 把当前任务 ID 写入 `localStorage`（`wxmp.job.v1`）；刷新后重新拉取 `/api/jobs/{id}` 恢复进度、日志、文件列表并继续轮询，任务不存在时自动清除。
- 先写 `<目标>.part`，成功后原子改名；失败删除 `.part`，不留半截文件。
- 请求头：桌面 Chrome UA + `Referer: https://mp.weixin.qq.com/`；支持用户自定义 Cookie。
- 文件校验：按 magic bytes 判断真实格式；GIF 必须以 `GIF8` 开头（允许 `GIF87a/GIF89a`）；若实际格式与扩展名不符，记录 warning 并以真实格式纠正扩展名。
- 跳过策略：目标文件存在且大小 > 0 时跳过；`force=true` 时覆盖。
- 输出目录：CLI/WebUI/Android 均可自定义；Docker 内默认 `/download`；WebUI 默认读环境变量 `WXMP_OUT`，否则 `/download`（本地运行回退 `./downloads`）。

### 3.6 WebUI 规则

- 默认监听 `127.0.0.1:2178`；容器内 `0.0.0.0:2178`。
- 流程：粘贴链接 → 解析预览（标题/公众号/时间/图片数/缩略图/原图链接）→ 设置目录与模板 → 下载 → 进度/日志/结果表。
- 合集链接（`mp/appmsgalbum`）解析为文章列表（最多 300 篇），下载时逐篇解析再套合集模板；`max_articles` 可限制本次篇数。
- 命名模板实时预览（服务端渲染，保证与下载一致），非法 token 立即标红。
- 同一时间只允许一个下载任务；支持取消（已下载文件保留）。
- `/api/open-dir` 在容器内无法打开宿主机目录，返回明确提示而不是报错。
- 安全：默认仅本机可访问；CSP `default-src 'self'`；Cookie 不写日志、不落盘。

## 4. 风险与约束

- 微信风控：高频请求可能触发“环境异常”验证页；需支持 Cookie，并提示用户降低并发。
- 防盗链：图片 CDN 必须带 Referer；部分账号开启“仅粉丝可看”时需登录 Cookie。
- 版权：仅用于个人备份公开内容，不得再分发；README 需写明免责声明。
- 兼容性：Windows 路径非法字符与 260 字符限制；Android SAF 目录写入；油猴受站点 CSP 与 File System Access 支持范围限制。

## 5. 未决问题

- [ ] 找一篇真实含 GIF 的公众号文章，验证 `/0?wx_fmt=gif` 返回动画 GIF（S1/S7 中补测）。
- [ ] 视频嗅探接口在无登录态下的可用性（实验性，失败不影响图片）。

## 交易规则

_（待补充：由维护者补齐）_
