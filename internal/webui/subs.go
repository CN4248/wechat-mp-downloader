package webui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"wxmp/internal/wx"
)

// Subscription scanner (定时追更).
//
// WeChat only exposes two anonymous listing surfaces: the article page itself
// and the album (合集) JSON endpoint. A subscription therefore watches one of:
//
//   - kind=album   : one album link, new entries are picked up by diffing the
//     album's newest pages against the remembered keys;
//   - kind=account : every album discovered on the seed article's page, which
//     is the only public "new article" feed an account offers.
//
// Every downloaded file is remembered together with the template variables that
// produced it, so editing the naming rule later renames the old files without
// re-downloading (or re-parsing) anything. Files that vanished are left alone.

const (
	defaultSubInterval = 600 // 10 minutes
	minSubInterval     = 120
	defaultSubMaxNew   = 10
	defaultSubMinGap   = 60
	maxSubSeen         = 50000 // 只存 msgid_itemidx 字符串，足够大合集的"已见"
	maxSubFiles        = 5000
	maxScanPages       = 3 // 稳态扫描：最新 3 页（60 篇）足够发现新增
	maxScanAlbums      = 20
	maxAlbumPages      = 50000 // 仅防"接口原地返回同一页"的死循环，不是篇数上限
	maxAuthorPages     = 20    // 公众号全部文章：单次最多翻 20 页（200 篇）
)

// albumPages returns how many 20-item pages are needed to walk an album.
// count comes from the album metadata and may be 0 when unknown; in that case
// the caller's fallback (usually "walk until the end") is used. The only hard
// bound is maxAlbumPages, which exists to break a pathological loop, not to
// limit article count.
func albumPages(count, fallback int) int {
	pages := fallback
	if count > 0 {
		pages = (count + 19) / 20
	}
	if pages < 1 {
		pages = 1
	}
	if pages > maxAlbumPages {
		pages = maxAlbumPages
	}
	return pages
}

// SubFile is one downloaded file plus the variable snapshot used to name it.
type SubFile struct {
	Path string            `json:"path"`
	Root string            `json:"root"`
	Vars map[string]string `json:"vars"`
}

// Sub is one follow task (追更任务).
type Sub struct {
	ID       string         `json:"id"`
	Kind     string         `json:"kind"`   // album | account
	Target   string         `json:"target"` // album URL (album) or __biz (account)
	Biz      string         `json:"biz"`
	GhID     string         `json:"gh_id,omitempty"`
	Name     string         `json:"name"`
	Account  string         `json:"account,omitempty"`
	Albums   []*wx.AlbumRef `json:"albums"`
	Enabled  bool           `json:"enabled"`
	Interval int            `json:"interval"`
	Dir      string         `json:"dir"`
	Template string         `json:"template"`
	Backfill int            `json:"backfill"`
	// 下载起点（含起点自身）：从这一篇开始向更新的方向下载。
	Since      string     `json:"since"`
	SinceTitle string     `json:"since_title"`
	SinceTime  int64      `json:"since_time"`
	SinceOrder int        `json:"since_order"`
	SinceAlbum string     `json:"since_album"`
	SinceCount int        `json:"since_count"`
	Seen       []string   `json:"seen"`
	Files      []*SubFile `json:"files"`

	LastCheck   int64  `json:"last_check"`
	LastNew     int    `json:"last_new"`
	LastMessage string `json:"last_message"`
	LastError   string `json:"last_error"`
	Created     int64  `json:"created"`

	checking bool
}

type subsData struct {
	Enabled  bool   `json:"enabled"`
	Interval int    `json:"interval"`
	MaxNew   int    `json:"max_new"`
	MinGap   int    `json:"min_gap"`
	Subs     []*Sub `json:"subs"`
}

type accountEntry struct {
	Biz      string         `json:"biz"`
	Name     string         `json:"name"`
	GhID     string         `json:"gh_id,omitempty"`
	Albums   []*wx.AlbumRef `json:"albums"`
	LastSeen int64          `json:"last_seen"`
}

// SubView is one subscription rendered for the WebUI.
type SubView struct {
	ID                string         `json:"id"`
	Kind              string         `json:"kind"`
	Target            string         `json:"target"`
	Name              string         `json:"name"`
	Account           string         `json:"account,omitempty"`
	Biz               string         `json:"biz"`
	GhID              string         `json:"gh_id,omitempty"`
	Dir               string         `json:"dir"`
	DirEffective      string         `json:"dir_effective"`
	Template          string         `json:"template"`
	TemplateEffective string         `json:"template_effective"`
	Interval          int            `json:"interval"`
	IntervalEffective int            `json:"interval_effective"`
	Enabled           bool           `json:"enabled"`
	Albums            []*wx.AlbumRef `json:"albums"`
	Backfill          int            `json:"backfill"`
	Since             string         `json:"since"`
	SinceTitle        string         `json:"since_title"`
	SinceTime         int64          `json:"since_time"`
	SinceAlbum        string         `json:"since_album"`
	SinceCount        int            `json:"since_count"`
	SeenCount         int            `json:"seen_count"`
	FilesCount        int            `json:"files_count"`
	LastCheck         int64          `json:"last_check"`
	LastNew           int            `json:"last_new"`
	LastMessage       string         `json:"last_message"`
	LastError         string         `json:"last_error"`
	Created           int64          `json:"created"`
	NextCheck         int64          `json:"next_check"`
	Checking          bool           `json:"checking"`
}

// Subscriber stores subscriptions and runs the periodic scanner.
type Subscriber struct {
	client *wx.Client
	mgr    *Manager

	path         string
	accountsPath string

	defaultOut        string
	defaultName       string
	defaultCollection string

	mu       sync.Mutex
	data     subsData
	accounts []*accountEntry
	lastScan int64
	scanning bool
	dirty    bool
	stopCh   chan struct{}
	closed   bool
	lastSave time.Time
}

// NewSubscriber loads subscriptions from dataDir.
func NewSubscriber(client *wx.Client, mgr *Manager, dataDir, defaultOut, defaultName, defaultCollection string) *Subscriber {
	s := &Subscriber{
		client:            client,
		mgr:               mgr,
		path:              filepath.Join(dataDir, "subs.json"),
		accountsPath:      filepath.Join(dataDir, "accounts.json"),
		defaultOut:        defaultOut,
		defaultName:       defaultName,
		defaultCollection: defaultCollection,
		stopCh:            make(chan struct{}),
		data: subsData{
			Enabled:  true,
			Interval: defaultSubInterval,
			MaxNew:   defaultSubMaxNew,
			MinGap:   defaultSubMinGap,
		},
	}
	s.load()
	return s
}

// Start launches the periodic scanner.
func (s *Subscriber) Start() { go s.loop() }

// Close stops the scanner.
func (s *Subscriber) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	close(s.stopCh)
	s.saveNow()
}

// ---------- persistence ----------

func (s *Subscriber) load() {
	if raw, err := os.ReadFile(s.path); err == nil {
		var d subsData
		if json.Unmarshal(raw, &d) == nil {
			s.data.Enabled = d.Enabled
			s.data.Interval = d.Interval
			s.data.MaxNew = d.MaxNew
			s.data.MinGap = d.MinGap
			s.data.Subs = d.Subs
		}
	}
	if raw, err := os.ReadFile(s.accountsPath); err == nil {
		var list []*accountEntry
		if json.Unmarshal(raw, &list) == nil {
			s.accounts = list
		}
	}
	s.normalize()
	for _, sub := range s.data.Subs {
		s.normalizeSub(sub)
	}
}

func (s *Subscriber) normalize() {
	if s.data.Interval < minSubInterval {
		s.data.Interval = defaultSubInterval
	}
	if s.data.Interval > 7*24*3600 {
		s.data.Interval = 7 * 24 * 3600
	}
	if s.data.MaxNew < 1 {
		s.data.MaxNew = defaultSubMaxNew
	}
	if s.data.MaxNew > 200 {
		s.data.MaxNew = 200
	}
	if s.data.MinGap < 30 {
		s.data.MinGap = defaultSubMinGap
	}
	if s.data.MinGap > 3600 {
		s.data.MinGap = 3600
	}
}

func (s *Subscriber) normalizeSub(sub *Sub) {
	if sub.ID == "" {
		sub.ID = randomID()
	}
	switch sub.Kind {
	case "account", "author":
	default:
		sub.Kind = "album"
	}
	if sub.Interval < 0 {
		sub.Interval = 0
	}
	if sub.Backfill < 0 {
		sub.Backfill = 0
	}
	if sub.Backfill > 1000000 {
		sub.Backfill = 1000000
	}
	if sub.Albums == nil {
		sub.Albums = []*wx.AlbumRef{}
	}
	if sub.Seen == nil {
		sub.Seen = []string{}
	}
	if sub.Files == nil {
		sub.Files = []*SubFile{}
	}
	if sub.Created == 0 {
		sub.Created = time.Now().Unix()
	}
}

func (s *Subscriber) saveNow() {
	s.mu.Lock()
	if !s.dirty && !s.lastSave.IsZero() {
		s.mu.Unlock()
		return
	}
	s.dirty = false
	s.lastSave = time.Now()
	payload := s.data
	accounts := s.accounts
	s.mu.Unlock()

	writeAtomic(s.path, payload)
	writeAtomic(s.accountsPath, accounts)
}

func writeAtomic(path string, v any) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	raw, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return
	}
	_ = os.Chmod(tmp, 0o600)
	_ = os.Rename(tmp, path)
}

func (s *Subscriber) markDirty() {
	s.mu.Lock()
	s.dirty = true
	s.mu.Unlock()
}

func randomID() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return time.Now().Format("150405") + hex.EncodeToString(b)
}

// ---------- helpers ----------

func (s *Subscriber) intervalOf(sub *Sub) int {
	if sub.Interval >= minSubInterval {
		return sub.Interval
	}
	return s.data.Interval
}

func (s *Subscriber) templateOf(sub *Sub) string {
	if tpl := strings.TrimSpace(sub.Template); tpl != "" {
		return tpl
	}
	if sub.Kind == "album" {
		return s.defaultCollection
	}
	return s.defaultName
}

// feedToCollectionArticle adapts an account-feed entry to the album entry type
// so scans, start points and naming can share one code path.
func feedToCollectionArticle(a *wx.FeedArticle) *wx.CollectionArticle {
	if a == nil {
		return nil
	}
	return &wx.CollectionArticle{
		Title: a.Title, URL: a.URL, MsgID: a.Mid, ItemIdx: a.Idx,
		PublishTime: a.PublishTime, Cover: a.Cover,
	}
}

// walkFeed pages through an account's article feed (newest first). fn returns
// true to stop paging early.
func (s *Subscriber) walkFeed(ctx context.Context, biz string, maxPages int, fn func([]*wx.FeedArticle) bool) error {
	offset := 0
	for page := 0; page < maxPages; page++ {
		pg, err := s.client.FetchAccountFeed(ctx, biz, offset, 10)
		if err != nil {
			return err
		}
		if len(pg.Articles) == 0 {
			return nil
		}
		if fn(pg.Articles) {
			return nil
		}
		if !pg.Continue || pg.NextOffset <= offset {
			return nil
		}
		offset = pg.NextOffset
	}
	return nil
}

func (s *Subscriber) dirOf(sub *Sub) string {
	if dir := strings.TrimSpace(sub.Dir); dir != "" {
		if abs, err := filepath.Abs(expandHome(dir)); err == nil {
			return abs
		}
	}
	return s.defaultOut
}

func (s *Subscriber) findSub(id string) *Sub {
	for _, sub := range s.data.Subs {
		if sub.ID == id {
			return sub
		}
	}
	return nil
}

// ensureAlbumCount learns an album's total article count once. The JSON paging
// endpoint carries no pos_num, and <collection_order> needs the absolute
// position, so the count is cached on the album reference (and persisted).
func (s *Subscriber) ensureAlbumCount(ctx context.Context, subID, biz string, album *wx.AlbumRef) {
	if album == nil || album.ID == "" || album.ArticleCount > 0 {
		return
	}
	info, err := s.client.FetchAlbumInfo(ctx, wx.AlbumURL(biz, album.ID))
	if err != nil || info.ArticleCount <= 0 {
		return
	}
	album.ArticleCount = info.ArticleCount
	s.mu.Lock()
	if live := s.findSub(subID); live != nil {
		for _, a := range live.Albums {
			if a.ID == album.ID && a.ArticleCount == 0 {
				a.ArticleCount = info.ArticleCount
			}
		}
		s.dirty = true
	}
	s.mu.Unlock()
}

// orderAt maps a newest-first paging index to the album position used by
// <collection_order>. WeChat's pos_num decreases along the folder's own display
// order, so the first entry of a newest-first page holds the highest number.
func orderAt(idx, count int) int {
	if count <= 0 {
		return 0
	}
	order := count - idx
	if order < 1 {
		order = 1
	}
	return order
}

// albumCursor pages an album newest-first. WeChat returns entries in the
// album's own display order by default (some albums are oldest-first), so the
// first request detects that and switches to is_reverse=1 when needed.
type albumCursor struct {
	biz       string
	album     *wx.AlbumRef
	begin     string
	beginIdx  string
	reverse   bool
	ascending bool
	stalled   bool // 接口原地返回同一页时置位，避免死循环
	started   bool
}

func (s *Subscriber) nextAlbumPage(ctx context.Context, cur *albumCursor, count int) (*wx.AlbumPage, error) {
	if !cur.started {
		pg, err := s.client.FetchAlbumPage(ctx, cur.biz, cur.album.ID, "", "", count, false)
		if err != nil {
			return nil, err
		}
		if ascendingPage(pg.Articles) {
			if rev, revErr := s.client.FetchAlbumPage(ctx, cur.biz, cur.album.ID, "", "", count, true); revErr == nil {
				pg = rev
				cur.reverse = true
			}
		}
		cur.started = true
		cur.ascending = ascendingPage(pg.Articles)
		cur.advance(pg)
		return pg, nil
	}
	pg, err := s.client.FetchAlbumPage(ctx, cur.biz, cur.album.ID, cur.begin, cur.beginIdx, count, cur.reverse)
	if err != nil {
		return nil, err
	}
	cur.advance(pg)
	return pg, nil
}

func (cur *albumCursor) advance(pg *wx.AlbumPage) {
	if pg == nil || len(pg.Articles) == 0 {
		cur.begin, cur.beginIdx = "", ""
		return
	}
	last := pg.Articles[len(pg.Articles)-1]
	if cur.begin != "" && last.MsgID == cur.begin && last.ItemIdx == cur.beginIdx {
		cur.stalled = true
	}
	cur.begin, cur.beginIdx = last.MsgID, last.ItemIdx
}

// sinceMatches reports whether an entry is inside an author feed's download
// range when a start point (起点，含起点自身) is configured.
//
// Album subscriptions do not use this filter: their start point marks the whole
// album as seen (including older entries), which is exact even when the folder
// order is not chronological.
func sinceMatches(key string, art *wx.CollectionArticle, sinceKey string, sinceTime int64) bool {
	if sinceKey == "" {
		return true
	}
	if key == sinceKey {
		return true
	}
	if sinceTime > 0 && art.PublishTime > 0 {
		return art.PublishTime >= sinceTime
	}
	return false
}

// ascendingPage reports whether an album page is ordered oldest → newest,
// which means new entries live at the end of the album instead of the top.
func ascendingPage(arts []*wx.CollectionArticle) bool {
	if len(arts) < 2 {
		return false
	}
	first, last := arts[0], arts[len(arts)-1]
	if first == nil || last == nil || first.PublishTime <= 0 || last.PublishTime <= 0 {
		return false
	}
	return last.PublishTime > first.PublishTime
}

func artKey(a *wx.CollectionArticle) string {
	if a.MsgID != "" {
		return a.MsgID + "_" + a.ItemIdx
	}
	return a.URL
}

func toSet(list []string) map[string]bool {
	out := make(map[string]bool, len(list))
	for _, v := range list {
		out[v] = true
	}
	return out
}

// ---------- views ----------

func (s *Subscriber) viewOf(sub *Sub) *SubView {
	v := &SubView{
		ID: sub.ID, Kind: sub.Kind, Target: sub.Target, Name: sub.Name, Account: sub.Account, Biz: sub.Biz,
		GhID: sub.GhID, Dir: sub.Dir, DirEffective: s.dirOf(sub),
		Template: sub.Template, TemplateEffective: s.templateOf(sub),
		Interval: sub.Interval, IntervalEffective: s.intervalOf(sub),
		Enabled: sub.Enabled, Albums: sub.Albums, Backfill: sub.Backfill,
		Since: sub.Since, SinceTitle: sub.SinceTitle, SinceTime: sub.SinceTime,
		SinceAlbum: sub.SinceAlbum, SinceCount: sub.SinceCount,
		SeenCount: len(sub.Seen), FilesCount: len(sub.Files),
		LastCheck: sub.LastCheck, LastNew: sub.LastNew, LastMessage: sub.LastMessage,
		LastError: sub.LastError, Created: sub.Created, Checking: sub.checking,
	}
	if sub.Enabled && s.data.Enabled && sub.LastCheck > 0 {
		v.NextCheck = sub.LastCheck + int64(s.intervalOf(sub))
	}
	return v
}

func (s *Subscriber) nextRun() int64 {
	if !s.data.Enabled {
		return -1
	}
	now := time.Now().Unix()
	best := int64(-1)
	for _, sub := range s.data.Subs {
		if !sub.Enabled {
			continue
		}
		due := sub.LastCheck + int64(s.intervalOf(sub))
		if due < now {
			due = now
		}
		if best < 0 || due < best {
			best = due
		}
	}
	return best
}

// Snapshot returns the whole scanner state for the WebUI.
func (s *Subscriber) cookieStatus() (set, uin, key bool) {
	if s.client == nil {
		return false, false, false
	}
	return s.client.CookieStatus()
}

func (s *Subscriber) Snapshot() map[string]any {
	set, uin, key := s.cookieStatus()
	s.mu.Lock()
	defer s.mu.Unlock()
	views := make([]*SubView, 0, len(s.data.Subs))
	for _, sub := range s.data.Subs {
		views = append(views, s.viewOf(sub))
	}
	return map[string]any{
		"config": map[string]any{
			"enabled":      s.data.Enabled,
			"interval":     s.data.Interval,
			"max_new":      s.data.MaxNew,
			"min_gap":      s.data.MinGap,
			"min_interval": minSubInterval,
		},
		"subs":      views,
		"next_run":  s.nextRun(),
		"last_scan": s.lastScan,
		"now":       time.Now().Unix(),
		"accounts":  len(s.accounts),
		"queue":     s.mgr.QueueState(),
		"cookie":    map[string]any{"set": set, "uin": uin, "key": key},
	}
}

// ---------- accounts cache (author search) ----------

// RememberAccount caches one resolved account so it can be searched offline.
func (s *Subscriber) RememberAccount(info *wx.AccountInfo) {
	if info == nil || info.Biz == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var hit *accountEntry
	for _, e := range s.accounts {
		if e.Biz == info.Biz {
			hit = e
			break
		}
	}
	if hit == nil {
		hit = &accountEntry{Biz: info.Biz}
		s.accounts = append(s.accounts, hit)
	}
	if info.Name != "" {
		hit.Name = info.Name
	}
	if info.GhID != "" {
		hit.GhID = info.GhID
	}
	for _, a := range info.Albums {
		if a == nil || a.ID == "" {
			continue
		}
		found := false
		for _, old := range hit.Albums {
			if old.ID == a.ID {
				if a.Title != "" {
					old.Title = a.Title
				}
				found = true
				break
			}
		}
		if !found && len(hit.Albums) < maxScanAlbums {
			hit.Albums = append(hit.Albums, a)
		}
	}
	hit.LastSeen = time.Now().Unix()
	if len(s.accounts) > 300 {
		s.accounts = s.accounts[len(s.accounts)-300:]
	}
	s.dirty = true
}

// Search finds an account by name / gh id / biz, or resolves a pasted link.
//
// WeChat offers no anonymous account-search endpoint (Sogou's index returns
// nothing for anonymous callers), so this searches the local library that every
// parsed article feeds, and resolves links on demand.
func (s *Subscriber) Search(q string) map[string]any {
	q = strings.TrimSpace(q)
	out := make([]map[string]any, 0, 20)
	note := ""
	if q == "" {
		s.mu.Lock()
		list := append([]*accountEntry(nil), s.accounts...)
		s.mu.Unlock()
		sort.Slice(list, func(i, j int) bool { return list[i].LastSeen > list[j].LastSeen })
		for _, e := range list {
			if len(out) >= 30 {
				break
			}
			out = append(out, accountMap(e, "recent"))
		}
		if len(out) == 0 {
			note = "还没有记录任何公众号。粘贴任意一篇文章链接，或直接在下方按链接添加追更任务。"
		}
		return map[string]any{"accounts": out, "note": note}
	}

	if looksLikeLink(q) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		info, err := s.client.ResolveAccount(ctx, q)
		cancel()
		if err != nil {
			return map[string]any{"accounts": out, "note": "链接解析失败：" + err.Error()}
		}
		s.RememberAccount(info)
		s.saveNow()
		entry := &accountEntry{Biz: info.Biz, Name: info.Name, GhID: info.GhID, Albums: info.Albums, LastSeen: time.Now().Unix()}
		out = append(out, accountMap(entry, "link"))
		if len(info.Albums) == 0 {
			return map[string]any{"accounts": out, "note": "这个链接没有暴露任何公开合集；微信只公开合集列表，无法扫描该公众号的新文章。"}
		}
		return map[string]any{"accounts": out, "note": "已从链接解析出公众号，可直接添加追更。"}
	}

	low := strings.ToLower(q)
	s.mu.Lock()
	list := append([]*accountEntry(nil), s.accounts...)
	s.mu.Unlock()
	for _, e := range list {
		if strings.Contains(strings.ToLower(e.Name), low) ||
			strings.Contains(strings.ToLower(e.GhID), low) ||
			strings.Contains(strings.ToLower(e.Biz), low) {
			out = append(out, accountMap(e, "cache"))
		}
		if len(out) >= 30 {
			break
		}
	}
	if len(out) == 0 {
		note = "本机还没有记录这个公众号：微信没有对外的公众号搜索接口，请粘贴该公众号任意一篇文章链接来添加。"
	}
	return map[string]any{"accounts": out, "note": note}
}

func accountMap(e *accountEntry, source string) map[string]any {
	name := e.Name
	if name == "" {
		name = "公众号 " + e.Biz
	}
	return map[string]any{
		"name": name, "biz": e.Biz, "gh_id": e.GhID,
		"albums": e.Albums, "source": source,
	}
}

func looksLikeLink(s string) bool {
	low := strings.ToLower(s)
	return strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") ||
		strings.Contains(low, "mp.weixin.qq.com")
}

// ---------- create / update ----------

// Upsert creates or updates a subscription. When the naming rule changes, the
// already downloaded files are renamed too (missing files are left untouched).
func (s *Subscriber) Upsert(payload map[string]any) (*SubView, map[string]any, error) {
	id := strings.TrimSpace(str(payload["id"]))
	s.mu.Lock()
	var sub *Sub
	if id != "" {
		sub = s.findSub(id)
		if sub == nil {
			s.mu.Unlock()
			return nil, nil, fmt.Errorf("追更任务不存在")
		}
	}
	s.mu.Unlock()

	link := strings.TrimSpace(firstStr(payload["url"], payload["target"]))
	kind := strings.TrimSpace(str(payload["kind"]))
	created := sub == nil

	var info *wx.AccountInfo
	if link != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		got, err := s.client.ResolveAccount(ctx, link)
		cancel()
		if err != nil && (created || wx.IsAlbumURL(link)) {
			return nil, nil, err
		}
		info = got
	}
	if sub == nil {
		sub = &Sub{ID: randomID(), Enabled: true, Created: time.Now().Unix()}
	}
	if kind != "" {
		sub.Kind = kind
	}
	if info != nil {
		if info.Kind == "album" {
			sub.Kind = "album"
		} else if sub.Kind == "" {
			sub.Kind = "account"
		}
		sub.Biz = firstNonEmptyStr(info.Biz, sub.Biz)
		if info.Name != "" {
			sub.Account = info.Name
			sub.Name = info.Name
		}
		if info.Kind == "album" && info.Title != "" {
			// 追更合集时用合集标题作为任务名，公众号名单独展示。
			sub.Name = info.Title
		}
		if info.GhID != "" {
			sub.GhID = info.GhID
		}
		if len(info.Albums) > 0 && (created || sub.Kind == "account") {
			sub.Albums = mergeAlbums(sub.Albums, info.Albums)
		} else if len(info.Albums) > 0 && len(sub.Albums) == 0 {
			sub.Albums = mergeAlbums(sub.Albums, info.Albums)
		}
		if sub.Kind == "album" && len(info.Albums) > 0 {
			sub.Albums = []*wx.AlbumRef{info.Albums[0]}
			if sub.Albums[0].ArticleCount == 0 {
				sub.Albums[0].ArticleCount = info.ArticleCount
			}
		}
		s.RememberAccount(info)
	}
	if biz := strings.TrimSpace(str(payload["biz"])); biz != "" {
		sub.Biz = biz
	}
	// Explicit album ids from the UI (account subscriptions can own many albums).
	if rawAlbums, ok := payload["albums"]; ok {
		if list, ok2 := rawAlbums.([]any); ok2 {
			for _, item := range list {
				switch v := item.(type) {
				case string:
					if v = strings.TrimSpace(v); v != "" {
						sub.Albums = mergeAlbums(sub.Albums, []*wx.AlbumRef{{ID: v, Title: "合集 " + v, Biz: sub.Biz}})
					}
				case map[string]any:
					aid := strings.TrimSpace(str(v["id"]))
					if aid == "" {
						continue
					}
					sub.Albums = mergeAlbums(sub.Albums, []*wx.AlbumRef{{ID: aid, Title: firstNonEmptyStr(str(v["title"]), "合集 "+aid), Biz: sub.Biz}})
				}
			}
		}
	}
	if name := strings.TrimSpace(str(payload["name"])); name != "" {
		sub.Name = name
	}
	if sub.Name == "" {
		switch sub.Kind {
		case "account":
			sub.Name = "公众号 " + shortBiz(sub.Biz)
		default:
			sub.Name = "合集追更"
		}
	}
	if sub.Kind == "album" {
		if len(sub.Albums) == 0 {
			return nil, nil, fmt.Errorf("没有解析到合集 id，请粘贴完整合集链接")
		}
		if sub.Albums[0].Title != "" && (created || payload["name"] == nil) {
			sub.Name = sub.Albums[0].Title
		}
		sub.Biz = firstNonEmptyStr(sub.Biz, sub.Albums[0].Biz)
		sub.Albums = sub.Albums[:1]
		sub.Target = wx.AlbumURL(sub.Biz, sub.Albums[0].ID)
	} else if sub.Kind == "author" {
		// 按篇追更：只用 __biz 拉公众号文章列表，不需要合集。
		if sub.Biz == "" {
			return nil, nil, fmt.Errorf("没有解析到公众号 __biz，请粘贴该公众号任意文章链接")
		}
		sub.Target = sub.Biz
	} else {
		if sub.Biz == "" {
			return nil, nil, fmt.Errorf("没有解析到公众号 __biz，请粘贴该公众号任意文章链接")
		}
		if len(sub.Albums) == 0 {
			return nil, nil, fmt.Errorf("这个公众号没有公开合集：微信只公开合集列表，无法扫描它的新文章。请改用「公众号全部文章」或合集链接追更")
		}
		sub.Target = sub.Biz
	}
	if len(sub.Albums) > maxScanAlbums {
		sub.Albums = sub.Albums[:maxScanAlbums]
	}

	oldTemplate := s.templateOf(sub)
	oldDir := s.dirOf(sub)
	if v, ok := payload["template"]; ok {
		tpl := strings.TrimSpace(str(v))
		if tpl != "" {
			if err := wx.ValidateTemplate(tpl); err != nil {
				return nil, nil, fmt.Errorf("重命名规则：%w", err)
			}
		}
		sub.Template = tpl
	}
	if v, ok := payload["dir"]; ok {
		dir := strings.TrimSpace(str(v))
		if dir != "" {
			abs, err := filepath.Abs(expandHome(dir))
			if err != nil {
				return nil, nil, fmt.Errorf("下载目录无效：%v", err)
			}
			if st, statErr := os.Stat(abs); statErr == nil && !st.IsDir() {
				return nil, nil, fmt.Errorf("下载目录不是文件夹：%s", abs)
			}
			dir = abs
		}
		sub.Dir = dir
	}
	if v, ok := payload["enabled"]; ok {
		sub.Enabled = boolVal(v)
	}
	if v, ok := payload["interval"]; ok {
		sub.Interval = intVal(v)
		if sub.Interval != 0 && sub.Interval < minSubInterval {
			sub.Interval = minSubInterval
		}
	}
	if v, ok := payload["backfill"]; ok {
		sub.Backfill = intVal(v)
	}

	// 下载起点（含起点自身）。设置/清除起点都会重置基线，避免「已见」把起点
	// 之后的文章挡掉；清空 Seen 是安全的，因为已有文件会按「已存在」跳过。
	sinceApplied := false
	if _, ok := payload["since"]; ok {
		key := strings.TrimSpace(str(payload["since"]))
		if key == "" {
			sub.Since, sub.SinceTitle, sub.SinceTime = "", "", 0
			sub.SinceOrder, sub.SinceAlbum, sub.SinceCount = 0, "", 0
			sub.Seen = []string{}
			sub.LastCheck = 0
			sinceApplied = true
		} else {
			sub.Since = key
			sub.SinceTitle = strings.TrimSpace(str(payload["since_title"]))
			sub.SinceTime = int64(intVal(payload["since_time"]))
			sub.SinceOrder = intVal(payload["since_order"])
			sub.SinceAlbum = strings.TrimSpace(str(payload["since_album"]))
			sub.SinceCount = 0
			sub.Seen = []string{}
			sub.LastCheck = 0
			sinceApplied = true
		}
	}
	if created && link != "" && sub.LastCheck == 0 && payload["enabled"] == nil {
		sub.Enabled = true
	}

	newTemplate := s.templateOf(sub)
	newDir := s.dirOf(sub)
	extra := map[string]any{}
	if !created && (oldTemplate != newTemplate || oldDir != newDir) {
		s.mu.Lock()
		moved, missing := s.relayoutLocked(sub, newTemplate)
		s.mu.Unlock()
		extra["renamed"] = moved
		extra["missing"] = missing
	}

	s.mu.Lock()
	if created {
		s.data.Subs = append(s.data.Subs, sub)
	}
	s.normalizeSub(sub)
	s.dirty = true
	view := s.viewOf(sub)
	s.mu.Unlock()
	s.saveNow()

	if sinceApplied && sub.Since != "" && sub.Enabled {
		// 立即按起点补下（从起点含起点向更新的方向），不等待定时器。
		go func(id string) { _, _ = s.Backfill(id) }(sub.ID)
	} else if created && sub.Enabled {
		// A brand new subscription establishes its baseline right away.
		go func(id string) { _, _ = s.CheckNow(id) }(sub.ID)
	}
	return view, extra, nil
}

func mergeAlbums(dst, add []*wx.AlbumRef) []*wx.AlbumRef {
	for _, a := range add {
		if a == nil || a.ID == "" {
			continue
		}
		found := false
		for _, old := range dst {
			if old.ID == a.ID {
				if a.Title != "" {
					old.Title = a.Title
				}
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, &wx.AlbumRef{ID: a.ID, Title: a.Title, Biz: a.Biz})
		}
	}
	return dst
}

// Delete removes subscriptions.
func (s *Subscriber) Delete(ids []string) int {
	if len(ids) == 0 {
		return 0
	}
	set := toSet(ids)
	s.mu.Lock()
	before := len(s.data.Subs)
	kept := make([]*Sub, 0, before)
	for _, sub := range s.data.Subs {
		if !set[sub.ID] {
			kept = append(kept, sub)
		}
	}
	s.data.Subs = kept
	s.dirty = true
	s.mu.Unlock()
	s.saveNow()
	return before - len(kept)
}

// SetConfig updates the global scanner settings.
func (s *Subscriber) SetConfig(payload map[string]any) map[string]any {
	s.mu.Lock()
	if v, ok := payload["enabled"]; ok {
		s.data.Enabled = boolVal(v)
	}
	if v, ok := payload["interval"]; ok {
		s.data.Interval = intVal(v)
	}
	if v, ok := payload["max_new"]; ok {
		s.data.MaxNew = intVal(v)
	}
	if v, ok := payload["min_gap"]; ok {
		s.data.MinGap = intVal(v)
	}
	s.normalize()
	s.dirty = true
	s.mu.Unlock()
	s.saveNow()
	return s.Snapshot()
}

// Batch enables / disables / deletes / scans several subscriptions at once.
func (s *Subscriber) Batch(action string, ids []string) (map[string]any, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("没有选择追更任务")
	}
	set := toSet(ids)
	switch action {
	case "enable", "disable":
		count := 0
		s.mu.Lock()
		for _, sub := range s.data.Subs {
			if set[sub.ID] {
				sub.Enabled = action == "enable"
				count++
			}
		}
		s.dirty = true
		s.mu.Unlock()
		s.saveNow()
		return map[string]any{"action": action, "count": count}, nil
	case "delete":
		return map[string]any{"action": action, "count": s.Delete(ids)}, nil
	case "check":
		count := 0
		for _, id := range ids {
			if _, err := s.CheckNow(id); err == nil {
				count++
			}
		}
		return map[string]any{"action": action, "count": count}, nil
	}
	return nil, fmt.Errorf("未知的批量操作：%s", action)
}

// ---------- rename ----------

// relayoutLocked renames every remembered file with the current template.
// Files that no longer exist are skipped (the article may be gone).
// Callers must hold s.mu.
func (s *Subscriber) relayoutLocked(sub *Sub, tpl string) (moved, missing int) {
	for _, rec := range sub.Files {
		if rec == nil || rec.Path == "" {
			continue
		}
		if _, err := os.Stat(rec.Path); err != nil {
			missing++
			continue
		}
		if len(rec.Vars) == 0 {
			continue
		}
		rel, err := wx.RenderRelPath(tpl, rec.Vars)
		if err != nil {
			continue
		}
		root := rec.Root
		if root == "" {
			root = s.dirOf(sub)
		}
		target := filepath.Join(root, filepath.FromSlash(rel))
		if samePath(target, rec.Path) {
			continue
		}
		if _, err := os.Stat(target); err == nil {
			target = uniquePath(target)
			if samePath(target, rec.Path) {
				continue
			}
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			continue
		}
		if err := moveFile(rec.Path, target); err != nil {
			continue
		}
		rec.Path = target
		moved++
	}
	if moved > 0 {
		s.dirty = true
	}
	return moved, missing
}

func samePath(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return a == b
	}
	return aa == bb
}

func uniquePath(target string) string {
	ext := filepath.Ext(target)
	stem := strings.TrimSuffix(target, ext)
	for i := 2; i < 1000; i++ {
		cand := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if _, err := os.Stat(cand); err != nil {
			return cand
		}
	}
	return target
}

// moveFile renames a file, falling back to copy+delete across filesystems.
func moveFile(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		_ = os.Remove(to)
		return err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(to)
		return err
	}
	if info, err := os.Stat(from); err == nil {
		_ = os.Chmod(to, info.Mode().Perm())
	}
	return os.Remove(from)
}

// recordFile remembers one downloaded file for a subscription.
func (s *Subscriber) recordFile(subID string, task JobTask, media *wx.Media, r *wx.Result) {
	if r == nil || r.Path == "" || r.Status != "ok" {
		return
	}
	s.mu.Lock()
	sub := s.findSub(subID)
	if sub == nil {
		s.mu.Unlock()
		return
	}
	root := task.OutDir
	rec := &SubFile{Path: r.Path, Root: root, Vars: wx.VarsOf(media)}
	replaced := false
	for i, old := range sub.Files {
		if samePath(old.Path, rec.Path) {
			sub.Files[i] = rec
			replaced = true
			break
		}
	}
	if !replaced {
		sub.Files = append(sub.Files, rec)
	}
	if len(sub.Files) > maxSubFiles {
		sub.Files = sub.Files[len(sub.Files)-maxSubFiles:]
	}
	s.dirty = true
	s.mu.Unlock()
}

// ---------- scanning ----------

// CheckNow scans one subscription immediately.
func (s *Subscriber) CheckNow(id string) (map[string]any, error) {
	s.mu.Lock()
	sub := s.findSub(id)
	if sub == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("追更任务不存在")
	}
	if sub.checking || s.scanning {
		s.mu.Unlock()
		return nil, fmt.Errorf("正在扫描中，请稍后再试")
	}
	s.scanning = true
	sub.checking = true
	s.mu.Unlock()

	result, err := s.scan(sub)

	s.mu.Lock()
	sub.checking = false
	s.scanning = false
	sub.LastCheck = time.Now().Unix()
	if err != nil {
		sub.LastError = err.Error()
		if sub.LastMessage == "" {
			sub.LastMessage = "扫描失败：" + err.Error()
		}
	} else if result != nil {
		sub.LastError = ""
	}
	s.lastScan = time.Now().Unix()
	s.dirty = true
	view := s.viewOf(sub)
	s.mu.Unlock()
	s.saveNow()
	if err != nil {
		return nil, err
	}
	return map[string]any{"sub": view, "result": result}, nil
}

func (s *Subscriber) loop() {
	time.Sleep(10 * time.Second)
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-t.C:
			s.tick()
		}
	}
}

func (s *Subscriber) tick() {
	s.mu.Lock()
	if s.closed || !s.data.Enabled {
		s.mu.Unlock()
		return
	}
	now := time.Now().Unix()
	if s.scanning || now-s.lastScan < int64(s.data.MinGap) {
		stale := s.dirty && time.Since(s.lastSave) > 2*time.Second
		s.mu.Unlock()
		if stale {
			s.saveNow()
		}
		return
	}
	var due *Sub
	for _, sub := range s.data.Subs {
		if !sub.Enabled || sub.checking {
			continue
		}
		if now-sub.LastCheck < int64(s.intervalOf(sub)) {
			continue
		}
		if due == nil || sub.LastCheck < due.LastCheck {
			due = sub
		}
	}
	id := ""
	if due != nil {
		id = due.ID
	}
	s.mu.Unlock()
	if id == "" {
		if s.dirty {
			s.saveNow()
		}
		return
	}
	// Jitter keeps scans from landing on exactly the same second every time.
	time.Sleep(time.Duration(1000+int(time.Now().UnixNano()%3000)) * time.Millisecond)
	_, _ = s.CheckNow(id)
}

// scanPick is one new album entry plus the album it came from.
type scanPick struct {
	art   *wx.CollectionArticle
	album *wx.AlbumRef
}

// scan collects new entries and queues a job for them.
// ListArticles returns recent entries of a subscription (newest first) so the
// UI can pick a download start point (起点).
func (s *Subscriber) ListArticles(id string, pages int) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	s.mu.Lock()
	sub := s.findSub(id)
	if sub == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("追更任务不存在")
	}
	albums := append([]*wx.AlbumRef(nil), sub.Albums...)
	biz, kind, target, name := sub.Biz, sub.Kind, sub.Target, sub.Name
	sinceKey, sinceTitle := sub.Since, sub.SinceTitle
	seen := toSet(sub.Seen)
	s.mu.Unlock()

	if kind == "album" && len(albums) == 0 && biz != "" {
		albums = []*wx.AlbumRef{{ID: wx.AlbumIDFromURL(target), Title: name}}
	}
	if kind != "author" && len(albums) == 0 {
		return nil, fmt.Errorf("这个追更任务没有可列举的合集")
	}
	if pages < 0 {
		pages = 0
	}
	if pages > maxAlbumPages {
		pages = maxAlbumPages
	}
	if len(albums) > 1 && pages > 10 {
		pages = 10
	}

	type listed struct {
		key, title, url, cover, albumID, albumTitle string
		order, seq                                  int
		publish                                     int64
		seen, current                               bool
	}
	items := make([]listed, 0, 128)
	var firstErr error
	if kind == "author" {
		firstErr = s.walkFeed(ctx, biz, albumPages(0, pages), func(arts []*wx.FeedArticle) bool {
			for _, a := range arts {
				art := feedToCollectionArticle(a)
				if art == nil {
					continue
				}
				key := artKey(art)
				items = append(items, listed{
					key: key, title: art.Title, url: art.URL, cover: art.Cover,
					publish: art.PublishTime, seen: seen[key], current: key == sinceKey,
				})
			}
			return false
		})
		if firstErr != nil && len(items) == 0 {
			return nil, firstErr
		}
	}
	for _, album := range albums {
		if kind == "author" {
			break
		}
		if album == nil || album.ID == "" {
			continue
		}
		s.ensureAlbumCount(ctx, id, biz, album)
		cur := &albumCursor{biz: biz, album: album}
		limit := maxAlbumPages
		if pages > 0 {
			limit = pages
		} else if album.ArticleCount > 0 {
			limit = albumPages(album.ArticleCount, maxAlbumPages)
		}
		idx := 0
		base := len(items)
		for page := 0; page < limit; page++ {
			pg, err := s.nextAlbumPage(ctx, cur, 20)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				break
			}
			if len(pg.Articles) == 0 {
				break
			}
			for _, art := range pg.Articles {
				if art.Order == 0 {
					art.Order = orderAt(idx, album.ArticleCount)
				}
				idx++
				key := artKey(art)
				items = append(items, listed{
					key: key, title: art.Title, url: art.URL, cover: art.Cover,
					albumID: album.ID, albumTitle: album.Title, order: art.Order,
					seq:     len(items) - base,
					publish: art.PublishTime, seen: seen[key], current: key == sinceKey,
				})
			}
			if !pg.Continue || cur.stalled {
				break
			}
		}
	}
	if len(items) == 0 && firstErr != nil {
		return nil, firstErr
	}
	totals := map[string]int{}
	for _, it := range items {
		totals[it.albumID]++
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].publish > items[j].publish })
	articles := make([]map[string]any, 0, len(items))
	for _, it := range items {
		order := it.order
		if order == 0 && it.albumID != "" {
			order = totals[it.albumID] - it.seq
		}
		articles = append(articles, map[string]any{
			"key": it.key, "title": it.title, "order": order,
			"publish_time": it.publish, "url": it.url, "cover": it.cover,
			"album_id": it.albumID, "album_title": it.albumTitle,
			"seen": it.seen, "current": it.current,
		})
	}
	out := map[string]any{
		"sub_id":   id,
		"articles": articles,
		"total":    len(articles),
		"since":    map[string]any{"key": sinceKey, "title": sinceTitle},
	}
	if firstErr != nil {
		out["warning"] = firstErr.Error()
	}
	return out, nil
}

// Backfill queues everything from the configured start point (inclusive)
// towards newer entries. The start marker is resolved against the album's
// newest pages; the range is bounded by the start's publish time so a deleted
// start article still works.
func (s *Subscriber) Backfill(id string) (map[string]any, error) {
	// 不设篇数上限，只靠超时兜底：超大合集的整册补下需要时间。
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	s.mu.Lock()
	sub := s.findSub(id)
	if sub == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("追更任务不存在")
	}
	if sub.Since == "" {
		s.mu.Unlock()
		return nil, fmt.Errorf("还没有设置起点，请先选择从哪一篇开始下载")
	}
	if s.scanning {
		s.mu.Unlock()
		return nil, fmt.Errorf("正在扫描或补下中，请稍后再试")
	}
	s.scanning = true
	defer func() {
		s.mu.Lock()
		s.scanning = false
		s.mu.Unlock()
	}()
	albums := append([]*wx.AlbumRef(nil), sub.Albums...)
	biz, kind, target, name := sub.Biz, sub.Kind, sub.Target, sub.Name
	sinceKey, sinceTitle := sub.Since, sub.SinceTitle
	sinceTime, sinceOrder, sinceAlbum := sub.SinceTime, sub.SinceOrder, sub.SinceAlbum
	tpl, dir := s.templateOf(sub), s.dirOf(sub)
	s.mu.Unlock()

	if kind == "album" && len(albums) == 0 && biz != "" {
		albums = []*wx.AlbumRef{{ID: wx.AlbumIDFromURL(target), Title: name}}
	}
	picks := make([]scanPick, 0, 40)
	foundStart := false
	var album *wx.AlbumRef

	if kind == "author" {
		ended := false
		err := s.walkFeed(ctx, biz, maxAuthorPages, func(arts []*wx.FeedArticle) bool {
			for _, a := range arts {
				art := feedToCollectionArticle(a)
				if art == nil {
					continue
				}
				key := artKey(art)
				if key == sinceKey {
					if sinceTitle == "" {
						sinceTitle = art.Title
					}
					if sinceTime == 0 {
						sinceTime = art.PublishTime
					}
					picks = append(picks, scanPick{art: art})
					foundStart = true
					ended = true
					break
				}
				if sinceTime > 0 && art.PublishTime > 0 && art.PublishTime < sinceTime {
					ended = true
					break
				}
				picks = append(picks, scanPick{art: art})
			}
			return ended
		})
		if err != nil {
			return nil, err
		}
	} else {
		for _, a := range albums {
			if a == nil || a.ID == "" {
				continue
			}
			if sinceAlbum == "" || a.ID == sinceAlbum {
				album = a
				break
			}
		}
		if album == nil {
			return nil, fmt.Errorf("找不到起点所在的合集，请重新选择起点")
		}
		s.ensureAlbumCount(ctx, id, biz, album)
		cur := &albumCursor{biz: biz, album: album}
		// 整册枚举（受 2000 篇安全上限约束）：既拿到"起点→最新"的范围，
		// 也把起点之前的老文章一并记为已见，之后扫描就不会再回头下载它们。
		pageBudget := albumPages(album.ArticleCount, maxAlbumPages)
		all := make([]scanPick, 0, 64)
		idx := 0
		for page := 0; page < pageBudget; page++ {
			pg, err := s.nextAlbumPage(ctx, cur, 20)
			if err != nil {
				return nil, err
			}
			if len(pg.Articles) == 0 {
				break
			}
			pageKeys := make([]string, 0, len(pg.Articles))
			for _, art := range pg.Articles {
				if art.Order == 0 {
					art.Order = orderAt(idx, album.ArticleCount)
				}
				idx++
				all = append(all, scanPick{art: art, album: album})
				pageKeys = append(pageKeys, artKey(art))
			}
			// 逐页落盘"已见"：中途失败也不会把更早的文章当成新文章重下。
			s.mu.Lock()
			if live := s.findSub(id); live != nil {
				live.Seen = capSeen(append(pageKeys, live.Seen...))
				s.dirty = true
			}
			s.mu.Unlock()
			if !pg.Continue {
				break
			}
		}

		if album.ArticleCount == 0 && len(all) > 0 {
			// 没拿到合集元数据时，用实际枚举到的总数补出 <collection_order>。
			album.ArticleCount = len(all)
			for i, c := range all {
				if c.art.Order == 0 {
					c.art.Order = len(all) - i
				}
			}
		}

		cut := -1
		for i, c := range all {
			if artKey(c.art) == sinceKey {
				cut = i
				foundStart = true
				if sinceTitle == "" {
					sinceTitle = c.art.Title
				}
				if sinceTime == 0 {
					sinceTime = c.art.PublishTime
				}
				break
			}
		}
		if cut < 0 && sinceOrder > 0 {
			// 起点原文被删/换页：用合集位置兜底。
			for i, c := range all {
				if c.art.Order > 0 && c.art.Order <= sinceOrder {
					cut = i
					break
				}
			}
		}
		if cut >= 0 {
			for i := 0; i <= cut; i++ {
				picks = append(picks, all[i])
			}
		} else if sinceTime > 0 {
			// 最后兜底：按发布时间取"起点之后"的连续段。
			for _, c := range all {
				if c.art.PublishTime > 0 && c.art.PublishTime >= sinceTime {
					picks = append(picks, c)
				}
			}
		}
	}
	if !foundStart && sinceTime == 0 && sinceOrder == 0 {
		return nil, fmt.Errorf("起点文章已不在这份文章列表里，无法确定下载范围，请重新选择起点")
	}
	if len(picks) == 0 {
		return nil, fmt.Errorf("起点之后没有可下载的文章")
	}

	// 从旧到新下载，文件名里的序号更自然。
	sort.SliceStable(picks, func(i, j int) bool { return picks[i].art.PublishTime < picks[j].art.PublishTime })
	queued, err := s.queueDownloads(id, tpl, dir, picks)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(picks))
	for _, c := range picks {
		keys = append(keys, artKey(c.art))
	}
	msg := fmt.Sprintf("从起点《%s》补下 %d 篇，已建 %d 个下载任务", sinceTitle, len(picks), queued)
	if !foundStart {
		msg += "（起点原文已不在合集里，按发布时间范围补下）"
	}
	s.mu.Lock()
	if live := s.findSub(id); live != nil {
		live.Seen = capSeen(append(keys, live.Seen...))
		live.SinceTitle = sinceTitle
		live.SinceTime = sinceTime
		if album != nil {
			live.SinceAlbum = album.ID
		}
		live.SinceCount = len(picks)
		live.LastNew = len(picks)
		live.LastMessage = msg
		live.LastCheck = time.Now().Unix()
		s.dirty = true
	}
	s.mu.Unlock()
	s.saveNow()
	return map[string]any{"new": len(picks), "queued": queued, "message": msg, "found_start": foundStart}, nil
}

func (s *Subscriber) scan(sub *Sub) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	s.mu.Lock()
	seen := toSet(sub.Seen)
	sinceKey := sub.Since
	sinceTime := sub.SinceTime
	baseline := len(sub.Seen) == 0 && sinceKey == ""
	maxNew := s.data.MaxNew
	albums := append([]*wx.AlbumRef(nil), sub.Albums...)
	biz := sub.Biz
	subID := sub.ID
	backfill := sub.Backfill
	kind := sub.Kind
	target := sub.Target
	name := sub.Name
	s.mu.Unlock()

	if kind == "album" && len(albums) == 0 && biz != "" {
		albums = []*wx.AlbumRef{{ID: wx.AlbumIDFromURL(target), Title: name}}
	}

	var found []scanPick
	var scanErr error
	if kind == "author" {
		feedPages := maxAuthorPages
		if baseline && backfill > 0 {
			feedPages = maxAlbumPages
		}
		scanErr = s.walkFeed(ctx, biz, feedPages, func(arts []*wx.FeedArticle) bool {
			fresh := 0
			for _, a := range arts {
				art := feedToCollectionArticle(a)
				if art == nil || seen[artKey(art)] {
					continue
				}
				if !sinceMatches(artKey(art), art, sinceKey, sinceTime) {
					continue
				}
				fresh++
				found = append(found, scanPick{art: art})
			}
			return fresh == 0
		})
	}
	for _, album := range albums {
		if kind == "author" {
			break
		}
		if album == nil || album.ID == "" {
			continue
		}
		s.ensureAlbumCount(ctx, subID, biz, album)
		cur := &albumCursor{biz: biz, album: album}
		pageBudget := maxScanPages
		if baseline && backfill > 0 {
			// 「首次补下」要覆盖整册，不能只看最新 60 篇。
			pageBudget = albumPages(album.ArticleCount, maxAlbumPages)
		}
		idx := 0
		for page := 0; page < pageBudget; page++ {
			pg, err := s.nextAlbumPage(ctx, cur, 20)
			if err != nil {
				scanErr = err
				break
			}
			if len(pg.Articles) == 0 {
				break
			}
			fresh := 0
			for _, art := range pg.Articles {
				if art.Order == 0 {
					art.Order = orderAt(idx, album.ArticleCount)
				}
				idx++
				if seen[artKey(art)] {
					continue
				}
				if kind == "author" && !sinceMatches(artKey(art), art, sinceKey, sinceTime) {
					continue
				}
				fresh++
				found = append(found, scanPick{art: art, album: album})
			}
			if !pg.Continue || cur.stalled {
				break
			}
			if fresh == 0 && !cur.ascending {
				// 稳态：最新的一页都见过了，不必继续翻。
				break
			}
		}
	}

	// Oldest first so album order stays natural and "最多 N 篇" keeps the newest.
	sort.SliceStable(found, func(i, j int) bool { return found[i].art.PublishTime < found[j].art.PublishTime })

	if baseline {
		limit := backfill
		picked := found
		if len(picked) > limit {
			picked = picked[len(picked)-limit:]
		}
		keys := make([]string, 0, len(found))
		for _, c := range found {
			keys = append(keys, artKey(c.art))
		}
		s.mu.Lock()
		if live := s.findSub(subID); live != nil {
			live.Seen = capSeen(keys)
		}
		s.mu.Unlock()
		if len(picked) == 0 {
			if scanErr != nil {
				msg := "扫描失败：" + scanErr.Error()
				s.setMessage(subID, msg, 0)
				return map[string]any{"new": 0, "queued": 0, "message": msg}, scanErr
			}
			msg := fmt.Sprintf("已建立基线（%d 篇），之后只下载新文章", len(found))
			total := 0
			for _, a := range albums {
				if a != nil {
					total += a.ArticleCount
				}
			}
			if total > len(found) {
				msg = fmt.Sprintf("已建立基线（合集共 %d 篇，本次标记最新 %d 篇为已见），之后只下载新文章；要补下旧文章请点「选择起点」", total, len(found))
			}
			s.setMessage(subID, msg, 0)
			return map[string]any{"new": 0, "queued": 0, "message": msg}, nil
		}
		queued, err := s.queueDownloads(subID, s.templateOf(sub), s.dirOf(sub), picked)
		msg := fmt.Sprintf("首次补下最近 %d 篇，已建 %d 个任务", len(picked), queued)
		s.setMessage(subID, msg, len(picked))
		if err != nil {
			return nil, err
		}
		return map[string]any{"new": len(picked), "queued": queued, "message": msg}, scanErr
	}

	picked := found
	left := 0
	if len(picked) > maxNew {
		left = len(picked) - maxNew
		picked = picked[len(picked)-maxNew:]
	}
	if len(picked) == 0 {
		msg := "没有新文章"
		if scanErr != nil {
			msg = "扫描失败：" + scanErr.Error()
		}
		s.setMessage(subID, msg, 0)
		return map[string]any{"new": 0, "queued": 0, "message": msg}, scanErr
	}
	queued, err := s.queueDownloads(subID, s.templateOf(sub), s.dirOf(sub), picked)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	if live := s.findSub(subID); live != nil {
		keys := make([]string, 0, len(picked))
		for _, c := range picked {
			keys = append(keys, artKey(c.art))
		}
		live.Seen = capSeen(append(keys, live.Seen...))
	}
	s.mu.Unlock()

	msg := fmt.Sprintf("新增 %d 篇文章，已建 %d 个任务", len(picked), queued)
	if left > 0 {
		msg += fmt.Sprintf("（还有 %d 篇下一轮继续）", left)
	}
	if sinceKey != "" {
		msg += fmt.Sprintf("；起点《%s》之后只补它及更新的文章", sub.SinceTitle)
	}
	if scanErr != nil {
		msg += "；部分合集扫描失败：" + scanErr.Error()
	}
	s.setMessage(subID, msg, len(picked))
	return map[string]any{"new": len(picked), "queued": queued, "message": msg}, nil
}

// queueDownloads builds the job for the picked entries.
func (s *Subscriber) queueDownloads(subID, tpl, dir string, picked []scanPick) (int, error) {
	tasks := make([]JobTask, 0, len(picked))
	for _, c := range picked {
		if c.art == nil {
			continue
		}
		title := ""
		if c.album != nil {
			title = c.album.Title
		}
		tasks = append(tasks, JobTask{
			URL:                c.art.URL,
			OutDir:             dir,
			NameTemplate:       tpl,
			CollectionTemplate: tpl,
			CollectionID:       albumID(c.album),
			CollectionTitle:    title,
			CollectionOrder:    c.art.Order,
		})
	}
	if len(tasks) == 0 {
		return 0, nil
	}
	// A single job accepts at most maxJobTasks links, so a big catch-up scan is
	// split into several jobs instead of being rejected.
	const maxJobTasks = 30
	queued := 0
	name := s.subName(subID)
	for start := 0; start < len(tasks); start += maxJobTasks {
		end := start + maxJobTasks
		if end > len(tasks) {
			end = len(tasks)
		}
		req := DownloadRequest{
			Tasks:      tasks[start:end],
			OutDir:     dir,
			Jobs:       4,
			Source:     "sub",
			SourceName: name,
			SubID:      subID,
		}
		if _, err := s.mgr.StartSubJob(req, func(task JobTask, media *wx.Media, r *wx.Result) {
			s.recordFile(subID, task, media, r)
		}); err != nil {
			return queued, err
		}
		queued += end - start
	}
	return queued, nil
}

func albumID(a *wx.AlbumRef) string {
	if a == nil {
		return ""
	}
	return a.ID
}

func capSeen(list []string) []string {
	dedup := make([]string, 0, len(list))
	seen := map[string]bool{}
	for _, v := range list {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		dedup = append(dedup, v)
	}
	if len(dedup) > maxSubSeen {
		dedup = dedup[:maxSubSeen]
	}
	if dedup == nil {
		dedup = []string{}
	}
	return dedup
}

func (s *Subscriber) subName(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sub := s.findSub(id); sub != nil {
		return sub.Name
	}
	return ""
}

func (s *Subscriber) setMessage(id, msg string, newCount int) {
	s.mu.Lock()
	if sub := s.findSub(id); sub != nil {
		sub.LastMessage = msg
		sub.LastNew = newCount
	}
	s.dirty = true
	s.mu.Unlock()
}

// ---------- misc helpers ----------

func str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func firstStr(vals ...any) string {
	for _, v := range vals {
		if s := strings.TrimSpace(str(v)); s != "" {
			return s
		}
	}
	return ""
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func shortBiz(biz string) string {
	if biz == "" {
		return "未知"
	}
	if len(biz) <= 10 {
		return biz
	}
	return biz[:10] + "…"
}

func boolVal(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		return t == "1" || strings.EqualFold(t, "true") || t == "on"
	}
	return false
}

func intVal(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		var n int
		_, _ = fmt.Sscanf(strings.TrimSpace(t), "%d", &n)
		return n
	}
	return 0
}
