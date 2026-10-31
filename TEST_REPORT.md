# TEST_REPORT — 验证记录（微信公众号图片下载器 (wxmp)）

> 每次验证都必须追加一条记录，使用 `project_test`。记录 = 时间 + 步骤 + 方法/命令 + 结果 + 证据。
> 没有证据的"通过"视为无效记录。

## 验证规范

- 单元测试、集成测试、回测、数据校验分别记录，不要合并成一条含糊的结论。
- 证据写：命令、输出摘要、指标数字（收益、夏普、最大回撤、命中率等）。
- 失败也要记录，并写清失败现象与下一步；失败记录不得删除。
- 一个步骤完成前，其最近一条记录必须是"通过"。

<!-- project-flow:begin tests -->
_由 project-flow 维护 · 更新于 2026-10-22 15:26_

| 编号 | 时间 | 步骤 | 验证内容 | 结果 |
| --- | --- | --- | --- | --- |
| T1 | 2026-10-21 12:18 | S1 | Go 核心 + CLI 对测试文章解析并下载 8 张原图 | 通过 |
| T2 | 2026-10-21 12:19 | S2 | WebUI parse/preview/thumb/download 全链路 | 通过 |
| T3 | 2026-10-21 12:29 | S3 | Docker/Compose 配置校验与 Alpine 运行时验证 | 部分通过 |
| T4 | 2026-10-21 12:29 | S4 | Windows exe 交叉编译 + Wine 实跑 | 通过 |
| T5 | 2026-10-21 12:29 | S5 | Android APK 构建、安装与真机式端到端下载 | 通过 |
| T6 | 2026-10-21 12:30 | S6 | Tampermonkey 脚本语法检查与 Node 提取断言 | 通过 |
| T7 | 2026-10-21 12:32 | S7 | 指定测试文章的全形态端到端回归 | 通过 |
| T8 | 2026-10-21 12:32 | S8 | 文档、截图与 dist 打包校验 | 通过 |
| T9 | 2026-10-22 04:13 | S7 | 修复图集重复计数并把 post_id 改为短链 id | 通过 |
| T10 | 2026-10-22 04:13 | S1 | Go 单测覆盖短链 post_id 与 CDN 变体去重 | 通过 |
| T11 | 2026-10-22 04:13 | S6 | 油猴脚本独立运行、短链 id 与去重 | 通过 |
| T12 | 2026-10-22 04:13 | S5 | APK 重建并用桌面 JVM 验证新文章 | 通过 |
| T13 | 2026-10-22 04:14 | S3 | compose 静态校验（容器构建受沙箱限制） | 部分通过 |
| T14 | 2026-10-22 04:37 | S7 | 针对“3 张变 6 张”的变体去重加固 | 通过 |
| T15 | 2026-10-22 04:42 | S7 | 成对镜像图集去重（3 张变 6 张补强） | 通过 |
| T16 | 2026-10-22 10:29 | S9 | CLI 合集批量下载（前 2 篇 10 张原图） | 通过 |
| T17 | 2026-10-22 10:29 | S9 | WebUI 合集解析与批量下载（1.2.0） | 通过 |
| T18 | 2026-10-22 10:29 | S9 | Android 合集解析与下载（同源代码 JVM） | 通过 |
| T19 | 2026-10-22 10:29 | S9 | 独立版油猴合集多选批量下载 | 通过 |
| T20 | 2026-10-22 10:29 | S9 | 推送版油猴文章/合集多选推送（mock 服务） | 通过 |
| T21 | 2026-10-22 10:46 | S9 | Android APK 合集解析与批量下载（模拟器实测） | 通过 |
| T22 | 2026-10-22 12:14 | S9 | 油猴弹窗内多选模式与推送版按钮错位修复 | 通过 |
| T23 | 2026-10-22 12:31 | S9 | 合集页保存位置/命名规则与推送 out_dir 生效 | 通过 |
| T24 | 2026-10-22 12:33 | S9 | 推送路径留空回退与自定义 out_dir 落盘 | 通过 |
| T25 | 2026-10-22 15:26 | S9 | WebUI 刷新后恢复当前任务进度 | 通过 |
| T26 | 2026-10-22 15:26 | S9 | 修复共享前缀图片被误合并（4 图缺漏） | 通过 |
<!-- project-flow:end tests -->

## 明细

### T1 · Go 核心 + CLI 对测试文章解析并下载 8 张原图 — 通过（2026-10-21 12:18）

- 步骤：S1
- 方法/命令：—
- 证据：命令 ./build/wxmp-linux-amd64 -o /tmp/finaltest -j 6 <测试文章>；输出 8 行 ✓，标题「哪里搞错了」、公众号「星空3532」、id 2247523869；第 1 张 1254x1254 1.5MB、第 4 张 2508x3541 1.3MB；find 计数 8 个文件，du 3.2M
- 备注：—

### T2 · WebUI parse/preview/thumb/download 全链路 — 通过（2026-10-21 12:19）

- 步骤：S2
- 方法/命令：—
- 证据：POST /api/parse 返回 8 项、rel_path 哪里搞错了/2247523869-1.png；POST /api/download 创建 job 20261021-121929-8f536d2b1f；轮询 /api/jobs 得到 status=done done=8 success=8 skipped=0 failed=0 total=8；/tmp/webfinal3 落盘 8 个文件；/api/thumb 返回 200 image/png 37103 字节
- 备注：—

### T3 · Docker/Compose 配置校验与 Alpine 运行时验证 — 部分通过（2026-10-21 12:29）

- 步骤：S3
- 方法/命令：`docker-compose config；chroot /tmp/alpine /docker-entrypoint.sh -o /download ...；chroot 内 --web -listen 0.0.0.0:2182 + curl；docker build`
- 证据：docker-compose config 解析成功：network_mode: bridge、ports 2178:2178、bind /mnt/media/wechat → /download；alpine-minirootfs 3.20 chroot 内 entrypoint+wxmp 解析 8 张并下载 8/8（3.1MB，中文子目录正确），--web 模式 /api/health ok、/api/parse 返回 8 项；docker build 失败：Error response from daemon: unshare: operation not permitted（沙箱无 CAP_SYS_ADMIN，无法建命名空间）
- 备注：容器运行时行为已在 Alpine chroot 验证；Docker 镜像构建/运行受本沙箱 unshare 限制，需在有特权的宿主机执行 docker compose up -d --build。

### T4 · Windows exe 交叉编译 + Wine 实跑 — 通过（2026-10-21 12:29）

- 步骤：S4
- 方法/命令：`WINEDEBUG=-all wine dist/wxmp-windows-amd64.exe -o 'Z:\tmp\winfinal' -j 6 <测试文章>`
- 证据：wine 运行 exe：解析 8 张、标题/公众号/id 正确，8 行 ✓，第 1 张 1254x1254 1.5MB、第 4 张 2508x3541 1.3MB，完成成功 8 跳过 0 失败 0 共 3.1MB；/tmp/winfinal 8 个文件
- 备注：—

### T5 · Android APK 构建、安装与真机式端到端下载 — 通过（2026-10-21 12:29）

- 步骤：S5
- 方法/命令：`bash android/build-apk.sh；apksigner verify --print-certs；emulator install/launch + adb input；java -cp android/out/selftest SelfTest`
- 证据：APK 36K 构建成功，apksigner verify 通过（证书 SHA-256 607fdd18...）；模拟器安装并启动，输入测试文章后解析出 8 项（标题 哪里搞错了、id 2247523869、示例名 哪里搞错了-2247523869-1.png），点开始下载后日志显示「完成：8 成功·0 跳过·0 失败」，逐张 ✓ 及大小；桌面 JVM SelfTest 同源代码 ok=8 skip=0 fail=0 共 3,268,131 字节
- 备注：—

### T6 · Tampermonkey 脚本语法检查与 Node 提取断言 — 通过（2026-10-21 12:30）

- 步骤：S6
- 方法/命令：`node --check userscript/wxmp.user.js；node userscript/test.js（testdata/article-fixture.html）`
- 证据：node --check 通过；Node 断言输出 USERSCRIPT TESTS PASS png,jpg,gif：标题 测试文章标题、公众号 测试号、id 1234567890、去重后 3 项（含 GIF）、全部转 /0 原图、排除 watermark；中文模板 <文章标题>/<文章id>-<图片序号>.<扩展名> → 测试文章标题/1234567890-1.png；默认模板 → 测试文章标题-1234567890-1.png；<order2>_<kind>.<ext> → 01_image.png
- 备注：期间修复 userscript 只识别字面 <ext>、中文 <扩展名> 会重复追加扩展名的问题（新增 hasExtToken 按 canonical 判断）；浏览器内实际加载 Tampermonkey 未在本环境验证。

### T7 · 指定测试文章的全形态端到端回归 — 通过（2026-10-21 12:32）

- 步骤：S7
- 方法/命令：`gofmt -l .；go vet ./...；go test ./...；go build ./...；CLI/Wine/Alpine chroot/Android 模拟器/WebUI 全链路`
- 证据：gofmt 无输出、go vet 无输出、go test ./internal/wx ok、go build 通过；CLI 8/8（3.1MB，原图 1254×1254/2508×3541）；Wine exe 8/8；Alpine chroot 8/8 + WebUI /api/health ok、/api/parse 8 项；Android 模拟器解析 8 项并下载 8 成功 0 失败；WebUI job done=8 success=8；userscript Node 断言 PASS
- 备注：—

### T8 · 文档、截图与 dist 打包校验 — 通过（2026-10-21 12:32）

- 步骤：S8
- 方法/命令：`ls -lh dist；sha256sum -c SHA256SUMS.txt；ls docs`
- 证据：dist 含 wxmp-windows-amd64.exe 8.0M、wxmp-windows-amd64.zip 3.4M、wxmp-linux-amd64 7.8M、wxmp-android.apk 33K、wxmp.user.js 33K、SHA256SUMS.txt、快速开始.txt；sha256sum -c 五个文件全部「成功」；docs 含 webui-home/webui-preview/android-home/android-parse/android-download 五张截图；README 已补 Android 截图、油猴自测命令与各形态验证摘要
- 备注：—

### T9 · 修复图集重复计数并把 post_id 改为短链 id — 通过（2026-10-22 04:13）

- 步骤：S7
- 方法/命令：—
- 证据：./build/wxmp-linux-amd64 -dry-run ... tC1hauHuVvqRusojhhXe5g -> "文章 id：tC1hauHuVvqRusojhhXe5g","图片：3 张"；WebUI /api/parse 返回 post_id=tC1hauHuVvqRusojhhXe5g、medias=3；/api/download 任务 status=done success=3 skipped=0 failed=0；旧文章 y9op... 仍解析 8 张、名称含 y9op-1LgaEJ5hEYcFJyFJw。
- 备注：—

### T10 · Go 单测覆盖短链 post_id 与 CDN 变体去重 — 通过（2026-10-22 04:13）

- 步骤：S1
- 方法/命令：—
- 证据：go test ./internal/wx/ 通过（ok wxmp/internal/wx）；新增 TestParseDedupesCDNVariants：正文 mmbiz_jpg/<hash>/640 与图集 sz_mmbiz_jpg/<hash>/0 去重后 1 项，且 PostID 断言为 tC1hauHuVvqRusojhhXe5g；TestParseHTMLFixture 的 post_id 期望改为 abcdefghij。
- 备注：—

### T11 · 油猴脚本独立运行、短链 id 与去重 — 通过（2026-10-22 04:13）

- 步骤：S6
- 方法/命令：—
- 证据：node --check 通过；node userscript/test.js 输出 "USERSCRIPT TESTS PASS png,jpg,gif"，断言 post_id=fixtureslug123、CDN 前缀变体去重后 1 项；浏览器 harness ?fs=1 通过 window.__fs=["公众号独立运行测试/99887766-1.png:70", ...-2.jpg:70, ...-3.gif:70]，saved=[]，说明新 File System Access 分支直接写入子目录且不依赖本地服务。
- 备注：—

### T12 · APK 重建并用桌面 JVM 验证新文章 — 通过（2026-10-22 04:13）

- 步骤：S5
- 方法/命令：—
- 证据：bash android/build-apk.sh 成功，apksigner verify 证书 SHA-256=607fdd18...；桌面 JVM 用同一份 core 跑 SelfTest 新文章：id=tC1hauHuVvqRusojhhXe5g、medias=3、ok=3 fail=0、落盘 3 文件 1,005,213 字节 PASS（SelfTest 已去掉硬编码 8 张，改为按媒体数校验递归文件数）。
- 备注：—

### T13 · compose 静态校验（容器构建受沙箱限制） — 部分通过（2026-10-22 04:14）

- 步骤：S3
- 方法/命令：—
- 证据：docker compose -f docker-compose.yml config 通过：image wechat-mp-downloader:1.1.0、network_mode: bridge、2178:2178、command --web -listen 0.0.0.0:2178 -o /download、宿主机目录挂载；docker build 仍因本沙箱无 CAP_SYS_ADMIN 报 "unshare: operation not permitted"，无法实际起容器。
- 备注：—

### T14 · 针对“3 张变 6 张”的变体去重加固 — 通过（2026-10-22 04:37）

- 步骤：S7
- 方法/命令：—
- 证据：v1.1.3：go test 新增 TestParseSkipsMirroredPictureList、TestParseDedupesHashSuffixVariants、TestParseDedupesRepeatedContentVariants 全部 PASS（同一组图 6 个 hash 变体折叠为 3）；userscript node test 同前缀变体断言 PASS；WebUI /api/parse 返回 content_count=3、picture_count=3、medias=3；/api/download 任务 3 成功、文件 谁是乖狗狗呀？/tC1...-1.jpg 等 3 个。
- 备注：—

### T15 · 成对镜像图集去重（3 张变 6 张补强） — 通过（2026-10-22 04:42）

- 步骤：S7
- 方法/命令：—
- 证据：v1.1.4 新增 TestParseSkipsDoubledMirroredPictureList（正文 2 张、图集 4 条成对重复 → medias=2）并 PASS；全部 go test PASS；userscript 同步 doubleMirror + SHA-256 字节去重，node test PASS；dist 1.1.4 checksums 全成功、APK 签名通过；对 tC1hauHuVvqRusojhhXe5g 的 CLI 解析仍为 3 张。
- 备注：—

### T16 · CLI 合集批量下载（前 2 篇 10 张原图） — 通过（2026-10-22 10:29）

- 步骤：S9
- 方法/命令：`./build/wxmp-linux-amd64 -o /tmp/cli12 -max-articles 2 'https://mp.weixin.qq.com/mp/appmsgalbum?__biz=MzkzOTYwMzE0NQ==&action=getalbum&album_id=3186378941922820100'`
- 证据：两篇各 5 张：完成 成功 5 失败 0（7.7 MB）、成功 5 失败 0（6.8 MB）；find /tmp/cli12 -type f | wc -l = 10；路径 猫咪/106-公司的后辈也是猫妖吗/2247492190-1.png…-5.gif、猫咪/105-就算是同事也是会吃醋的/2247492134-1.png…-5.gif。
- 备注：106 篇合集解析约 2.3s；GIF 原图正常。

### T17 · WebUI 合集解析与批量下载（1.2.0） — 通过（2026-10-22 10:29）

- 步骤：S9
- 方法/命令：`curl POST /api/parse + /api/download (max_articles=1) on 127.0.0.1:2184`
- 证据：/api/parse 返回 collections=1，title=猫咪，account=涡轮酱呀，article_count=106，articles=106；/api/download 创建任务后 status=done，total=5 success=5 failed=0 skipped=0。
- 备注：—

### T18 · Android 合集解析与下载（同源代码 JVM） — 通过（2026-10-22 10:29）

- 步骤：S9
- 方法/命令：`javac core+AlbumTest/AlbumDownloadTest + java on JVM`
- 证据：AlbumParser.fetch(...,2) 输出 猫咪|涡轮酱呀|3186378941922820100|count=106|parsed=2，order 106/105；首篇 medias=5；名称 猫咪/106-公司的后辈也是猫妖吗/2247492190-1.png；两篇实际下载 TOTAL ok=10 skip=0 fail=0，/tmp/androidalbum 下 10 个文件。
- 备注：—

### T19 · 独立版油猴合集多选批量下载 — 通过（2026-10-22 10:29）

- 步骤：S9
- 方法/命令：`Chromium CDP + /tmp/usc-album harness: toggle multi, select 2, batch download`
- 证据：window.__saved=["测试文章标题-fixture1-1.png","测试文章标题-fixture2-1.png"]；status="完成：2/2 篇，2 张成功"；无本地服务请求，重名/同字节图片按“与已有图片内容相同，跳过”处理。
- 备注：—

### T20 · 推送版油猴文章/合集多选推送（mock 服务） — 通过（2026-10-22 10:29）

- 步骤：S9
- 方法/命令：`Chromium CDP + /tmp/usc-push harness (GET /api/health, POST /api/download, GET /api/jobs/job1 mocked)`
- 证据：文章页推送 body={"urls":["http://127.0.0.1:8100/s/fixture1.html"]}；合集页进入多选、勾选 2 篇后 body={"urls":[".../fixture1",".../fixture2"]}；状态 "done · 2/2 · 成功 2，跳过 0，失败 0"，日志 "已提交 2 篇 → 任务 job1✓ 下载完成"。
- 备注：—

### T21 · Android APK 合集解析与批量下载（模拟器实测） — 通过（2026-10-22 10:46）

- 步骤：S9
- 方法/命令：`adb install -r dist/wxmp-android.apk; am start -n com.wxmp.downloader/.MainActivity -a android.intent.action.SEND -t text/plain --es android.intent.extra.TEXT '<album url>'; 点“解析文章”→“开始下载”约 35s 后“取消”`
- 证据：模拟器解析出「猫咪 · 涡轮酱呀 · 合集 id 3186378941922820100 · 106 篇」；开始下载后进度 3/5，日志显示第 1/106 篇《公司的后辈也是猫妖吗》写入 猫咪/106-公司的后辈也是猫妖吗/2247492190-1.png（1.64 MB）、-2.png（1.75 MB）、-3.png（2.05 MB）；取消后状态「已取消：26 成功 · 0 跳过 · 0 失败」。截图 docs/android-home.png、docs/android-album.png、docs/android-album-download.png。
- 备注：—

### T22 · 油猴弹窗内多选模式与推送版按钮错位修复 — 通过（2026-10-22 12:14）

- 步骤：S9
- 方法/命令：`Chromium CDP：/tmp/usc-album 与 /tmp/usc-push 合集 harness；点击 FAB 检查弹窗 display，读取 push FAB computed bottom`
- 证据：独立版：弹窗关闭时 display=none，点击“合集批量下载”后 display=block 且含“进入多选模式”；勾选 2 篇后 saved=["测试文章标题-fixture1-1.png","测试文章标题-fixture2-1.png"]，status="完成：2/2 篇，2 张成功"。推送版：album-fab bottom=96px、弹窗 display none→block，含切换按钮；勾选 2 篇 POST {"urls":[".../fixture1",".../fixture2"]}，状态 done 2/2。文章页 push .fab bottom=96px、.panel bottom=158px。
- 备注：—

### T23 · 合集页保存位置/命名规则与推送 out_dir 生效 — 通过（2026-10-22 12:31）

- 步骤：S9
- 方法/命令：`Chromium CDP（/tmp/usc-album、/tmp/usc-push）+ 本地 WebUI：POST /api/download 带 out_dir/name_template，核对落盘`
- 证据：独立版合集弹窗：选择 mock 目录后 __fs=["测试目录/测试文章标题/1.png:70","测试目录/测试文章标题/1.png:70"]（自定义模板 <title>/<order>.<ext>，saved=[] 表示未走浏览器下载）。推送版合集弹窗：保存后 POST {"urls":[".../fixture1",".../fixture2"],"out_dir":"/download/猫咪","name_template":"<title>/<post_id>-<order>.<ext>"}；文章面板同样带 out_dir/name_template。真实服务端 POST 到 2185，job status=done success=3 out_dir=/tmp/pushout，文件落 /tmp/pushout/谁是乖狗狗呀？/2247496476-1.jpg、-2.jpg、-3.png。
- 备注：—

### T24 · 推送路径留空回退与自定义 out_dir 落盘 — 通过（2026-10-22 12:33）

- 步骤：S9
- 方法/命令：`Chromium CDP + dist/wxmp-linux-amd64（2186）+ curl POST /api/download`
- 证据：推送版留空时 POST 只有 {"urls":[...]}（不覆盖服务端默认）；填目录/模板后 POST 含 out_dir/name_template。用 dist 1.2.0 起服务验证：POST {"out_dir":"/tmp/pushout2","name_template":"<title>/<post_id>-<order>.<ext>"}，job done success=3 failed=0，落盘 /tmp/pushout2/谁是乖狗狗呀？/tC1hauHuVvqRusojhhXe5g-1.jpg、-2.jpg、-3.png，证明短链 id 与自定义目录/规则均生效。
- 备注：—

### T25 · WebUI 刷新后恢复当前任务进度 — 通过（2026-10-22 15:26）

- 步骤：S9
- 方法/命令：`Chromium CDP：2187 WebUI 启动合集任务后写入 wxmp.job.v1 并 reload，读取 #jobMeta/#statDone/#btnCancel`
- 证据：恢复后 #cardProgress hidden=false，meta="任务 20261022-151921-ff4fedaf9b · /tmp/refreshout · 并发 6 · 共 31 项"，cancelHidden=false，logs=47；5 秒后 meta 变为“共 72 项”，done 71、ok 32、skip 39，证明刷新后仍在实时更新。完成态任务刷新同样恢复：meta="完成 · 成功 39"，results=39。截图 docs/webui-refresh.png。
- 备注：—

### T26 · 修复共享前缀图片被误合并（4 图缺漏） — 通过（2026-10-22 15:26）

- 步骤：S9
- 方法/命令：`./dist/wxmp-linux-amd64 -o /tmp/art4dl -name '<order>.<ext>' '<mid=2247488472 文章>' + node userscript/test.js + file 输出检查`
- 证据：CLI：图片：4 张；成功 4，跳过 0，失败 0，共 9.3 MB；文件 1.png 1280x1807 2.4MB、2.png 同尺寸 2.4MB、3.png 2.5MB、4.gif GIF89a 300x300 2.0MB。油猴对保存的同一 HTML 解析：4 项（3 image + 1 gif）。回归：旧 8 图文章仍 8 张；图片共享 33 字符前缀时不再合并（Go 测试与 node 断言通过）。
- 备注：—
