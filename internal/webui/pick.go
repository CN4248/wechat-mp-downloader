package webui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"wxmp/internal/wx"
)

// PickItem is one media item selected in the userscript's in-page multi-select
// mode. URL is the original CDN address (size segment /0) the userscript built
// with the same rules as the article parser.
type PickItem struct {
	URL    string `json:"url"`
	Ext    string `json:"ext"`
	Kind   string `json:"kind"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	IsGIF  bool   `json:"is_gif"`
}

// MediaPick is the payload of "push picked media": the article page (used only
// for the naming context) plus the media URLs the user clicked in the page.
type MediaPick struct {
	PageURL string     `json:"page_url"`
	Picked  []PickItem `json:"picked"`
}

const maxPickedMedia = 500

// WeChat only serves article media from its own CDNs. Rejecting every other
// host keeps this endpoint from becoming a generic URL fetcher.
var pickHostSuffixes = []string{"qpic.cn", "qlogo.cn", "weixin.qq.com", "qq.com"}

func pickHostAllowed(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, suffix := range pickHostSuffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

func normalizePickKind(p PickItem) string {
	switch strings.ToLower(strings.TrimSpace(p.Kind)) {
	case "video":
		return "video"
	case "gif":
		return "gif"
	case "image":
		return "image"
	}
	if p.IsGIF {
		return "gif"
	}
	if strings.Contains(strings.ToLower(p.URL), ".mp4") {
		return "video"
	}
	return "image"
}

func normalizePickExt(p PickItem) string {
	kind := normalizePickKind(p)
	if kind == "video" {
		return "mp4"
	}
	if kind == "gif" || p.IsGIF {
		return "gif"
	}
	e := strings.ToLower(strings.TrimSpace(p.Ext))
	e = strings.TrimPrefix(e, ".")
	if e == "jpeg" {
		e = "jpg"
	}
	if e == "" {
		return "jpg"
	}
	return e
}

func validPickExt(e string) bool {
	switch e {
	case "jpg", "png", "gif", "webp", "bmp", "mp4":
		return true
	}
	return false
}

// normalizePickTasks validates a media-pick request and turns it into a single
// job task. Everything else about the naming still comes from the article
// parse, so names are identical to a full-article download.
func (m *Manager) normalizePickTasks(req DownloadRequest) ([]JobTask, string, error) {
	pick := req.Pick
	if pick == nil || len(pick.Picked) == 0 {
		return nil, "", errors.New("没有选中的媒体")
	}
	if len(pick.Picked) > maxPickedMedia {
		return nil, "", fmt.Errorf("一次最多推送 %d 项媒体", maxPickedMedia)
	}
	pageURL := strings.TrimSpace(pick.PageURL)
	if wx.IsAlbumURL(pageURL) {
		return nil, "", errors.New("多选推送只支持文章页，请先打开一篇文章")
	}
	pageURL, err := wx.NormalizeArticleURL(pageURL)
	if err != nil {
		return nil, "", fmt.Errorf("文章链接无效：%w", err)
	}

	tpl := strings.TrimSpace(req.NameTemplate)
	if tpl == "" {
		tpl = m.defaultName
	}
	if err := wx.ValidateTemplate(tpl); err != nil {
		return nil, "", err
	}
	abs, err := m.defaultOutDir(req.OutDir)
	if err != nil {
		return nil, "", err
	}

	seen := map[string]bool{}
	cleaned := make([]PickItem, 0, len(pick.Picked))
	for i, item := range pick.Picked {
		raw := strings.TrimSpace(item.URL)
		u, parseErr := url.Parse(raw)
		if parseErr != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, "", fmt.Errorf("第 %d 项媒体地址无效", i+1)
		}
		if !pickHostAllowed(u.Hostname()) {
			return nil, "", fmt.Errorf("第 %d 项媒体地址不是微信图床：%s", i+1, u.Hostname())
		}
		if item.Width < 0 || item.Height < 0 || item.Width > 100000 || item.Height > 100000 {
			return nil, "", fmt.Errorf("第 %d 项媒体尺寸异常", i+1)
		}
		if rawExt := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(item.Ext), ".")); rawExt != "" && !validPickExt(rawExt) {
			return nil, "", fmt.Errorf("第 %d 项媒体类型不支持：%s", i+1, item.Ext)
		}
		item.URL = raw
		item.Kind = normalizePickKind(item)
		item.Ext = normalizePickExt(item)
		key := wx.MediaKey(item.URL)
		if key == "" {
			return nil, "", fmt.Errorf("第 %d 项媒体地址无效", i+1)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		cleaned = append(cleaned, item)
	}
	if len(cleaned) == 0 {
		return nil, "", errors.New("没有有效的选中媒体")
	}
	pick.PageURL = pageURL
	pick.Picked = cleaned

	task := JobTask{URL: pageURL, OutDir: abs, NameTemplate: tpl}
	return []JobTask{task}, abs, nil
}

// selectPickedMedias maps the pushed items onto the server-side article parse.
// Matched items reuse the parsed media object, so seq/ext/kind and therefore
// the rendered file names are exactly the same as a full-article download.
// Items the parse cannot see (for example a video widget without a direct
// address) fall back to the pushed URL and continue after the parsed seq.
func selectPickedMedias(art *wx.Article, picks []PickItem) ([]*wx.Media, int) {
	index := map[string]*wx.Media{}
	maxSeq := 0
	if art != nil {
		for _, m := range art.Medias {
			if m == nil {
				continue
			}
			if m.Seq > maxSeq {
				maxSeq = m.Seq
			}
			key := wx.MediaKey(m.URL)
			if key == "" {
				continue
			}
			if _, ok := index[key]; !ok {
				index[key] = m
			}
		}
	}

	seen := map[string]bool{}
	out := make([]*wx.Media, 0, len(picks))
	fallback := 0
	for _, p := range picks {
		key := wx.MediaKey(p.URL)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		if m, ok := index[key]; ok {
			out = append(out, m)
			continue
		}

		fallback++
		maxSeq++
		kind := normalizePickKind(p)
		ext := normalizePickExt(p)
		mediaURL := strings.TrimSpace(p.URL)
		if strings.HasPrefix(mediaURL, "//") {
			mediaURL = "https:" + mediaURL
		}
		if kind != "video" {
			mediaURL = wx.OriginalURL(mediaURL, ext)
		}
		m := &wx.Media{
			Seq: maxSeq, Kind: kind, URL: mediaURL, PreviewURL: wx.ResizeURL(mediaURL, "300"),
			Ext: ext, Width: p.Width, Height: p.Height, IsGIF: kind == "gif" || p.IsGIF,
			Source: "pick",
		}
		if art != nil {
			m.Title = art.Title
			m.PostID = art.PostID
			m.Account = art.Account
			m.Author = art.Author
			m.Biz = art.Biz
			m.SN = art.SN
			m.Mid = art.Mid
			m.Idx = art.Idx
			m.PublishTime = art.PublishTime
			m.ArticleURL = art.URL
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, fallback
}

// runPick executes a "push picked media" job: fetch the article for the naming
// context, keep only the picked media, then run the normal downloader.
func (m *Manager) runPick(j *job) {
	pick := j.request.Pick
	tpl := taskTemplate(j.tasks)
	if tpl == "" {
		tpl = m.defaultName
	}
	outDir := ""
	if len(j.tasks) > 0 {
		outDir = j.tasks[0].OutDir
	}
	j.logf("推送选中媒体 %d 项 · %s", len(pick.Picked), pick.PageURL)

	ctx, cancel := context.WithTimeout(j.ctx, 90*time.Second)
	page, final, err := m.client.FetchPage(ctx, pick.PageURL)
	cancel()
	if err != nil {
		v := &ArticleView{Index: 0, URL: pick.PageURL, FinalURL: pick.PageURL, Title: "解析失败", Error: err.Error(), OutDir: outDir, NameTemplate: tpl}
		j.addArticle(v, nil, j.tasks[0])
		j.mu.Lock()
		j.errText = "读取文章失败：" + err.Error()
		j.mu.Unlock()
		j.logf("✗ 读取文章失败：%v", err)
		return
	}

	art := wx.ParseHTML(page, final, wx.ParseOptions{IncludeVideo: true})
	if m.subs != nil && art.Biz != "" {
		m.subs.RememberAccount(wx.AccountOf(page, final, art))
	}
	medias, fallback := selectPickedMedias(art, pick.Picked)
	v := buildArticleView(art, tpl, 0)
	v.OutDir = outDir
	v.NameTemplate = tpl
	if len(medias) == 0 {
		v.Error = "选中的媒体都不在文章里（内容可能已更新）"
		j.addArticle(v, nil, j.tasks[0])
		j.mu.Lock()
		j.errText = v.Error
		j.mu.Unlock()
		j.logf("✗ %s", v.Error)
		return
	}
	j.addArticle(v, medias, j.tasks[0])
	j.mu.Lock()
	j.total = len(medias)
	j.mu.Unlock()
	if fallback > 0 {
		j.logf("提示：%d 项未在服务端解析结果中，按推送地址直接下载", fallback)
	}
	j.logf("✓ 《%s》按选中列表下载 %d 项", art.Title, len(medias))
	m.remember(j.snapshot())
	if j.paused() {
		return
	}

	d := &wx.Downloader{
		Client:       m.client,
		OutDir:       outDir,
		Jobs:         j.jobs,
		Force:        j.force,
		NameTemplate: tpl,
		OnEvent: func(ev wx.Event) {
			if ev.Result != nil {
				j.addResult(ev.Result, 0)
				m.remember(j.snapshot())
			}
		},
	}
	d.Run(j.ctx, medias)
}

// handleDownloadMedia starts a job that downloads exactly the picked media.
func (s *Server) handleDownloadMedia(w http.ResponseWriter, r *http.Request) {
	var req DownloadRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Pick == nil {
		writeError(w, http.StatusBadRequest, "缺少选中的媒体（pick）")
		return
	}
	view, err := s.mgr.Start(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": view})
}
