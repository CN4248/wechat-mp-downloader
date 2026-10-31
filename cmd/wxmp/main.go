package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"wxmp/internal/webui"
	"wxmp/internal/wx"
)

var version = "1.2.1"

func main() {
	var (
		outDir    = flag.String("o", defaultOut(), "下载目录")
		jobs      = flag.Int("j", 4, "并发下载数 (1-16)")
		nameTpl   = flag.String("name", wx.DefaultNameTemplate, "单篇重命名规则，例如 <title>/<post_id>-<order>.<ext>")
		collTpl   = flag.String("collection-name", wx.DefaultCollectionTemplate, "合集重命名规则，额外支持 <collection_title>/<collection_id>/<collection_order>")
		force     = flag.Bool("force", false, "覆盖已存在的文件")
		cover     = flag.Bool("cover", false, "同时下载封面图")
		video     = flag.Bool("video", false, "尝试下载内嵌视频（实验性）")
		dryRun    = flag.Bool("dry-run", false, "只解析并打印将要保存的路径，不下载")
		verbose   = flag.Bool("v", false, "输出图片原始链接与详细信息")
		cookie    = flag.String("cookie", os.Getenv("WXMP_COOKIE"), "微信 Cookie（仅内存使用，不落盘）")
		web       = flag.Bool("web", false, "启动 WebUI")
		listen    = flag.String("listen", "127.0.0.1:2178", "WebUI 监听地址")
		noBrowser = flag.Bool("no-browser", false, "启动 WebUI 后不自动打开浏览器")
		maxArt    = flag.Int("max-articles", 0, "合集最多解析多少篇（0=全部，调试用）")
		showVer   = flag.Bool("version", false, "打印版本")
	)
	flag.Usage = usage
	flag.Parse()

	if *showVer {
		fmt.Println("wxmp " + version)
		return
	}
	if err := wx.ValidateTemplate(*nameTpl); err != nil {
		fmt.Fprintln(os.Stderr, "命名规则错误："+err.Error())
		os.Exit(2)
	}
	if err := wx.ValidateTemplate(*collTpl); err != nil {
		fmt.Fprintln(os.Stderr, "合集命名规则错误："+err.Error())
		os.Exit(2)
	}

	args := flag.Args()
	if len(args) == 0 && !*web {
		if piped, _ := stdinURLs(); len(piped) > 0 {
			args = piped
		}
	}
	if *web || len(args) == 0 {
		cfg := webui.Config{
			DefaultOut:     *outDir,
			Jobs:           *jobs,
			Listen:         *listen,
			Version:        version,
			NoBrowser:      *noBrowser,
			NameTemplate:   *nameTpl,
			CollectionName: *collTpl,
		}
		if err := webui.Run(context.Background(), cfg); err != nil {
			fmt.Fprintln(os.Stderr, "WebUI 启动失败："+err.Error())
			os.Exit(1)
		}
		return
	}
	if len(args) > 30 {
		fmt.Fprintln(os.Stderr, "一次最多 30 条链接")
		os.Exit(2)
	}

	c := wx.NewClient()
	c.Cookie = *cookie

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	code := 0
	var totalOK, totalSkip, totalFail int
	for i, raw := range args {
		if len(args) > 1 {
			fmt.Printf("\n===== [%d/%d] %s =====\n", i+1, len(args), strings.TrimSpace(raw))
		}
		if wx.IsAlbumURL(raw) {
			max := wx.MaxCollectionArticles
			if *maxArt > 0 && *maxArt < max {
				max = *maxArt
			}
			col, cerr := c.FetchCollection(ctx, raw, max)
			if cerr != nil {
				fmt.Fprintln(os.Stderr, "合集解析失败："+cerr.Error())
				code = 1
				continue
			}
			fmt.Printf("合集：%s · %s · %d 篇\n", col.Title, col.Account, len(col.Articles))
			for j, ca := range col.Articles {
				fmt.Printf("\n--- [%d/%d] #%d %s ---\n", j+1, len(col.Articles), ca.Order, ca.Title)
				ok, skip, fail, err := processOne(ctx, c, ca.URL, *outDir, *jobs, *dryRun, *force, *cover, *video, *collTpl, *verbose, col, ca)
				totalOK += ok
				totalSkip += skip
				totalFail += fail
				if err != nil {
					fmt.Fprintln(os.Stderr, "错误："+err.Error())
					code = 1
				}
			}
			continue
		}
		ok, skip, fail, err := processOne(ctx, c, raw, *outDir, *jobs, *dryRun, *force, *cover, *video, *nameTpl, *verbose, nil, nil)
		totalOK += ok
		totalSkip += skip
		totalFail += fail
		if err != nil {
			fmt.Fprintln(os.Stderr, "错误："+err.Error())
			code = 1
		}
	}
	if len(args) > 1 || totalFail > 0 {
		fmt.Printf("\n合计：成功 %d，跳过 %d，失败 %d\n", totalOK, totalSkip, totalFail)
	}
	if totalFail > 0 {
		code = 1
	}
	if code != 0 {
		os.Exit(code)
	}
}

func processOne(ctx context.Context, c *wx.Client, raw, outDir string, jobs int, dry, force, cover, video bool, nameTpl string, verbose bool, coll *wx.Collection, ca *wx.CollectionArticle) (int, int, int, error) {
	start := time.Now()
	html, final, err := c.FetchPage(ctx, raw)
	if err != nil {
		return 0, 0, 0, err
	}
	art := wx.ParseHTML(html, final, wx.ParseOptions{IncludeCover: cover, IncludeVideo: video})
	if coll != nil && ca != nil {
		art.CollectionID, art.CollectionTitle, art.CollectionOrder = coll.ID, coll.Title, ca.Order
		for _, m := range art.Medias {
			m.CollectionID, m.CollectionTitle, m.CollectionOrder = coll.ID, coll.Title, ca.Order
		}
	}
	fmt.Printf("标题：%s\n公众号：%s\n发布：%s\n文章 id：%s\n图片：%d 张", art.Title, art.Account, fmtTime(art.PublishTime), art.PostID, len(art.Medias))
	if art.VideoCount > 0 {
		fmt.Printf("，视频：%d 个", art.VideoCount)
	}
	fmt.Printf("（解析 %.2fs）\n", time.Since(start).Seconds())
	for _, w := range art.Warnings {
		fmt.Println("提示：" + w)
	}
	if len(art.Medias) == 0 {
		return 0, 0, 0, fmt.Errorf("没有解析到可下载的图片")
	}

	if dry {
		ok, skip, fail := 0, 0, 0
		for _, m := range art.Medias {
			rel, buildErr := wx.BuildRelPath(m, nameTpl)
			if buildErr != nil {
				fmt.Printf("  %2d  ✗ %v\n", m.Seq, buildErr)
				fail++
				continue
			}
			planned := filepath.Join(outDir, filepath.FromSlash(rel))
			status := "待下载"
			if st, statErr := os.Stat(planned); statErr == nil && st.Size() > 0 {
				status = "已存在"
				skip++
			} else {
				ok++
			}
			fmt.Printf("  %2d  %-9s %4dx%-4d  %-5s  %s\n", m.Seq, status, m.Width, m.Height, strings.ToUpper(m.Ext), rel)
			if verbose {
				fmt.Printf("      %s\n", m.URL)
			}
		}
		fmt.Printf("预计下载 %d，已存在 %d，命名失败 %d\n", ok, skip, fail)
		if fail > 0 {
			return ok, skip, fail, fmt.Errorf("%d 个文件命名失败", fail)
		}
		return ok, skip, 0, nil
	}

	d := &wx.Downloader{Client: c, OutDir: outDir, Jobs: jobs, Force: force, NameTemplate: nameTpl}
	results := d.Run(ctx, art.Medias)
	ok, skip, fail := 0, 0, 0
	var bytes int64
	for _, r := range results {
		switch r.Status {
		case "ok":
			ok++
			bytes += r.Size
			fmt.Printf("  ✓ %2d  %4dx%-4d  %-9s  %s\n", r.Media.Seq, r.Width, r.Height, wx.ParseSize(r.Size), r.RelPath)
		case "skipped":
			skip++
			fmt.Printf("  = %2d  已存在       %s\n", r.Media.Seq, r.RelPath)
		default:
			fail++
			fmt.Printf("  ✗ %2d  失败         %s：%s\n", r.Media.Seq, r.Media.URL, r.Error)
		}
		if r.Warning != "" {
			fmt.Printf("      ⚠ %s\n", r.Warning)
		}
		if verbose && r.MD5 != "" {
			fmt.Printf("      MD5 %s\n", r.MD5)
		}
	}
	fmt.Printf("完成：成功 %d，跳过 %d，失败 %d，共 %s\n", ok, skip, fail, wx.ParseSize(bytes))
	if fail > 0 {
		return ok, skip, fail, fmt.Errorf("%d 个文件下载失败", fail)
	}
	return ok, skip, 0, nil
}

func fmtTime(ts int64) string {
	if ts <= 0 {
		return "未知"
	}
	return time.Unix(ts, 0).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")
}

func defaultOut() string {
	if v := strings.TrimSpace(os.Getenv("WXMP_OUT")); v != "" {
		return v
	}
	if st, err := os.Stat("/download"); err == nil && st.IsDir() {
		return "/download"
	}
	return "downloads"
}

func stdinURLs() ([]string, error) {
	st, err := os.Stdin.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice != 0 {
		return nil, nil
	}
	var out []string
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, sc.Err()
}

func usage() {
	fmt.Fprintf(os.Stderr, `wxmp %s — 微信公众号文章图片/动图原图下载器

用法:
  wxmp [选项] <文章链接> [更多链接...]
  wxmp --web [-listen 0.0.0.0:2178]

示例:
  wxmp -o ./out https://mp.weixin.qq.com/s/xxxx
  wxmp -o /download -name '<标题>/<文章id>-<图片序号>.<扩展名>' https://mp.weixin.qq.com/s/xxxx
  wxmp --web -listen 0.0.0.0:2178

选项:
`, version)
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, `
命名变量: <title> <post_id> <order> <ext> <account> <author> <date> <time>
          <year> <month> <day> <kind> <width> <height> <order2> <biz> <sn> <mid> <idx>
中文别名: <文章标题> <文章id> <图片序号> <扩展名> <公众号> <作者> <日期> <时间>
          模板中的 / 会创建子文件夹，例如 <文章标题>/<文章id>-<图片序号>.<扩展名>
`)
}
