package webui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"wxmp/internal/wx"
)

// ---------- API view models ----------

// MediaView is one preview item.
type MediaView struct {
	Index      int    `json:"index"`
	Seq        int    `json:"seq"`
	Kind       string `json:"kind"`
	URL        string `json:"url"`
	PreviewURL string `json:"preview_url"`
	Ext        string `json:"ext"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	IsGIF      bool   `json:"is_gif"`
	Name       string `json:"name"`
	RelPath    string `json:"rel_path"`
	Source     string `json:"source"`
	FileID     string `json:"file_id,omitempty"`

	Title       string `json:"title"`
	PostID      string `json:"post_id"`
	Account     string `json:"account"`
	Author      string `json:"author"`
	Biz         string `json:"biz"`
	SN          string `json:"sn"`
	Mid         string `json:"mid"`
	Idx         string `json:"idx"`
	PublishTime int64  `json:"publish_time"`
	ArticleURL  string `json:"article_url"`

	CollectionID    string `json:"collection_id,omitempty"`
	CollectionTitle string `json:"collection_title,omitempty"`
	CollectionOrder int    `json:"collection_order,omitempty"`
}

// ArticleView is the parse result rendered for the WebUI.
type ArticleView struct {
	Index           int          `json:"index"`
	URL             string       `json:"url"`
	FinalURL        string       `json:"final_url"`
	Title           string       `json:"title"`
	Account         string       `json:"account"`
	Author          string       `json:"author"`
	PostID          string       `json:"post_id"`
	PublishTime     int64        `json:"publish_time"`
	PublishText     string       `json:"publish_text"`
	ImageCount      int          `json:"image_count"`
	GIFCount        int          `json:"gif_count"`
	ContentCount    int          `json:"content_count"`
	PictureCount    int          `json:"picture_count"`
	VideoCount      int          `json:"video_count"`
	CollectionID    string       `json:"collection_id,omitempty"`
	CollectionTitle string       `json:"collection_title,omitempty"`
	CollectionOrder int          `json:"collection_order,omitempty"`
	Medias          []*MediaView `json:"medias"`
	Warnings        []string     `json:"warnings"`
	Error           string       `json:"error"`
}

// CollectionArticleView is one row in an album preview.
type CollectionArticleView struct {
	Index       int    `json:"index"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Order       int    `json:"order"`
	MsgID       string `json:"msgid"`
	ItemIdx     string `json:"itemidx"`
	PublishTime int64  `json:"publish_time"`
	Cover       string `json:"cover"`
}

// CollectionView is the album preview rendered for the WebUI.
type CollectionView struct {
	Index        int                      `json:"index"`
	URL          string                   `json:"url"`
	FinalURL     string                   `json:"final_url"`
	ID           string                   `json:"id"`
	Title        string                   `json:"title"`
	Account      string                   `json:"account"`
	Description  string                   `json:"description"`
	ArticleCount int                      `json:"article_count"`
	Articles     []*CollectionArticleView `json:"articles"`
	Warnings     []string                 `json:"warnings"`
	Error        string                   `json:"error"`
}

// FileView is one finished download.
type FileView struct {
	ArticleIndex int    `json:"article_index"`
	Seq          int    `json:"seq"`
	Name         string `json:"name"`
	RelPath      string `json:"rel_path"`
	Size         int64  `json:"size"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`
	Warning      string `json:"warning,omitempty"`
	Ext          string `json:"ext"`
	IsGIF        bool   `json:"is_gif"`
	PreviewURL   string `json:"preview_url"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
}

// LogLine is one job log entry.
type LogLine struct {
	Time string `json:"time"`
	Text string `json:"text"`
}

// JobView is the polled job snapshot.
type JobView struct {
	ID                 string         `json:"id"`
	Status             string         `json:"status"`
	URLs               []string       `json:"urls"`
	OutDir             string         `json:"out_dir"`
	NameTemplate       string         `json:"name_template"`
	CollectionTemplate string         `json:"collection_template"`
	Jobs               int            `json:"jobs"`
	Force              bool           `json:"force"`
	Total              int            `json:"total"`
	Done               int            `json:"done"`
	Success            int            `json:"success"`
	Skipped            int            `json:"skipped"`
	Failed             int            `json:"failed"`
	Articles           []*ArticleView `json:"articles"`
	Results            []*FileView    `json:"results"`
	Logs               []LogLine      `json:"logs"`
	Error              string         `json:"error"`
	StartedAt          int64          `json:"started_at"`
	FinishedAt         int64          `json:"finished_at"`
}

// DownloadRequest starts a download job.
type DownloadRequest struct {
	URL                string   `json:"url"`
	URLs               []string `json:"urls"`
	OutDir             string   `json:"out_dir"`
	Jobs               int      `json:"jobs"`
	Force              bool     `json:"force"`
	Cover              bool     `json:"cover"`
	Video              bool     `json:"video"`
	NameTemplate       string   `json:"name_template"`
	CollectionTemplate string   `json:"collection_template"`
	MaxArticles        int      `json:"max_articles"`
}

// PreviewRequest renders names for a template.
type PreviewRequest struct {
	Template string      `json:"template"`
	Medias   []*wx.Media `json:"medias"`
}

type parsedArticle struct {
	View   *ArticleView
	Medias []*wx.Media
}

type job struct {
	id                 string
	urls               []string
	outDir             string
	nameTemplate       string
	collectionTemplate string
	maxArticles        int
	jobs               int
	force              bool
	cover              bool
	video              bool

	ctx    context.Context
	cancel context.CancelFunc

	mu         sync.Mutex
	status     string
	total      int
	done       int
	success    int
	skipped    int
	failed     int
	articles   []*parsedArticle
	results    []*FileView
	logs       []LogLine
	errText    string
	startedAt  time.Time
	finishedAt time.Time
}

const maxLogs = 500

func newJob(req DownloadRequest, urls []string, outDir, tpl, ctpl string) *job {
	ctx, cancel := context.WithCancel(context.Background())
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return &job{
		id:                 time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b),
		urls:               urls,
		outDir:             outDir,
		nameTemplate:       tpl,
		collectionTemplate: ctpl,
		maxArticles:        req.MaxArticles,
		jobs:               req.Jobs,
		force:              req.Force,
		cover:              req.Cover,
		video:              req.Video,
		ctx:                ctx,
		cancel:             cancel,
		status:             "running",
		startedAt:          time.Now(),
	}
}

func (j *job) logf(format string, args ...any) {
	line := LogLine{Time: time.Now().Format("15:04:05"), Text: fmt.Sprintf(format, args...)}
	j.mu.Lock()
	if len(j.logs) >= maxLogs {
		copy(j.logs, j.logs[len(j.logs)-maxLogs+1:])
		j.logs = j.logs[:maxLogs-1]
	}
	j.logs = append(j.logs, line)
	j.mu.Unlock()
}

func (j *job) addArticle(v *ArticleView, medias []*wx.Media) {
	j.mu.Lock()
	j.articles = append(j.articles, &parsedArticle{View: v, Medias: medias})
	j.mu.Unlock()
}

func (j *job) setTotal(n int) {
	j.mu.Lock()
	j.total = n
	j.mu.Unlock()
}

func (j *job) addResult(r *wx.Result, articleIndex int) {
	view := &FileView{
		ArticleIndex: articleIndex,
		Size:         r.Size,
		Status:       r.Status,
		Ext:          r.Media.Ext,
		IsGIF:        r.Media.IsGIF,
		Width:        r.Width,
		Height:       r.Height,
		PreviewURL:   wx.ResizeURL(r.Media.URL, "300"),
	}
	if r.Media != nil {
		view.Seq = r.Media.Seq
	}
	view.Name = filepath.Base(r.Path)
	view.RelPath = r.RelPath
	view.Error = r.Error
	view.Warning = r.Warning

	j.mu.Lock()
	j.done++
	switch r.Status {
	case "ok":
		j.success++
	case "skipped":
		j.skipped++
	default:
		j.failed++
	}
	j.results = append(j.results, view)
	j.mu.Unlock()

	switch r.Status {
	case "ok":
		j.logf("✓ #%d %s · %s", view.Seq, view.RelPath, wx.ParseSize(r.Size))
	case "skipped":
		j.logf("= #%d 已存在 · %s", view.Seq, view.RelPath)
	case "canceled":
		j.logf("× #%d 已取消", view.Seq)
	default:
		j.logf("✗ #%d 失败 · %s", view.Seq, r.Error)
	}
}

func (j *job) finish() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.finishedAt = time.Now()
	switch {
	case j.ctx.Err() != nil:
		j.status = "canceled"
	case j.failed > 0 && j.success == 0 && j.skipped == 0:
		j.status = "failed"
	case j.total == 0 && j.errText != "":
		j.status = "failed"
	default:
		j.status = "done"
	}
	if j.errText == "" && j.failed > 0 {
		j.errText = fmt.Sprintf("%d 个文件失败", j.failed)
	}
}

func (j *job) snapshot() *JobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	v := &JobView{
		ID:                 j.id,
		Status:             j.status,
		URLs:               append([]string(nil), j.urls...),
		OutDir:             j.outDir,
		NameTemplate:       j.nameTemplate,
		CollectionTemplate: j.collectionTemplate,
		Jobs:               j.jobs,
		Force:              j.force,
		Total:              j.total,
		Done:               j.done,
		Success:            j.success,
		Skipped:            j.skipped,
		Failed:             j.failed,
		Error:              j.errText,
		StartedAt:          j.startedAt.Unix(),
	}
	if !j.finishedAt.IsZero() {
		v.FinishedAt = j.finishedAt.Unix()
	}
	for _, pa := range j.articles {
		v.Articles = append(v.Articles, pa.View)
	}
	v.Results = append(v.Results, j.results...)
	v.Logs = append(v.Logs, j.logs...)
	return v
}

// ---------- manager ----------

// JobSummary is the lightweight history row returned by /api/jobs.
type JobSummary struct {
	ID         string   `json:"id"`
	Status     string   `json:"status"`
	URLs       []string `json:"urls"`
	OutDir     string   `json:"out_dir"`
	Total      int      `json:"total"`
	Done       int      `json:"done"`
	Success    int      `json:"success"`
	Skipped    int      `json:"skipped"`
	Failed     int      `json:"failed"`
	Error      string   `json:"error"`
	StartedAt  int64    `json:"started_at"`
	FinishedAt int64    `json:"finished_at"`
}

// Manager owns at most one running job and keeps every job snapshot on disk.
type Manager struct {
	client            *wx.Client
	defaultOut        string
	defaultName       string
	defaultCollection string
	dataDir           string

	mu        sync.Mutex
	jobs      map[string]*job
	order     []string
	activeID  string
	history   map[string]*JobView
	histOrder []string
}

// NewManager creates a job manager and restores the persisted history.
func NewManager(client *wx.Client, defaultOut, defaultName, defaultCollection, dataDir string) *Manager {
	m := &Manager{
		client:            client,
		defaultOut:        defaultOut,
		defaultName:       defaultName,
		defaultCollection: defaultCollection,
		dataDir:           dataDir,
		jobs:              map[string]*job{},
		history:           map[string]*JobView{},
	}
	m.loadHistory()
	return m
}

func (m *Manager) historyPath() string {
	return filepath.Join(m.dataDir, "jobs.json")
}

func (m *Manager) loadHistory() {
	raw, err := os.ReadFile(m.historyPath())
	if err != nil {
		return
	}
	var views []*JobView
	if err := json.Unmarshal(raw, &views); err != nil {
		return
	}
	changed := false
	for _, v := range views {
		if v == nil || strings.TrimSpace(v.ID) == "" {
			continue
		}
		if v.Status == "running" {
			v.Status = "failed"
			if v.Error == "" {
				v.Error = "服务重启导致任务中断"
			} else {
				v.Error += "；服务重启导致任务中断"
			}
			if v.FinishedAt == 0 {
				v.FinishedAt = time.Now().Unix()
			}
			changed = true
		}
		if _, ok := m.history[v.ID]; !ok {
			m.histOrder = append(m.histOrder, v.ID)
		}
		m.history[v.ID] = v
	}
	if changed {
		m.saveHistory()
	}
}

// saveHistory writes a copy of all known job snapshots atomically.
// Callers must not hold m.mu.
func (m *Manager) saveHistory() {
	m.mu.Lock()
	views := make([]*JobView, 0, len(m.histOrder))
	for _, id := range m.histOrder {
		if v := m.history[id]; v != nil {
			views = append(views, v)
		}
	}
	file := m.historyPath()
	m.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return
	}
	raw, err := json.MarshalIndent(views, "", " ")
	if err != nil {
		return
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, file)
}

func (m *Manager) remember(v *JobView) {
	if v == nil || v.ID == "" {
		return
	}
	m.mu.Lock()
	if _, ok := m.history[v.ID]; !ok {
		m.histOrder = append(m.histOrder, v.ID)
	}
	m.history[v.ID] = v
	m.mu.Unlock()
	m.saveHistory()
}

// List returns every persisted job, newest first.
func (m *Manager) List() []*JobSummary {
	m.mu.Lock()
	out := make([]*JobSummary, 0, len(m.history))
	for _, id := range m.histOrder {
		v := m.history[id]
		if v == nil {
			continue
		}
		out = append(out, &JobSummary{
			ID: v.ID, Status: v.Status, URLs: append([]string(nil), v.URLs...),
			OutDir: v.OutDir, Total: v.Total, Done: v.Done,
			Success: v.Success, Skipped: v.Skipped, Failed: v.Failed,
			Error: v.Error, StartedAt: v.StartedAt, FinishedAt: v.FinishedAt,
		})
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}

// Start validates the request and launches a job.
func (m *Manager) Start(req DownloadRequest) (*JobView, error) {
	urls := make([]string, 0, len(req.URLs)+1)
	if strings.TrimSpace(req.URL) != "" {
		urls = append(urls, req.URL)
	}
	urls = append(urls, req.URLs...)
	clean := make([]string, 0, len(urls))
	seen := map[string]bool{}
	for _, raw := range urls {
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == '\r' }) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			u, err := wx.NormalizeInputURL(part)
			if err != nil {
				return nil, err
			}
			if seen[u] {
				continue
			}
			seen[u] = true
			clean = append(clean, u)
		}
	}
	if len(clean) == 0 {
		return nil, fmt.Errorf("请先填写文章链接")
	}
	if len(clean) > 30 {
		return nil, fmt.Errorf("一次最多 30 条链接")
	}
	tpl := strings.TrimSpace(req.NameTemplate)
	if tpl == "" {
		tpl = m.defaultName
	}
	if err := wx.ValidateTemplate(tpl); err != nil {
		return nil, err
	}
	ctpl := strings.TrimSpace(req.CollectionTemplate)
	if ctpl == "" {
		ctpl = m.defaultCollection
	}
	if err := wx.ValidateTemplate(ctpl); err != nil {
		return nil, fmt.Errorf("合集命名规则：%w", err)
	}
	outDir := strings.TrimSpace(req.OutDir)
	if outDir == "" {
		outDir = m.defaultOut
	}
	outDir = expandHome(outDir)
	abs, err := filepath.Abs(outDir)
	if err != nil {
		return nil, fmt.Errorf("下载目录无效：%v", err)
	}
	if st, statErr := os.Stat(abs); statErr == nil && !st.IsDir() {
		return nil, fmt.Errorf("下载目录不是文件夹：%s", abs)
	}
	jobs := req.Jobs
	if jobs <= 0 {
		jobs = 4
	}
	if jobs > 16 {
		jobs = 16
	}

	m.mu.Lock()
	if m.activeID != "" {
		if active := m.jobs[m.activeID]; active != nil {
			active.mu.Lock()
			running := active.status == "running"
			active.mu.Unlock()
			if running {
				m.mu.Unlock()
				return nil, fmt.Errorf("已有任务正在运行，请等待完成或先取消")
			}
		}
	}
	j := newJob(req, clean, abs, tpl, ctpl)
	j.jobs = jobs
	m.jobs[j.id] = j
	m.order = append(m.order, j.id)
	m.activeID = j.id
	m.mu.Unlock()

	view := j.snapshot()
	m.remember(view)
	go m.run(j)
	return view, nil
}

func (m *Manager) run(j *job) {
	opts := wx.ParseOptions{IncludeCover: j.cover, IncludeVideo: j.video}

	type target struct {
		url     string
		coll    *wx.Collection
		article *wx.CollectionArticle
	}
	targets := make([]target, 0, len(j.urls))
	for i, u := range j.urls {
		if j.ctx.Err() != nil {
			break
		}
		if wx.IsAlbumURL(u) {
			j.logf("解析合集 %d/%d · %s", i+1, len(j.urls), u)
			ctx, cancel := context.WithTimeout(j.ctx, 180*time.Second)
			max := wx.MaxCollectionArticles
			if j.maxArticles > 0 && j.maxArticles < max {
				max = j.maxArticles
			}
			col, err := m.client.FetchCollection(ctx, u, max)
			cancel()
			if err != nil {
				v := &ArticleView{Index: len(targets), URL: u, FinalURL: u, Title: "合集解析失败", Error: err.Error()}
				j.addArticle(v, nil)
				j.logf("✗ 合集解析失败：%v", err)
				continue
			}
			j.logf("合集《%s》· %s · 共 %d 篇", col.Title, col.Account, len(col.Articles))
			for _, w := range col.Warnings {
				j.logf("提示：%s", w)
			}
			for _, ca := range col.Articles {
				targets = append(targets, target{url: ca.URL, coll: col, article: ca})
			}
			continue
		}
		targets = append(targets, target{url: u})
	}

	total := 0
	for ti, t := range targets {
		if j.ctx.Err() != nil {
			break
		}
		tpl := j.nameTemplate
		label := t.url
		if t.coll != nil {
			tpl = j.collectionTemplate
			label = fmt.Sprintf("#%d %s", t.article.Order, t.article.Title)
		}
		j.logf("解析文章 %d/%d · %s", ti+1, len(targets), label)

		ctx, cancel := context.WithTimeout(j.ctx, 90*time.Second)
		page, final, err := m.client.FetchPage(ctx, t.url)
		cancel()
		if err != nil {
			v := &ArticleView{Index: ti, URL: t.url, FinalURL: t.url, Title: "解析失败", Error: err.Error()}
			if t.article != nil {
				v.Title = t.article.Title
			}
			j.addArticle(v, nil)
			j.logf("✗ 解析失败：%v", err)
			m.remember(j.snapshot())
			continue
		}
		art := wx.ParseHTML(page, final, opts)
		if t.coll != nil {
			for _, md := range art.Medias {
				md.CollectionID = t.coll.ID
				md.CollectionTitle = t.coll.Title
				md.CollectionOrder = t.article.Order
			}
			art.CollectionID = t.coll.ID
			art.CollectionTitle = t.coll.Title
			art.CollectionOrder = t.article.Order
		}
		v := buildArticleView(art, tpl, ti)
		j.addArticle(v, art.Medias)
		m.remember(j.snapshot())
		for _, w := range art.Warnings {
			j.logf("提示：%s", w)
		}
		j.logf("✓ 《%s》解析到 %d 项（图片 %d，动图 %d）", v.Title, len(v.Medias), v.ImageCount, v.GIFCount)

		j.mu.Lock()
		total += len(art.Medias)
		j.total = total
		j.mu.Unlock()
		m.remember(j.snapshot())
		if len(art.Medias) == 0 {
			continue
		}
		articleIndex := ti
		d := &wx.Downloader{
			Client:       m.client,
			OutDir:       j.outDir,
			Jobs:         j.jobs,
			Force:        j.force,
			NameTemplate: tpl,
			OnEvent: func(ev wx.Event) {
				if ev.Result != nil {
					j.addResult(ev.Result, articleIndex)
					m.remember(j.snapshot())
				}
			},
		}
		d.Run(j.ctx, art.Medias)
	}

	if total == 0 {
		j.mu.Lock()
		for _, pa := range j.articles {
			if pa.View.Error != "" {
				if j.errText != "" {
					j.errText += "；"
				}
				j.errText += pa.View.Error
			}
		}
		if j.errText == "" {
			j.errText = "没有解析到可下载的图片"
		}
		j.mu.Unlock()
		j.logf("✗ 没有可下载的内容")
	}
	j.finish()
	m.remember(j.snapshot())
}

// Get returns a snapshot by id.
func (m *Manager) Get(id string) (*JobView, bool) {
	m.mu.Lock()
	j := m.jobs[id]
	if j == nil {
		if v := m.history[id]; v != nil {
			m.mu.Unlock()
			return v, true
		}
	}
	m.mu.Unlock()
	if j == nil {
		return nil, false
	}
	return j.snapshot(), true
}

// Cancel stops a running job.
func (m *Manager) Cancel(id string) (*JobView, bool) {
	m.mu.Lock()
	j := m.jobs[id]
	m.mu.Unlock()
	if j == nil {
		return nil, false
	}
	j.cancel()
	return j.snapshot(), true
}

func (m *Manager) pruneLocked() {
	const keep = 20
	for len(m.order) > keep {
		oldest := m.order[0]
		if oldest == m.activeID {
			break
		}
		j := m.jobs[oldest]
		if j != nil {
			j.mu.Lock()
			finished := j.status != "running"
			j.mu.Unlock()
			if !finished {
				break
			}
		}
		delete(m.jobs, oldest)
		m.order = m.order[1:]
	}
}

// ---------- rendering helpers ----------

func buildArticleView(a *wx.Article, tpl string, index int) *ArticleView {
	v := &ArticleView{
		Index:           index,
		URL:             a.URL,
		FinalURL:        a.FinalURL,
		Title:           a.Title,
		Account:         a.Account,
		Author:          a.Author,
		PostID:          a.PostID,
		PublishTime:     a.PublishTime,
		Warnings:        a.Warnings,
		ContentCount:    a.ContentCount,
		PictureCount:    a.PictureCount,
		CollectionID:    a.CollectionID,
		CollectionTitle: a.CollectionTitle,
		CollectionOrder: a.CollectionOrder,
	}
	if a.PublishTime > 0 {
		v.PublishText = time.Unix(a.PublishTime, 0).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04")
	}
	for i, m := range a.Medias {
		if m.Kind == "video" {
			v.VideoCount++
		} else if m.IsGIF || m.Kind == "gif" {
			v.GIFCount++
		} else {
			v.ImageCount++
		}
		rel, err := wx.BuildRelPath(m, tpl)
		if err != nil {
			rel = ""
		}
		mv := &MediaView{
			Index:           i,
			Seq:             m.Seq,
			Kind:            m.Kind,
			URL:             m.URL,
			PreviewURL:      wx.ResizeURL(m.URL, "300"),
			Ext:             m.Ext,
			Width:           m.Width,
			Height:          m.Height,
			IsGIF:           m.IsGIF,
			RelPath:         rel,
			Name:            filepath.Base(rel),
			Source:          m.Source,
			FileID:          m.FileID,
			Title:           m.Title,
			PostID:          m.PostID,
			Account:         m.Account,
			Author:          m.Author,
			Biz:             m.Biz,
			SN:              m.SN,
			Mid:             m.Mid,
			Idx:             m.Idx,
			PublishTime:     m.PublishTime,
			ArticleURL:      m.ArticleURL,
			CollectionID:    m.CollectionID,
			CollectionTitle: m.CollectionTitle,
			CollectionOrder: m.CollectionOrder,
		}
		v.Medias = append(v.Medias, mv)
	}
	return v
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func buildCollectionView(col *wx.Collection, index int) *CollectionView {
	v := &CollectionView{
		Index: index, URL: col.URL, FinalURL: col.FinalURL, ID: col.ID,
		Title: col.Title, Account: col.Account, Description: col.Description,
		ArticleCount: col.ArticleCount, Warnings: col.Warnings,
	}
	for i, a := range col.Articles {
		v.Articles = append(v.Articles, &CollectionArticleView{
			Index: i, Title: a.Title, URL: a.URL, Order: a.Order,
			MsgID: a.MsgID, ItemIdx: a.ItemIdx, PublishTime: a.PublishTime, Cover: a.Cover,
		})
	}
	return v
}
