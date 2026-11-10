package webui

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"wxmp/internal/wx"
)

func newTestSubscriber(t *testing.T) *Subscriber {
	t.Helper()
	dir := t.TempDir()
	mgr := NewManager(nil, dir, wx.DefaultNameTemplate, wx.DefaultCollectionTemplate, dir)
	t.Cleanup(mgr.Close)
	s := NewSubscriber(nil, mgr, dir, dir, wx.DefaultNameTemplate, wx.DefaultCollectionTemplate)
	t.Cleanup(s.Close)
	return s
}

func TestSubscriberRelayoutRenamesAndSkipsMissing(t *testing.T) {
	s := newTestSubscriber(t)
	root := t.TempDir()
	mk := func(name, body string) string {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a := mk("old/a.jpg", "a")
	b := mk("old/b.jpg", "b")
	sub := &Sub{
		ID: "s1", Kind: "album", Enabled: true,
		Files: []*SubFile{
			{Path: a, Root: root, Vars: map[string]string{"title": "标题A", "post_id": "p1", "order": "1", "order2": "01", "ext": "jpg"}},
			{Path: b, Root: root, Vars: map[string]string{"title": "标题B", "post_id": "p2", "order": "2", "order2": "02", "ext": "jpg"}},
		},
	}
	// Delete one file to simulate "作品已消失".
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}

	moved, missing := s.relayoutLocked(sub, "<title>/<order2>-<post_id>.<ext>")
	if moved != 1 || missing != 1 {
		t.Fatalf("moved=%d missing=%d, want 1/1", moved, missing)
	}
	want := filepath.Join(root, "标题A", "01-p1.jpg")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
	if sub.Files[0].Path != want {
		t.Fatalf("record path = %q, want %q", sub.Files[0].Path, want)
	}

	// Rendering the same template again must be a no-op.
	moved, missing = s.relayoutLocked(sub, "<title>/<order2>-<post_id>.<ext>")
	if moved != 0 || missing != 1 {
		t.Fatalf("idempotent relayout: moved=%d missing=%d", moved, missing)
	}
}

func TestSubscriberUpsertKeepsTemplateAndRenames(t *testing.T) {
	s := newTestSubscriber(t)
	root := t.TempDir()
	file := filepath.Join(root, "旧.jpg")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Offline create: an album subscription identified by biz + album id.
	view, _, err := s.Upsert(map[string]any{
		"kind":     "album",
		"biz":      "MzkxMjM0NTY3OA==",
		"albums":   []any{map[string]any{"id": "1234567890", "title": "合集"}},
		"name":     "合集",
		"template": "<collection_title>/<post_id>-<order>.<ext>",
		"enabled":  false,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if view.Kind != "album" || view.Target == "" {
		t.Fatalf("bad view: %#v", view)
	}
	sub := s.findSub(view.ID)
	sub.Files = []*SubFile{{Path: file, Root: root, Vars: map[string]string{
		"collection_title": "合集", "post_id": "p9", "order": "4", "order2": "04", "ext": "jpg",
	}}}
	got, extra, err := s.Upsert(map[string]any{
		"id":       view.ID,
		"template": "<collection_title>/<order2>-<post_id>.<ext>",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.Template != "<collection_title>/<order2>-<post_id>.<ext>" {
		t.Fatalf("template = %q", got.Template)
	}
	if extra["renamed"] != 1 {
		t.Fatalf("renamed = %v, want 1", extra["renamed"])
	}
	if _, err := os.Stat(filepath.Join(root, "合集", "04-p9.jpg")); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}

	// A bad template must be rejected and leave the subscription untouched.
	if _, _, err := s.Upsert(map[string]any{"id": view.ID, "template": "<nope>"}); err == nil {
		t.Fatal("invalid template accepted")
	}
}

func TestSubscriberAccountNeedsAlbums(t *testing.T) {
	s := newTestSubscriber(t)
	if _, _, err := s.Upsert(map[string]any{"kind": "account", "biz": "MzkxMjM0NTY3OA==", "name": "公众号", "enabled": false}); err == nil {
		t.Fatal("account without albums must be rejected")
	}
	view, _, err := s.Upsert(map[string]any{
		"kind":    "account",
		"biz":     "MzkxMjM0NTY3OA==",
		"name":    "公众号",
		"albums":  []any{"1234567890"},
		"enabled": false,
	})
	if err != nil {
		t.Fatalf("account upsert: %v", err)
	}
	if len(view.Albums) != 1 || view.Target != "MzkxMjM0NTY3OA==" {
		t.Fatalf("bad account view: %#v", view)
	}
}

func TestSubscriberBatchAndDelete(t *testing.T) {
	s := newTestSubscriber(t)
	v1, _, _ := s.Upsert(map[string]any{"kind": "album", "biz": "b", "albums": []any{"111111"}, "enabled": false})
	v2, _, _ := s.Upsert(map[string]any{"kind": "album", "biz": "b", "albums": []any{"222222"}, "enabled": false})
	if n := len(s.Snapshot()["subs"].([]*SubView)); n != 2 {
		t.Fatalf("subs = %d", n)
	}
	if res, err := s.Batch("enable", []string{v1.ID}); err != nil || res["count"] != 1 {
		t.Fatalf("batch enable: %v %v", res, err)
	}
	if !s.findSub(v1.ID).Enabled {
		t.Fatal("subscription not enabled")
	}
	if n := s.Delete([]string{v2.ID}); n != 1 {
		t.Fatalf("delete = %d", n)
	}
	if s.findSub(v2.ID) != nil {
		t.Fatal("subscription still present")
	}
}

func TestManagerTaskOverridesAndValidation(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(nil, dir, wx.DefaultNameTemplate, wx.DefaultCollectionTemplate, dir)
	defer m.Close()
	m.Close() // stopped worker: nothing is fetched without a client
	m.PauseAll()

	out := filepath.Join(dir, "custom")
	view, err := m.Start(DownloadRequest{
		Tasks: []JobTask{
			{URL: "https://mp.weixin.qq.com/s/abcdefghij", OutDir: out, NameTemplate: "<post_id>/<order>.<ext>"},
			{URL: "https://mp.weixin.qq.com/mp/appmsgalbum?__biz=biz&action=getalbum&album_id=123"},
		},
		OutDir: dir,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if view.Status != "paused" {
		t.Fatalf("status = %q, want paused while the queue is paused", view.Status)
	}
	if len(view.Tasks) != 2 {
		t.Fatalf("tasks = %d", len(view.Tasks))
	}
	if view.Tasks[0].OutDir != out {
		t.Fatalf("per-task dir = %q, want %q", view.Tasks[0].OutDir, out)
	}
	if view.Tasks[0].NameTemplate != "<post_id>/<order>.<ext>" {
		t.Fatalf("per-task template = %q", view.Tasks[0].NameTemplate)
	}
	if view.Tasks[1].OutDir != dir {
		t.Fatalf("fallback dir = %q, want %q", view.Tasks[1].OutDir, dir)
	}
	if !strings.HasPrefix(view.Tasks[1].URL, "https://mp.weixin.qq.com/mp/appmsgalbum") {
		t.Fatalf("album url not normalized: %q", view.Tasks[1].URL)
	}

	// Invalid per-task template is rejected.
	if _, err := m.Start(DownloadRequest{Tasks: []JobTask{{URL: "https://mp.weixin.qq.com/s/abcdefghij", NameTemplate: "<bogus>"}}}); err == nil {
		t.Fatal("invalid template accepted")
	}
}

// newQuietManager builds a manager whose worker is stopped, so queue bookkeeping
// can be tested without a network client.
func newQuietManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	m := NewManager(nil, dir, wx.DefaultNameTemplate, wx.DefaultCollectionTemplate, dir)
	m.Close()
	t.Cleanup(m.Close)
	return m
}

func TestManagerQueueControls(t *testing.T) {
	m := newQuietManager(t)
	m.PauseAll()
	j1, err := m.Start(DownloadRequest{Tasks: []JobTask{{URL: "https://mp.weixin.qq.com/s/aaaaaaa1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if j1.Status != "paused" {
		t.Fatalf("new job while paused = %q", j1.Status)
	}
	if q := m.QueueState(); q["paused"] != 1 || q["paused_all"] != true {
		t.Fatalf("queue state = %#v", q)
	}

	id := j1.ID
	if _, ok := m.Resume(id); !ok {
		t.Fatal("resume failed")
	}
	if v, _ := m.Get(id); v.Status != "queued" {
		t.Fatalf("resumed status = %q", v.Status)
	}
	// Put it back so nothing runs without a network client.
	m.Pause(id)

	retry, err := m.Retry(id)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if retry.ID == id || retry.Status != "paused" {
		t.Fatalf("retry job = %#v", retry)
	}

	if n := m.RetryFailed(); n != 0 {
		t.Fatalf("RetryFailed = %d, want 0 (no failed jobs yet)", n)
	}
	if n := m.ClearCompleted(); n != 0 {
		t.Fatalf("ClearCompleted = %d, want 0", n)
	}

	// Batch pause / delete.
	if res, err := m.Batch("pause", []string{retry.ID}); err != nil || res["count"] != 1 {
		t.Fatalf("batch pause: %v %v", res, err)
	}
	if res, err := m.Batch("delete", []string{retry.ID}); err != nil || res["count"] != 1 {
		t.Fatalf("batch delete: %v %v", res, err)
	}
	if _, ok := m.Get(retry.ID); ok {
		t.Fatal("deleted job still visible")
	}
	if n := m.Delete([]string{id}); n != 1 {
		t.Fatalf("delete = %d", n)
	}
}

func TestManagerKeepsFailedJobsWhenClearingCompleted(t *testing.T) {
	m := newQuietManager(t)
	m.remember(&JobView{ID: "done-1", Status: "done", URLs: []string{"u1"}, StartedAt: 1})
	m.remember(&JobView{ID: "fail-1", Status: "failed", URLs: []string{"u2"}, StartedAt: 2})
	if n := m.ClearCompleted(); n != 1 {
		t.Fatalf("ClearCompleted = %d, want 1 (failed job kept)", n)
	}
	if _, ok := m.Get("done-1"); ok {
		t.Fatal("done job not removed")
	}
	if _, ok := m.Get("fail-1"); !ok {
		t.Fatal("failed job should stay for retry")
	}
}

func TestAscendingPageDetectsReversedAlbums(t *testing.T) {
	newestFirst := []*wx.CollectionArticle{{PublishTime: 300}, {PublishTime: 200}, {PublishTime: 100}}
	if ascendingPage(newestFirst) {
		t.Fatal("newest-first page misdetected as reversed")
	}
	oldestFirst := []*wx.CollectionArticle{{PublishTime: 100}, {PublishTime: 200}, {PublishTime: 300}}
	if !ascendingPage(oldestFirst) {
		t.Fatal("oldest-first page not detected")
	}
	if ascendingPage(nil) || ascendingPage([]*wx.CollectionArticle{{PublishTime: 1}}) {
		t.Fatal("short pages must never be treated as reversed")
	}
	if ascendingPage([]*wx.CollectionArticle{{PublishTime: 0}, {PublishTime: 300}}) {
		t.Fatal("missing timestamps must not be treated as reversed")
	}
}

func TestOrderAtFollowsCollectionPosition(t *testing.T) {
	if got := orderAt(0, 37); got != 37 {
		t.Fatalf("newest order = %d, want 37", got)
	}
	if got := orderAt(36, 37); got != 1 {
		t.Fatalf("oldest order = %d, want 1", got)
	}
	if got := orderAt(0, 0); got != 0 {
		t.Fatalf("unknown count = %d, want 0", got)
	}
	if got := orderAt(99, 3); got != 1 {
		t.Fatalf("clamped order = %d, want 1", got)
	}
}

func TestSinceMatchesRange(t *testing.T) {
	start := &wx.CollectionArticle{MsgID: "200", ItemIdx: "1", PublishTime: 2000}
	newer := &wx.CollectionArticle{MsgID: "300", ItemIdx: "1", PublishTime: 3000}
	older := &wx.CollectionArticle{MsgID: "100", ItemIdx: "1", PublishTime: 1000}
	sameTime := &wx.CollectionArticle{MsgID: "201", ItemIdx: "1", PublishTime: 2000}

	if !sinceMatches(artKey(start), start, "200_1", 2000) {
		t.Fatal("start itself must be included")
	}
	if !sinceMatches(artKey(newer), newer, "200_1", 2000) {
		t.Fatal("newer article must be included")
	}
	if sinceMatches(artKey(older), older, "200_1", 2000) {
		t.Fatal("older article must be excluded")
	}
	if !sinceMatches(artKey(sameTime), sameTime, "200_1", 2000) {
		t.Fatal("same timestamp must be included")
	}
	if !sinceMatches("k", older, "", 0) {
		t.Fatal("no start point means everything matches")
	}
	if sinceMatches("other", &wx.CollectionArticle{PublishTime: 0}, "k", 2000) {
		t.Fatal("article without timestamp must not match a time-bounded range")
	}
}

// feedRT serves the account-feed endpoint with a mutable article list.
type feedRT struct {
	mu   sync.Mutex
	arts []map[string]any
}

func (f *feedRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if !strings.Contains(req.URL.Path, "profile_ext") {
		return &http.Response{
			StatusCode: 404, Body: io.NopCloser(strings.NewReader("not found")),
			Header: http.Header{}, Request: req,
		}, nil
	}
	f.mu.Lock()
	items := make([]string, 0, len(f.arts))
	for _, a := range f.arts {
		items = append(items, fmt.Sprintf(
			`{"comm_msg_info":{"datetime":%v},"app_msg_ext_info":{"title":%q,"content_url":%q,"cover":""}}`,
			a["time"], a["title"],
			fmt.Sprintf("http://mp.weixin.qq.com/s?__biz=MzA=&mid=%s&idx=%s&sn=x", a["mid"], a["idx"]),
		))
	}
	f.mu.Unlock()
	inner := `{"list":[` + strings.Join(items, ",") + `]}`
	body := `{"ret":0,"errmsg":"ok","can_msg_continue":0,"general_msg_list":` + strconv.Quote(inner) + `}`
	return &http.Response{
		StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)),
		Header: http.Header{"Content-Type": {"application/json"}}, Request: req,
	}, nil
}

func (f *feedRT) prepend(art map[string]any) {
	f.mu.Lock()
	f.arts = append([]map[string]any{art}, f.arts...)
	f.mu.Unlock()
}

func TestSubscriberAuthorScanBaselineThenNew(t *testing.T) {
	rt := &feedRT{arts: []map[string]any{
		{"mid": "101", "idx": "1", "title": "旧二", "time": 2000},
		{"mid": "100", "idx": "1", "title": "旧一", "time": 1000},
	}}
	client := wx.NewClient()
	client.HTTP = &http.Client{Transport: rt}
	client.SetCookie("uin=o1; key=k")

	dir := t.TempDir()
	mgr := NewManager(nil, dir, wx.DefaultNameTemplate, wx.DefaultCollectionTemplate, dir)
	mgr.Close()
	s := NewSubscriber(client, mgr, dir, dir, wx.DefaultNameTemplate, wx.DefaultCollectionTemplate)
	t.Cleanup(s.Close)

	sub := &Sub{ID: "a1", Kind: "author", Biz: "Mzkz==", Name: "号", Enabled: true, Created: time.Now().Unix()}
	s.mu.Lock()
	s.data.Subs = append(s.data.Subs, sub)
	s.mu.Unlock()

	// 第一次扫描：只建立基线，不下载。
	if _, err := s.CheckNow("a1"); err != nil {
		t.Fatalf("baseline scan: %v", err)
	}
	if got := len(s.findSub("a1").Seen); got != 2 {
		t.Fatalf("baseline seen = %d, want 2", got)
	}
	if jobs := mgr.List(); len(jobs) != 0 {
		t.Fatalf("baseline must not queue downloads, got %d", len(jobs))
	}

	// 公众号发了一篇新文章：扫描应发现并建任务。
	rt.prepend(map[string]any{"mid": "102", "idx": "1", "title": "新文章", "time": 3000})
	res, err := s.CheckNow("a1")
	if err != nil {
		t.Fatalf("incremental scan: %v", err)
	}
	if res["result"].(map[string]any)["new"] != 1 {
		t.Fatalf("result = %#v", res["result"])
	}
	if !toSet(s.findSub("a1").Seen)["102_1"] {
		t.Fatalf("seen = %v, want 102_1", s.findSub("a1").Seen)
	}
	jobs := mgr.List()
	if len(jobs) != 1 || jobs[0].Source != "sub" || len(jobs[0].URLs) != 1 {
		t.Fatalf("jobs = %#v", jobs)
	}
	if !strings.Contains(jobs[0].URLs[0], "mid=102") {
		t.Fatalf("job url = %q", jobs[0].URLs[0])
	}

	// 再扫一次：已经追过，不应重复建任务。
	if _, err := s.CheckNow("a1"); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if jobs := mgr.List(); len(jobs) != 1 {
		t.Fatalf("duplicate job created, jobs = %d", len(jobs))
	}
}

func TestSubscriberAuthorNeedsCookie(t *testing.T) {
	client := wx.NewClient() // 没有 Cookie
	dir := t.TempDir()
	mgr := NewManager(nil, dir, wx.DefaultNameTemplate, wx.DefaultCollectionTemplate, dir)
	mgr.Close()
	s := NewSubscriber(client, mgr, dir, dir, wx.DefaultNameTemplate, wx.DefaultCollectionTemplate)
	t.Cleanup(s.Close)
	sub := &Sub{ID: "a2", Kind: "author", Biz: "Mzkz==", Name: "号", Enabled: true, Created: time.Now().Unix()}
	s.mu.Lock()
	s.data.Subs = append(s.data.Subs, sub)
	s.mu.Unlock()

	if _, err := s.CheckNow("a2"); err == nil {
		t.Fatal("author scan without cookie must fail")
	}
	if got := s.findSub("a2").LastError; !strings.Contains(got, "Cookie") {
		t.Fatalf("last_error = %q", got)
	}
}

// albumRT serves the album paging endpoint with a fixed article list.
type albumRT struct {
	arts []map[string]any // newest first
}

func (a *albumRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if !strings.Contains(req.URL.Path, "appmsgalbum") || req.URL.Query().Get("action") != "getalbum" {
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("<html>")), Header: http.Header{}, Request: req}, nil
	}
	begin := req.URL.Query().Get("begin_msgid")
	start := 0
	if begin != "" {
		for i, it := range a.arts {
			if fmt.Sprint(it["mid"]) == begin {
				start = i + 1
				break
			}
		}
	}
	end := start + 20
	if end > len(a.arts) {
		end = len(a.arts)
	}
	items := make([]string, 0, end-start)
	for _, it := range a.arts[start:end] {
		items = append(items, fmt.Sprintf(
			`{"msgid":%q,"itemidx":"1","title":%q,"create_time":%q,"url":%q,"cover_img_1_1":""}`,
			it["mid"], it["title"], it["time"],
			fmt.Sprintf("http://mp.weixin.qq.com/s?__biz=MzA=&mid=%s&idx=1&sn=x", it["mid"]),
		))
	}
	cont := 1
	if end >= len(a.arts) {
		cont = 0
	}
	body := fmt.Sprintf(`{"base_resp":{"ret":0},"getalbum_resp":{"article_list":[%s],"continue_flag":%d}}`,
		strings.Join(items, ","), cont)
	return &http.Response{
		StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)),
		Header: http.Header{"Content-Type": {"application/json"}}, Request: req,
	}, nil
}

func newAlbumSubscriber(t *testing.T, n int) (*Subscriber, *Manager, *Sub) {
	t.Helper()
	rt := &albumRT{}
	for i := 0; i < n; i++ {
		rt.arts = append(rt.arts, map[string]any{
			"mid": fmt.Sprintf("%d", 300+i), "title": fmt.Sprintf("文章%d", n-i),
			"time": fmt.Sprintf("%d", 900000+n-i),
		})
	}
	client := wx.NewClient()
	client.HTTP = &http.Client{Transport: rt}
	dir := t.TempDir()
	mgr := NewManager(nil, dir, wx.DefaultNameTemplate, wx.DefaultCollectionTemplate, dir)
	mgr.Close()
	s := NewSubscriber(client, mgr, dir, dir, wx.DefaultNameTemplate, wx.DefaultCollectionTemplate)
	t.Cleanup(s.Close)
	sub := &Sub{
		ID: "album1", Kind: "album", Biz: "Mzkz==", Name: "大合集", Enabled: true,
		Albums: []*wx.AlbumRef{{ID: "318", Title: "大合集", Biz: "Mzkz=="}}, Created: time.Now().Unix(),
	}
	s.mu.Lock()
	s.data.Subs = append(s.data.Subs, sub)
	s.mu.Unlock()
	return s, mgr, sub
}

func TestListArticlesReturnsWholeAlbum(t *testing.T) {
	s, _, _ := newAlbumSubscriber(t, 108)
	res, err := s.ListArticles("album1", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	arts := res["articles"].([]map[string]any)
	if len(arts) != 108 {
		t.Fatalf("listed %d articles, want all 108", len(arts))
	}
	if arts[0]["order"] != 108 || arts[len(arts)-1]["order"] != 1 {
		t.Fatalf("orders = %v .. %v", arts[0]["order"], arts[len(arts)-1]["order"])
	}
}

func TestBackfillFromOldestCoversWholeAlbum(t *testing.T) {
	const n = 108
	s, mgr, sub := newAlbumSubscriber(t, n)
	// 起点 = 最老的一篇（order 1）
	oldest := &wx.CollectionArticle{MsgID: "408", ItemIdx: "1", Order: 1, PublishTime: 900000}
	s.mu.Lock()
	sub.Since = artKey(oldest)
	sub.SinceOrder = 1
	sub.SinceTime = oldest.PublishTime
	sub.SinceAlbum = "318"
	s.mu.Unlock()

	res, err := s.Backfill("album1")
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if res["new"] != n {
		t.Fatalf("backfill picked %v articles, want %d", res["new"], n)
	}
	if got := len(s.findSub("album1").Seen); got != n {
		t.Fatalf("seen = %d, want %d", got, n)
	}
	jobs := mgr.List()
	total := 0
	for _, j := range jobs {
		total += len(j.URLs)
	}
	if total != n {
		t.Fatalf("queued %d tasks across %d jobs, want %d", total, len(jobs), n)
	}
	if len(jobs) != 4 {
		t.Fatalf("jobs = %d, want 4 (30/30/30/18 chunks)", len(jobs))
	}
}

func TestAlbumPagesBudget(t *testing.T) {
	if got := albumPages(0, 3); got != 3 {
		t.Fatalf("fallback = %d", got)
	}
	if got := albumPages(108, 3); got != 6 {
		t.Fatalf("108 articles = %d pages, want 6", got)
	}
	if got := albumPages(100000, 3); got != 5000 {
		t.Fatalf("100k articles = %d pages, want 5000 (no per-article cap)", got)
	}
	if got := albumPages(100000000, 3); got != maxAlbumPages {
		t.Fatalf("absurd album = %d, want the loop guard %d", got, maxAlbumPages)
	}
}

func TestBaselineBackfillCoversWholeAlbum(t *testing.T) {
	const n = 108
	s, mgr, sub := newAlbumSubscriber(t, n)
	s.mu.Lock()
	sub.Backfill = n // 添加时选择「首次补下最近 108 篇」
	s.mu.Unlock()

	res, err := s.CheckNow("album1")
	if err != nil {
		t.Fatalf("baseline scan: %v", err)
	}
	result := res["result"].(map[string]any)
	if result["new"] != n {
		t.Fatalf("baseline backfill picked %v, want %d", result["new"], n)
	}
	total := 0
	for _, j := range mgr.List() {
		total += len(j.URLs)
	}
	if total != n {
		t.Fatalf("queued %d tasks, want %d", total, n)
	}
	if got := len(s.findSub("album1").Seen); got != n {
		t.Fatalf("seen = %d, want %d", got, n)
	}
}

func TestHugeAlbumHasNoArticleCap(t *testing.T) {
	const n = 2500
	s, mgr, sub := newAlbumSubscriber(t, n)
	res, err := s.ListArticles("album1", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := len(res["articles"].([]map[string]any)); got != n {
		t.Fatalf("listed %d, want %d (no per-article cap)", got, n)
	}

	s.mu.Lock()
	oldest := &wx.CollectionArticle{MsgID: fmt.Sprintf("%d", 300+n-1), ItemIdx: "1", Order: 1}
	sub.Since = artKey(oldest)
	sub.SinceOrder = 1
	sub.SinceAlbum = "318"
	s.mu.Unlock()

	back, err := s.Backfill("album1")
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if back["new"] != n {
		t.Fatalf("backfill picked %v, want %d", back["new"], n)
	}
	total := 0
	for _, j := range mgr.List() {
		total += len(j.URLs)
	}
	if total != n {
		t.Fatalf("queued %d tasks, want %d", total, n)
	}
}
