package webui

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"wxmp/internal/wx"
)

//go:embed static
var staticFiles embed.FS

// Config configures the WebUI server.
type Config struct {
	DefaultOut     string
	Jobs           int
	Listen         string
	Version        string
	NoBrowser      bool
	NameTemplate   string
	CollectionName string
	DataDir        string
	// Cookie is optional (WXMP_COOKIE / -cookie). Kept in memory only.
	Cookie string
}

// Server serves the WebUI and JSON API.
type Server struct {
	cfg     Config
	client  *wx.Client
	mgr     *Manager
	subs    *Subscriber
	thumbs  *thumbCache
	roots   []string
	started time.Time
}

// Run starts the WebUI and blocks until ctx is cancelled.
func Run(parent context.Context, cfg Config) error {
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:2178"
	}
	if cfg.DefaultOut == "" {
		cfg.DefaultOut = "downloads"
	}
	if cfg.Jobs <= 0 {
		cfg.Jobs = 4
	}
	if cfg.Jobs > 16 {
		cfg.Jobs = 16
	}
	if strings.TrimSpace(cfg.NameTemplate) == "" {
		cfg.NameTemplate = wx.DefaultNameTemplate
	}
	if err := wx.ValidateTemplate(cfg.NameTemplate); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.CollectionName) == "" {
		cfg.CollectionName = wx.DefaultCollectionTemplate
	}
	if err := wx.ValidateTemplate(cfg.CollectionName); err != nil {
		return fmt.Errorf("合集命名规则：%w", err)
	}
	if strings.TrimSpace(cfg.DataDir) == "" {
		cfg.DataDir = "data"
	}
	if absData, err := filepath.Abs(expandHome(cfg.DataDir)); err == nil {
		cfg.DataDir = absData
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("创建数据目录失败：%w", err)
	}
	absOut, err := filepath.Abs(expandHome(cfg.DefaultOut))
	if err == nil {
		cfg.DefaultOut = absOut
	}

	client := wx.NewClient()
	client.SetCookie(cfg.Cookie)
	mgr := NewManager(client, cfg.DefaultOut, cfg.NameTemplate, cfg.CollectionName, cfg.DataDir)
	subs := NewSubscriber(client, mgr, cfg.DataDir, cfg.DefaultOut, cfg.NameTemplate, cfg.CollectionName)
	mgr.SetSubscriber(subs)
	subs.Start()
	s := &Server{
		cfg:     cfg,
		client:  client,
		mgr:     mgr,
		subs:    subs,
		thumbs:  newThumbCache(),
		roots:   downloadRoots(cfg.DefaultOut),
		started: time.Now(),
	}

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		mgr.Close()
		subs.Close()
		return fmt.Errorf("监听 %s 失败：%w", cfg.Listen, err)
	}
	url := "http://" + browserHost(ln.Addr().String())
	fmt.Printf("WebUI: %s\n", url)
	fmt.Printf("下载目录: %s\n", cfg.DefaultOut)
	fmt.Printf("默认命名: %s\n", cfg.NameTemplate)
	fmt.Printf("合集命名: %s\n", cfg.CollectionName)
	fmt.Printf("定时追更: %v（数据目录 %s）\n", true, cfg.DataDir)
	if !cfg.NoBrowser {
		go openBrowser(url)
	}

	ctx, stop := signal.NotifyContext(parent, os.Interrupt)
	defer stop()

	httpSrv := &http.Server{
		Handler:           s.routes(),
		ReadHeaderTimeout: 20 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.Serve(ln) }()

	select {
	case <-ctx.Done():
		subs.Close()
		mgr.Close()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
		return nil
	case serveErr := <-errCh:
		if serveErr == http.ErrServerClosed {
			return nil
		}
		return serveErr
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	staticSub, _ := fs.Sub(staticFiles, "static")

	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("POST /api/parse", s.handleParse)
	mux.HandleFunc("POST /api/preview", s.handlePreview)
	mux.HandleFunc("POST /api/download", s.handleDownload)
	mux.HandleFunc("POST /api/download/media", s.handleDownloadMedia)
	mux.HandleFunc("GET /api/jobs", s.handleJobs)
	mux.HandleFunc("GET /api/jobs/{id}", s.handleJob)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", s.handleCancel)
	mux.HandleFunc("POST /api/jobs/{id}/pause", s.handleJobPause)
	mux.HandleFunc("POST /api/jobs/{id}/resume", s.handleJobResume)
	mux.HandleFunc("POST /api/jobs/{id}/retry", s.handleJobRetry)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.handleJobDelete)
	mux.HandleFunc("POST /api/jobs/pause-all", s.handlePauseAll)
	mux.HandleFunc("POST /api/jobs/resume-all", s.handleResumeAll)
	mux.HandleFunc("POST /api/jobs/retry-failed", s.handleRetryFailed)
	mux.HandleFunc("POST /api/jobs/clear-completed", s.handleClearCompleted)
	mux.HandleFunc("POST /api/jobs/batch", s.handleJobsBatch)
	mux.HandleFunc("GET /api/subs", s.handleSubsList)
	mux.HandleFunc("POST /api/subs", s.handleSubUpsert)
	mux.HandleFunc("DELETE /api/subs/{id}", s.handleSubDelete)
	mux.HandleFunc("POST /api/subs/{id}/check", s.handleSubCheck)
	mux.HandleFunc("GET /api/subs/{id}/articles", s.handleSubArticles)
	mux.HandleFunc("POST /api/subs/{id}/backfill", s.handleSubBackfill)
	mux.HandleFunc("POST /api/subs/batch", s.handleSubsBatch)
	mux.HandleFunc("POST /api/subs/config", s.handleSubsConfig)
	mux.HandleFunc("POST /api/subs/search", s.handleSubsSearch)
	mux.HandleFunc("POST /api/settings/cookie", s.handleSetCookie)
	mux.HandleFunc("POST /api/open-dir", s.handleOpenDir)
	mux.HandleFunc("GET /api/thumb", s.handleThumb)
	mux.HandleFunc("GET /api/health", s.handleHealth)

	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; "+
				"script-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "界面文件缺失", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

type tokenView struct {
	Token   string `json:"token"`
	Alias   string `json:"alias"`
	Desc    string `json:"desc"`
	Example string `json:"example"`
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	tokens := make([]tokenView, 0, len(wx.NameTokens))
	for _, t := range wx.NameTokens {
		tokens = append(tokens, tokenView{Token: t.Token, Alias: t.Alias, Desc: t.Desc, Example: t.Example})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"app":                "wxmp",
		"version":            s.cfg.Version,
		"default_out":        s.cfg.DefaultOut,
		"default_name":       s.cfg.NameTemplate,
		"default_collection": s.cfg.CollectionName,
		"jobs":               s.cfg.Jobs,
		"max_urls":           30,
		"max_tasks":          30,
		"cookie":             cookieStatus(s.client),
		"download_roots":     s.roots,
		"tokens":             tokens,
		"sub_defaults": map[string]any{
			"album_template":   s.cfg.CollectionName,
			"account_template": s.cfg.NameTemplate,
			"interval":         defaultSubInterval,
			"max_new":          defaultSubMaxNew,
		},
		"uptime": int64(time.Since(s.started).Seconds()),
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"app":        "wxmp",
		"version":    s.cfg.Version,
		"pick_media": true,
	})
}

type parseRequest struct {
	URL                string   `json:"url"`
	URLs               []string `json:"urls"`
	NameTemplate       string   `json:"name_template"`
	CollectionTemplate string   `json:"collection_template"`
	Cover              bool     `json:"cover"`
	Video              bool     `json:"video"`
}

func (s *Server) handleParse(w http.ResponseWriter, r *http.Request) {
	var req parseRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	urls, err := cleanURLs(req.URL, req.URLs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tpl := strings.TrimSpace(req.NameTemplate)
	if tpl == "" {
		tpl = s.cfg.NameTemplate
	}
	if err := wx.ValidateTemplate(tpl); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctpl := strings.TrimSpace(req.CollectionTemplate)
	if ctpl == "" {
		ctpl = s.cfg.CollectionName
	}
	if err := wx.ValidateTemplate(ctpl); err != nil {
		writeError(w, http.StatusBadRequest, "合集命名规则："+err.Error())
		return
	}

	views := make([]*ArticleView, 0, len(urls))
	collections := make([]*CollectionView, 0, len(urls))
	for i, u := range urls {
		if wx.IsAlbumURL(u) {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
			col, fetchErr := s.client.FetchCollection(ctx, u, wx.MaxCollectionArticles)
			cancel()
			cv := &CollectionView{Index: i, URL: u, FinalURL: u, Title: "合集解析失败"}
			if fetchErr != nil {
				cv.Error = fetchErr.Error()
			} else {
				cv = buildCollectionView(col, i)
				if len(cv.Articles) == 0 {
					cv.Error = "没有解析到合集文章（可能已删除或需要验证）"
				}
			}
			collections = append(collections, cv)
			continue
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		page, final, fetchErr := s.client.FetchPage(ctx, u)
		cancel()
		if fetchErr != nil {
			views = append(views, &ArticleView{Index: i, URL: u, FinalURL: u, Title: "解析失败", Error: fetchErr.Error()})
			continue
		}
		art := wx.ParseHTML(page, final, wx.ParseOptions{IncludeCover: req.Cover, IncludeVideo: req.Video})
		if art.Biz != "" {
			s.subs.RememberAccount(wx.AccountOf(page, final, art))
		}
		view := buildArticleView(art, tpl, i)
		if len(view.Medias) == 0 {
			view.Error = "没有解析到图片（可能是纯文字、已删除或需要验证）"
		}
		views = append(views, view)
	}
	writeJSON(w, http.StatusOK, map[string]any{"articles": views, "collections": collections})
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	var req PreviewRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tpl := strings.TrimSpace(req.Template)
	if tpl == "" {
		tpl = s.cfg.NameTemplate
	}
	if err := wx.ValidateTemplate(tpl); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Medias) > 1000 {
		writeError(w, http.StatusBadRequest, "预览数量过多")
		return
	}
	names := make([]string, len(req.Medias))
	errs := make([]string, len(req.Medias))
	for i, m := range req.Medias {
		if m == nil {
			continue
		}
		rel, err := wx.BuildRelPath(m, tpl)
		if err != nil {
			errs[i] = err.Error()
			continue
		}
		names[i] = rel
	}
	writeJSON(w, http.StatusOK, map[string]any{"names": names, "errors": errs})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	var req DownloadRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	view, err := s.mgr.Start(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": view})
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"jobs": s.mgr.List(), "queue": s.mgr.QueueState()})
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	view, ok := s.mgr.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	view, ok := s.mgr.Cancel(id)
	if !ok {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleJobPause(w http.ResponseWriter, r *http.Request) {
	if view, ok := s.mgr.Pause(r.PathValue("id")); ok {
		writeJSON(w, http.StatusOK, view)
		return
	}
	writeError(w, http.StatusNotFound, "任务不存在")
}

func (s *Server) handleJobResume(w http.ResponseWriter, r *http.Request) {
	if view, ok := s.mgr.Resume(r.PathValue("id")); ok {
		writeJSON(w, http.StatusOK, view)
		return
	}
	writeError(w, http.StatusNotFound, "任务不存在")
}

func (s *Server) handleJobRetry(w http.ResponseWriter, r *http.Request) {
	view, err := s.mgr.Retry(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": view})
}

func (s *Server) handleJobDelete(w http.ResponseWriter, r *http.Request) {
	n := s.mgr.Delete([]string{r.PathValue("id")})
	if n == 0 {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n, "queue": s.mgr.QueueState()})
}

func (s *Server) handlePauseAll(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"queue": s.mgr.PauseAll()})
}

func (s *Server) handleResumeAll(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"queue": s.mgr.ResumeAll()})
}

func (s *Server) handleRetryFailed(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"count": s.mgr.RetryFailed()})
}

func (s *Server) handleClearCompleted(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"deleted": s.mgr.ClearCompleted()})
}

func (s *Server) handleJobsBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string   `json:"action"`
		IDs    []string `json:"ids"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.mgr.Batch(req.Action, req.IDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res["queue"] = s.mgr.QueueState()
	writeJSON(w, http.StatusOK, res)
}

// ---------- settings ----------

func cookieStatus(c *wx.Client) map[string]any {
	set, uin, key := c.CookieStatus()
	return map[string]any{"set": set, "uin": uin, "key": key}
}

// handleSetCookie stores the WeChat cookie in memory only (never logged or
// written to disk). The response only reports whether uin/key were found.
func (s *Server) handleSetCookie(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Cookie string `json:"cookie"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.client.SetCookie(req.Cookie)
	writeJSON(w, http.StatusOK, cookieStatus(s.client))
}

// ---------- subscriptions ----------

func (s *Server) handleSubsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.subs.Snapshot())
}

func (s *Server) handleSubUpsert(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if err := readJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	view, extra, err := s.subs.Upsert(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	out := map[string]any{"sub": view}
	if extra != nil {
		out["renamed"] = extra["renamed"]
		out["missing"] = extra["missing"]
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSubDelete(w http.ResponseWriter, r *http.Request) {
	n := s.subs.Delete([]string{r.PathValue("id")})
	if n == 0 {
		writeError(w, http.StatusNotFound, "追更任务不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}

func (s *Server) handleSubCheck(w http.ResponseWriter, r *http.Request) {
	res, err := s.subs.CheckNow(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSubArticles(w http.ResponseWriter, r *http.Request) {
	pages := 5
	if raw := strings.TrimSpace(r.URL.Query().Get("pages")); raw != "" {
		fmt.Sscanf(raw, "%d", &pages)
	}
	res, err := s.subs.ListArticles(r.PathValue("id"), pages)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSubBackfill(w http.ResponseWriter, r *http.Request) {
	res, err := s.subs.Backfill(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSubsBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string   `json:"action"`
		IDs    []string `json:"ids"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.subs.Batch(req.Action, req.IDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSubsConfig(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if err := readJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.subs.SetConfig(payload))
}

func (s *Server) handleSubsSearch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Q string `json:"q"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.subs.Search(req.Q))
}

func (s *Server) handleOpenDir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	p := strings.TrimSpace(req.Path)
	if p == "" {
		writeError(w, http.StatusBadRequest, "路径为空")
		return
	}
	p = expandHome(p)
	abs, err := filepath.Abs(p)
	if err != nil {
		writeError(w, http.StatusBadRequest, "路径无效")
		return
	}
	if st, statErr := os.Stat(abs); statErr != nil || !st.IsDir() {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "目录尚不存在，下载时会自动创建"})
		return
	}
	if err := openDirectory(abs); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "无法打开目录：" + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- thumbnail proxy ----------

type thumbEntry struct {
	data  []byte
	ctype string
	at    time.Time
}

type thumbCache struct {
	mu    sync.Mutex
	items map[string]*thumbEntry
	total int64
}

const maxThumbBytes = 48 << 20

func newThumbCache() *thumbCache {
	return &thumbCache{items: map[string]*thumbEntry{}}
}

func (c *thumbCache) get(key string) (*thumbEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok || time.Since(e.at) > 30*time.Minute {
		return nil, false
	}
	return e, true
}

func (c *thumbCache) put(key string, e *thumbEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.total+int64(len(e.data)) > maxThumbBytes {
		c.items = map[string]*thumbEntry{}
		c.total = 0
	}
	c.items[key] = e
	c.total += int64(len(e.data))
}

func (s *Server) handleThumb(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("u"))
	if raw == "" {
		http.Error(w, "missing u", http.StatusBadRequest)
		return
	}
	size := strings.TrimSpace(r.URL.Query().Get("s"))
	switch size {
	case "", "300":
		size = "300"
	case "200", "400", "640":
	default:
		size = "300"
	}
	thumbURL := wx.ResizeURL(raw, size)
	if !allowedImageHost(thumbURL) {
		http.Error(w, "host not allowed", http.StatusForbidden)
		return
	}
	key := size + "|" + thumbURL
	if e, ok := s.thumbs.get(key); ok {
		w.Header().Set("Content-Type", e.ctype)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(e.data)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	data, ctype, err := s.client.FetchBytes(ctx, thumbURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if ctype == "" || !strings.HasPrefix(ctype, "image/") {
		ctype = http.DetectContentType(data)
	}
	if !strings.HasPrefix(ctype, "image/") {
		http.Error(w, "not an image", http.StatusBadGateway)
		return
	}
	s.thumbs.put(key, &thumbEntry{data: data, ctype: ctype, at: time.Now()})
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(data)
}

func allowedImageHost(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	for _, suffix := range []string{"qpic.cn", "qlogo.cn", "gtimg.cn", "qq.com"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

// ---------- helpers ----------

func cleanURLs(single string, many []string) ([]string, error) {
	raw := make([]string, 0, len(many)+1)
	if strings.TrimSpace(single) != "" {
		raw = append(raw, single)
	}
	raw = append(raw, many...)
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, chunk := range raw {
		for _, line := range strings.Split(chunk, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			u, err := wx.NormalizeInputURL(line)
			if err != nil {
				return nil, err
			}
			if seen[u] {
				continue
			}
			seen[u] = true
			out = append(out, u)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("请先填写文章链接")
	}
	if len(out) > 30 {
		return nil, fmt.Errorf("一次最多 30 条链接")
	}
	return out, nil
}

func readJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("请求格式错误：%v", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// downloadRoots lists the directories the WebUI offers as suggestions.
func downloadRoots(defaultOut string) []string {
	roots := []string{defaultOut}
	for _, cand := range []string{"/download"} {
		if cand == defaultOut {
			continue
		}
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			roots = append(roots, cand)
		}
	}
	return roots
}

func browserHost(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func openDirectory(dir string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	return cmd.Start()
}
