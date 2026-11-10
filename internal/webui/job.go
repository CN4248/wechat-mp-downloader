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
	OutDir          string       `json:"out_dir,omitempty"`
	NameTemplate    string       `json:"name_template,omitempty"`
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

// JobTask is one input link plus its (optional) per-task overrides.
//
// A link parsed in the preview list can be started with its own download
// directory and its own naming rule; anything left empty falls back to the
// job-level default. Collection* is filled for tasks produced by a
// subscription scan so album variables keep working without re-expanding.
type JobTask struct {
	URL                string `json:"url"`
	OutDir             string `json:"out_dir,omitempty"`
	NameTemplate       string `json:"name_template,omitempty"`
	CollectionTemplate string `json:"collection_template,omitempty"`
	CollectionID       string `json:"collection_id,omitempty"`
	CollectionTitle    string `json:"collection_title,omitempty"`
	CollectionOrder    int    `json:"collection_order,omitempty"`
}

// JobView is the polled job snapshot.
type JobView struct {
	ID                 string           `json:"id"`
	Status             string           `json:"status"`
	URLs               []string         `json:"urls"`
	Tasks              []JobTask        `json:"tasks,omitempty"`
	OutDir             string           `json:"out_dir"`
	NameTemplate       string           `json:"name_template"`
	CollectionTemplate string           `json:"collection_template"`
	Jobs               int              `json:"jobs"`
	Force              bool             `json:"force"`
	Source             string           `json:"source,omitempty"`
	SourceName         string           `json:"source_name,omitempty"`
	SubID              string           `json:"sub_id,omitempty"`
	Total              int              `json:"total"`
	Done               int              `json:"done"`
	Success            int              `json:"success"`
	Skipped            int              `json:"skipped"`
	Failed             int              `json:"failed"`
	Articles           []*ArticleView   `json:"articles"`
	Results            []*FileView      `json:"results"`
	Logs               []LogLine        `json:"logs"`
	Error              string           `json:"error"`
	StartedAt          int64            `json:"started_at"`
	FinishedAt         int64            `json:"finished_at"`
	Request            *DownloadRequest `json:"request,omitempty"`
}

// DownloadRequest starts a download job.
type DownloadRequest struct {
	URL                string    `json:"url"`
	URLs               []string  `json:"urls"`
	Tasks              []JobTask `json:"tasks"`
	OutDir             string    `json:"out_dir"`
	Jobs               int       `json:"jobs"`
	Force              bool      `json:"force"`
	Cover              bool      `json:"cover"`
	Video              bool      `json:"video"`
	NameTemplate       string    `json:"name_template"`
	CollectionTemplate string    `json:"collection_template"`
	MaxArticles        int       `json:"max_articles"`
	Source             string    `json:"source,omitempty"`
	SourceName         string    `json:"source_name,omitempty"`
	SubID              string    `json:"sub_id,omitempty"`
	// Pick is set by the userscript's "push picked media" action. When present
	// the job downloads exactly those media, using the article parse only for
	// naming context and per-item seq/ext.
	Pick *MediaPick `json:"pick,omitempty"`
}

// PreviewRequest renders names for a template.
type PreviewRequest struct {
	Template string      `json:"template"`
	Medias   []*wx.Media `json:"medias"`
}

type parsedArticle struct {
	View   *ArticleView
	Medias []*wx.Media
	Task   JobTask
}

type job struct {
	id string

	tasks       []JobTask
	jobs        int
	force       bool
	cover       bool
	video       bool
	maxArticles int

	source     string
	sourceName string
	subID      string
	request    DownloadRequest

	ctx    context.Context
	cancel context.CancelFunc

	mu          sync.Mutex
	status      string
	pauseWanted bool
	total       int
	done        int
	success     int
	skipped     int
	failed      int
	articles    []*parsedArticle
	results     []*FileView
	logs        []LogLine
	errText     string
	startedAt   time.Time
	finishedAt  time.Time

	// onFile receives every finished item (used by the subscription scanner to
	// remember which file belongs to which subscription so it can be renamed).
	onFile func(task JobTask, media *wx.Media, r *wx.Result)
}

const maxLogs = 500

func newJob(req DownloadRequest, tasks []JobTask) *job {
	ctx, cancel := context.WithCancel(context.Background())
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return &job{
		id:          time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b),
		tasks:       tasks,
		jobs:        req.Jobs,
		force:       req.Force,
		cover:       req.Cover,
		video:       req.Video,
		maxArticles: req.MaxArticles,
		source:      req.Source,
		sourceName:  req.SourceName,
		subID:       req.SubID,
		request:     req,
		ctx:         ctx,
		cancel:      cancel,
		status:      "queued",
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

func (j *job) addArticle(v *ArticleView, medias []*wx.Media, task JobTask) {
	j.mu.Lock()
	j.articles = append(j.articles, &parsedArticle{View: v, Medias: medias, Task: task})
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

// paused reports whether a pause was requested. It is checked between targets.
func (j *job) paused() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.pauseWanted || j.ctx.Err() != nil
}

func (j *job) finish() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.finishedAt = time.Now()
	switch {
	case j.pauseWanted:
		j.status = "paused"
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
		OutDir:             taskOutDir(j.tasks),
		NameTemplate:       taskTemplate(j.tasks),
		CollectionTemplate: j.request.CollectionTemplate,
		Jobs:               j.jobs,
		Force:              j.force,
		Source:             j.source,
		SourceName:         j.sourceName,
		SubID:              j.subID,
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
	for _, t := range j.tasks {
		v.Tasks = append(v.Tasks, t)
		v.URLs = append(v.URLs, t.URL)
	}
	for _, pa := range j.articles {
		v.Articles = append(v.Articles, pa.View)
	}
	v.Results = append(v.Results, j.results...)
	v.Logs = append(v.Logs, j.logs...)
	req := j.request
	v.Request = &req
	return v
}

func taskOutDir(tasks []JobTask) string {
	if len(tasks) == 0 {
		return ""
	}
	return tasks[0].OutDir
}

func taskTemplate(tasks []JobTask) string {
	if len(tasks) == 0 {
		return ""
	}
	if tasks[0].NameTemplate != "" {
		return tasks[0].NameTemplate
	}
	return tasks[0].CollectionTemplate
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
	Source     string   `json:"source,omitempty"`
	SourceName string   `json:"source_name,omitempty"`
	StartedAt  int64    `json:"started_at"`
	FinishedAt int64    `json:"finished_at"`
}

// Manager owns the download queue and keeps every job snapshot on disk.
type Manager struct {
	client            *wx.Client
	defaultOut        string
	defaultName       string
	defaultCollection string
	dataDir           string
	subs              *Subscriber

	mu        sync.Mutex
	jobs      map[string]*job
	order     []string
	history   map[string]*JobView
	histOrder []string
	pausedAll bool
	wakeCh    chan struct{}
	stopCh    chan struct{}
	closed    bool
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
		wakeCh:            make(chan struct{}, 1),
		stopCh:            make(chan struct{}),
	}
	m.loadHistory()
	go m.loop()
	return m
}

// SetSubscriber wires the subscription scanner in (used to record downloaded
// files so a later rule change can rename them).
func (m *Manager) SetSubscriber(s *Subscriber) { m.subs = s }

// Close stops the queue worker.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.mu.Unlock()
	close(m.stopCh)
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
		switch v.Status {
		case "running", "queued", "paused":
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
			Source: v.Source, SourceName: v.SourceName,
		})
	}
	paused := m.pausedAll
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	_ = paused
	return out
}

// QueueState reports whether the whole queue is paused.
func (m *Manager) QueueState() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	running := ""
	queued, paused := 0, 0
	for _, id := range m.order {
		j := m.jobs[id]
		if j == nil {
			continue
		}
		j.mu.Lock()
		st := j.status
		j.mu.Unlock()
		switch st {
		case "running":
			running = id
		case "queued":
			queued++
		case "paused":
			paused++
		}
	}
	return map[string]any{
		"paused_all": m.pausedAll,
		"running":    running,
		"queued":     queued,
		"paused":     paused,
	}
}

// normalizeTasks validates every link and fills the per-task defaults.
func (m *Manager) normalizeTasks(req DownloadRequest) ([]JobTask, string, error) {
	raw := make([]JobTask, 0, len(req.Tasks)+len(req.URLs)+1)
	if strings.TrimSpace(req.URL) != "" {
		raw = append(raw, JobTask{URL: req.URL})
	}
	for _, u := range req.URLs {
		raw = append(raw, JobTask{URL: u})
	}
	raw = append(raw, req.Tasks...)

	defTpl := strings.TrimSpace(req.NameTemplate)
	if defTpl == "" {
		defTpl = m.defaultName
	}
	if err := wx.ValidateTemplate(defTpl); err != nil {
		return nil, "", err
	}
	defCtpl := strings.TrimSpace(req.CollectionTemplate)
	if defCtpl == "" {
		defCtpl = m.defaultCollection
	}
	if err := wx.ValidateTemplate(defCtpl); err != nil {
		return nil, "", fmt.Errorf("合集命名规则：%w", err)
	}
	abs, err := m.defaultOutDir(req.OutDir)
	if err != nil {
		return nil, "", err
	}

	out := make([]JobTask, 0, len(raw))
	seen := map[string]bool{}
	for _, t := range raw {
		for _, part := range strings.FieldsFunc(t.URL, func(r rune) bool { return r == '\n' || r == '\r' }) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			u, err := wx.NormalizeInputURL(part)
			if err != nil {
				return nil, "", err
			}
			if seen[u] {
				continue
			}
			seen[u] = true
			nt := t
			nt.URL = u
			nt.OutDir = expandHome(strings.TrimSpace(t.OutDir))
			if nt.OutDir == "" {
				nt.OutDir = abs
			} else if nt.OutDir, err = filepath.Abs(nt.OutDir); err != nil {
				return nil, "", fmt.Errorf("任务下载目录无效：%v", err)
			}
			nt.NameTemplate = strings.TrimSpace(t.NameTemplate)
			if nt.NameTemplate == "" {
				nt.NameTemplate = defTpl
			}
			if err := wx.ValidateTemplate(nt.NameTemplate); err != nil {
				return nil, "", fmt.Errorf("任务命名规则：%w", err)
			}
			nt.CollectionTemplate = strings.TrimSpace(t.CollectionTemplate)
			if nt.CollectionTemplate == "" {
				nt.CollectionTemplate = defCtpl
			}
			if err := wx.ValidateTemplate(nt.CollectionTemplate); err != nil {
				return nil, "", fmt.Errorf("任务合集命名规则：%w", err)
			}
			out = append(out, nt)
		}
	}
	if len(out) == 0 {
		return nil, "", fmt.Errorf("请先填写文章链接")
	}
	if len(out) > 30 {
		return nil, "", fmt.Errorf("一次最多 30 个任务")
	}
	return out, abs, nil
}

// defaultOutDir resolves the download root for a request (or the manager
// default), expanding ~ and rejecting paths that are not folders.
func (m *Manager) defaultOutDir(out string) (string, error) {
	out = strings.TrimSpace(out)
	if out == "" {
		out = m.defaultOut
	}
	abs, err := filepath.Abs(expandHome(out))
	if err != nil {
		return "", fmt.Errorf("下载目录无效：%v", err)
	}
	if st, statErr := os.Stat(abs); statErr == nil && !st.IsDir() {
		return "", fmt.Errorf("下载目录不是文件夹：%s", abs)
	}
	return abs, nil
}

// normalizeRequest dispatches between article-link jobs and the userscript's
// "push picked media" jobs.
func (m *Manager) normalizeRequest(req DownloadRequest) ([]JobTask, string, error) {
	if req.Pick != nil {
		return m.normalizePickTasks(req)
	}
	return m.normalizeTasks(req)
}

// Start validates the request and queues a job.
func (m *Manager) Start(req DownloadRequest) (*JobView, error) {
	tasks, abs, err := m.normalizeRequest(req)
	if err != nil {
		return nil, err
	}
	req.Tasks = tasks
	req.OutDir = abs
	return m.enqueue(req, nil), nil
}

// enqueue creates and remembers a job, then wakes the queue worker.
func (m *Manager) enqueue(req DownloadRequest, onFile func(JobTask, *wx.Media, *wx.Result)) *JobView {
	j := newJob(req, req.Tasks)
	if onFile != nil {
		j.onFile = onFile
	}
	m.mu.Lock()
	m.jobs[j.id] = j
	m.order = append(m.order, j.id)
	if m.pausedAll {
		j.mu.Lock()
		j.status = "paused"
		j.mu.Unlock()
	}
	m.pruneLocked()
	m.mu.Unlock()

	view := j.snapshot()
	m.remember(view)
	m.kick()
	return view
}

// StartSubJob queues the downloads produced by a subscription scan.
func (m *Manager) StartSubJob(req DownloadRequest, onFile func(JobTask, *wx.Media, *wx.Result)) (*JobView, error) {
	tasks, abs, err := m.normalizeRequest(req)
	if err != nil {
		return nil, err
	}
	req.Tasks = tasks
	req.OutDir = abs
	return m.enqueue(req, onFile), nil
}

func (m *Manager) kick() {
	select {
	case m.wakeCh <- struct{}{}:
	default:
	}
}

func (m *Manager) loop() {
	for {
		select {
		case <-m.stopCh:
			return
		case <-m.wakeCh:
		}
		for {
			j := m.nextQueued()
			if j == nil {
				break
			}
			m.runJob(j)
		}
	}
}

func (m *Manager) nextQueued() *job {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range m.order {
		j := m.jobs[id]
		if j == nil {
			continue
		}
		j.mu.Lock()
		st := j.status
		j.mu.Unlock()
		if st == "queued" {
			return j
		}
	}
	return nil
}

func (m *Manager) runJob(j *job) {
	j.mu.Lock()
	if j.status != "queued" {
		j.mu.Unlock()
		return
	}
	j.status = "running"
	j.startedAt = time.Now()
	j.mu.Unlock()
	m.remember(j.snapshot())

	m.run(j)
	j.finish()
	view := j.snapshot()
	m.remember(view)

	// A paused job stays in the queue until the user resumes it.
	if j.ctx.Err() != nil && j.status == "canceled" {
		j.logf("任务已取消")
	}
}

type jobTarget struct {
	task JobTask
	url  string
	coll *wx.Collection
	ca   *wx.CollectionArticle
	ti   int // task index (for article index in the view)
}

// buildTargets expands album tasks into per-article targets.
func (m *Manager) buildTargets(j *job) []jobTarget {
	targets := make([]jobTarget, 0, len(j.tasks))
	for ti, t := range j.tasks {
		if j.paused() {
			break
		}
		sub := t.CollectionID != "" || t.CollectionTitle != ""
		if sub {
			targets = append(targets, jobTarget{
				task: t, url: t.URL, ti: ti,
				coll: &wx.Collection{ID: t.CollectionID, Title: t.CollectionTitle},
				ca:   &wx.CollectionArticle{Order: t.CollectionOrder},
			})
			continue
		}
		if wx.IsAlbumURL(t.URL) {
			j.logf("解析合集 %d/%d · %s", ti+1, len(j.tasks), t.URL)
			ctx, cancel := context.WithTimeout(j.ctx, 180*time.Second)
			max := wx.MaxCollectionArticles // 0 = 整册
			if j.maxArticles > 0 {
				max = j.maxArticles
			}
			col, err := m.client.FetchCollection(ctx, t.URL, max)
			cancel()
			if err != nil {
				v := &ArticleView{Index: len(targets), URL: t.URL, FinalURL: t.URL, Title: "合集解析失败", Error: err.Error()}
				j.addArticle(v, nil, t)
				j.logf("✗ 合集解析失败：%v", err)
				continue
			}
			j.logf("合集《%s》· %s · 共 %d 篇", col.Title, col.Account, len(col.Articles))
			for _, w := range col.Warnings {
				j.logf("提示：%s", w)
			}
			for _, ca := range col.Articles {
				targets = append(targets, jobTarget{task: t, url: ca.URL, coll: col, ca: ca, ti: ti})
			}
			continue
		}
		targets = append(targets, jobTarget{task: t, url: t.URL, ti: ti})
	}
	return targets
}

func (m *Manager) run(j *job) {
	if j.request.Pick != nil {
		m.runPick(j)
		return
	}
	opts := wx.ParseOptions{IncludeCover: j.cover, IncludeVideo: j.video}
	targets := m.buildTargets(j)

	total := 0
	for ti, t := range targets {
		if j.paused() {
			break
		}
		tpl := t.task.NameTemplate
		label := t.url
		if t.coll != nil {
			tpl = t.task.CollectionTemplate
			if t.ca != nil && t.ca.Order > 0 {
				label = fmt.Sprintf("#%d %s", t.ca.Order, t.ca.Title)
			}
		}
		j.logf("解析文章 %d/%d · %s", ti+1, len(targets), label)

		ctx, cancel := context.WithTimeout(j.ctx, 90*time.Second)
		page, final, err := m.client.FetchPage(ctx, t.url)
		cancel()
		if err != nil {
			v := &ArticleView{Index: ti, URL: t.url, FinalURL: t.url, Title: "解析失败", Error: err.Error(), OutDir: t.task.OutDir, NameTemplate: tpl}
			if t.ca != nil && t.ca.Title != "" {
				v.Title = t.ca.Title
			}
			j.addArticle(v, nil, t.task)
			j.logf("✗ 解析失败：%v", err)
			m.remember(j.snapshot())
			continue
		}
		art := wx.ParseHTML(page, final, opts)
		if m.subs != nil && art.Biz != "" {
			// 每次下载都顺手把公众号记进本机库，「搜索公众号作者」才能覆盖到。
			m.subs.RememberAccount(wx.AccountOf(page, final, art))
		}
		if t.coll != nil {
			order := 0
			if t.ca != nil {
				order = t.ca.Order
			}
			for _, md := range art.Medias {
				md.CollectionID = t.coll.ID
				md.CollectionTitle = t.coll.Title
				md.CollectionOrder = order
			}
			art.CollectionID = t.coll.ID
			art.CollectionTitle = t.coll.Title
			art.CollectionOrder = order
		}
		v := buildArticleView(art, tpl, ti)
		v.OutDir = t.task.OutDir
		v.NameTemplate = tpl
		j.addArticle(v, art.Medias, t.task)
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
		if j.paused() {
			break
		}
		articleIndex := ti
		task := t.task
		media := art.Medias
		d := &wx.Downloader{
			Client:       m.client,
			OutDir:       task.OutDir,
			Jobs:         j.jobs,
			Force:        j.force,
			NameTemplate: tpl,
			OnEvent: func(ev wx.Event) {
				if ev.Result != nil {
					j.addResult(ev.Result, articleIndex)
					if j.onFile != nil && ev.Result.Status == "ok" {
						j.onFile(task, ev.Result.Media, ev.Result)
					}
					m.remember(j.snapshot())
				}
			},
		}
		d.Run(j.ctx, media)
	}

	if total == 0 && j.ctx.Err() == nil {
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

func (m *Manager) jobByID(id string) *job {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jobs[id]
}

// Pause stops a job: a queued job is simply held back, a running job finishes
// the current article and then waits in the paused state.
func (m *Manager) Pause(id string) (*JobView, bool) {
	j := m.jobByID(id)
	if j == nil {
		return nil, false
	}
	j.mu.Lock()
	switch j.status {
	case "running":
		j.pauseWanted = true
	case "queued", "paused":
		j.pauseWanted = true
		j.status = "paused"
	}
	j.mu.Unlock()
	view := j.snapshot()
	m.remember(view)
	return view, true
}

// Resume puts a paused job back into the queue. Downloaded files are skipped,
// so resuming never downloads the same picture twice.
func (m *Manager) Resume(id string) (*JobView, bool) {
	j := m.jobByID(id)
	if j == nil {
		return nil, false
	}
	j.mu.Lock()
	if j.status == "paused" {
		j.pauseWanted = false
		j.status = "queued"
	}
	j.mu.Unlock()
	view := j.snapshot()
	m.remember(view)
	m.kick()
	return view, true
}

// Cancel stops a job for good.
func (m *Manager) Cancel(id string) (*JobView, bool) {
	j := m.jobByID(id)
	if j == nil {
		return nil, false
	}
	j.mu.Lock()
	if j.status == "queued" || j.status == "paused" {
		j.status = "canceled"
		j.finishedAt = time.Now()
	}
	j.mu.Unlock()
	j.cancel()
	view := j.snapshot()
	m.remember(view)
	return view, true
}

// Retry queues a fresh copy of a finished job. Existing files are skipped.
func (m *Manager) Retry(id string) (*JobView, error) {
	m.mu.Lock()
	var src *JobView
	if j := m.jobs[id]; j != nil {
		src = j.snapshot()
	} else {
		src = m.history[id]
	}
	m.mu.Unlock()
	if src == nil {
		return nil, fmt.Errorf("任务不存在")
	}
	req := DownloadRequest{Tasks: src.Tasks, URLs: src.URLs, OutDir: src.OutDir, Jobs: src.Jobs, Force: src.Force}
	if src.Request != nil {
		req = *src.Request
		req.Force = src.Force
		if len(req.Tasks) == 0 && len(req.URLs) == 0 {
			req.URLs = src.URLs
		}
	}
	req.Source = src.Source
	req.SourceName = src.SourceName
	req.SubID = src.SubID
	tasks, abs, err := m.normalizeRequest(req)
	if err != nil {
		return nil, err
	}
	req.Tasks = tasks
	req.OutDir = abs
	var onFile func(JobTask, *wx.Media, *wx.Result)
	if req.SubID != "" && m.subs != nil {
		subID := req.SubID
		onFile = func(task JobTask, media *wx.Media, r *wx.Result) {
			m.subs.recordFile(subID, task, media, r)
		}
	}
	return m.enqueue(req, onFile), nil
}

// Delete removes job records (and stops them when still queued/running).
func (m *Manager) Delete(ids []string) int {
	if len(ids) == 0 {
		return 0
	}
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	m.mu.Lock()
	removed := 0
	for id := range set {
		if j := m.jobs[id]; j != nil {
			j.cancel()
			delete(m.jobs, id)
		}
		if _, ok := m.history[id]; ok {
			delete(m.history, id)
			removed++
		}
	}
	order := m.order[:0]
	for _, id := range m.order {
		if !set[id] {
			order = append(order, id)
		}
	}
	m.order = order
	hist := m.histOrder[:0]
	for _, id := range m.histOrder {
		if !set[id] {
			hist = append(hist, id)
		}
	}
	m.histOrder = hist
	m.mu.Unlock()
	m.saveHistory()
	return removed
}

// PauseAll pauses the whole queue and asks the running job to stop.
func (m *Manager) PauseAll() map[string]any {
	m.mu.Lock()
	m.pausedAll = true
	ids := append([]string(nil), m.order...)
	m.mu.Unlock()
	for _, id := range ids {
		if j := m.jobByID(id); j != nil {
			j.mu.Lock()
			if j.status == "queued" {
				j.pauseWanted = true
				j.status = "paused"
			} else if j.status == "running" {
				j.pauseWanted = true
			}
			j.mu.Unlock()
		}
	}
	m.snapshotRunning()
	return m.QueueState()
}

// ResumeAll resumes the queue and every paused job.
func (m *Manager) ResumeAll() map[string]any {
	m.mu.Lock()
	m.pausedAll = false
	ids := append([]string(nil), m.order...)
	m.mu.Unlock()
	for _, id := range ids {
		if j := m.jobByID(id); j != nil {
			j.mu.Lock()
			if j.status == "paused" {
				j.pauseWanted = false
				j.status = "queued"
			}
			j.mu.Unlock()
		}
	}
	m.snapshotRunning()
	m.kick()
	return m.QueueState()
}

// RetryFailed re-queues every failed job.
func (m *Manager) RetryFailed() int {
	m.mu.Lock()
	ids := make([]string, 0, len(m.history))
	for _, id := range m.histOrder {
		if v := m.history[id]; v != nil && v.Status == "failed" {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	n := 0
	for _, id := range ids {
		if _, err := m.Retry(id); err == nil {
			n++
		}
	}
	return n
}

// ClearCompleted deletes finished job records (files are kept on disk).
// Failed jobs are kept so "重试所有失败任务" still has something to work on.
func (m *Manager) ClearCompleted() int {
	m.mu.Lock()
	ids := make([]string, 0, len(m.history))
	for _, id := range m.histOrder {
		if v := m.history[id]; v != nil {
			switch v.Status {
			case "done", "canceled":
				ids = append(ids, id)
			}
		}
	}
	m.mu.Unlock()
	return m.Delete(ids)
}

// Batch applies one action to the selected job ids.
func (m *Manager) Batch(action string, ids []string) (map[string]any, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("没有选择任务")
	}
	count := 0
	switch action {
	case "pause":
		for _, id := range ids {
			if _, ok := m.Pause(id); ok {
				count++
			}
		}
	case "resume":
		for _, id := range ids {
			if _, ok := m.Resume(id); ok {
				count++
			}
		}
	case "cancel":
		for _, id := range ids {
			if _, ok := m.Cancel(id); ok {
				count++
			}
		}
	case "delete":
		count = m.Delete(ids)
	case "retry":
		for _, id := range ids {
			if _, err := m.Retry(id); err == nil {
				count++
			}
		}
	default:
		return nil, fmt.Errorf("未知的批量操作：%s", action)
	}
	return map[string]any{"action": action, "count": count}, nil
}

func (m *Manager) snapshotRunning() {
	m.mu.Lock()
	ids := append([]string(nil), m.order...)
	m.mu.Unlock()
	for _, id := range ids {
		if j := m.jobByID(id); j != nil {
			m.remember(j.snapshot())
		}
	}
}

func (m *Manager) pruneLocked() {
	const keep = 60
	for len(m.order) > keep {
		oldest := m.order[0]
		j := m.jobs[oldest]
		if j != nil {
			j.mu.Lock()
			finished := j.status != "running" && j.status != "queued" && j.status != "paused"
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
