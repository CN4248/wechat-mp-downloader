package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"wxmp/internal/wx"
)

func pickTestRequest(dir string) DownloadRequest {
	return DownloadRequest{
		NameTemplate: "<title>-<post_id>-<order>.<ext>",
		OutDir:       dir,
		Pick: &MediaPick{
			PageURL: "https://mp.weixin.qq.com/s/fixtureslug123",
			Picked: []PickItem{
				{
					URL:    "https://mmbiz.qpic.cn/mmbiz_png/hashA/0?wx_fmt=png",
					Ext:    "png",
					Kind:   "image",
					Width:  640,
					Height: 480,
				},
			},
		},
	}
}

func TestPickRequestValidation(t *testing.T) {
	m := newQuietManager(t)
	m.PauseAll()
	dir := t.TempDir()

	cases := []struct {
		name string
		req  DownloadRequest
		want string
	}{
		{"empty picks", DownloadRequest{Pick: &MediaPick{PageURL: "https://mp.weixin.qq.com/s/abcdefghij"}}, "没有选中的媒体"},
		{"album page", DownloadRequest{Pick: &MediaPick{
			PageURL: "https://mp.weixin.qq.com/mp/appmsgalbum?__biz=biz&action=getalbum&album_id=123",
			Picked:  []PickItem{{URL: "https://mmbiz.qpic.cn/a/0?wx_fmt=png", Ext: "png"}},
		}}, "只支持文章页"},
		{"foreign host", DownloadRequest{Pick: &MediaPick{
			PageURL: "https://mp.weixin.qq.com/s/abcdefghij",
			Picked:  []PickItem{{URL: "http://169.254.169.254/latest/meta-data", Ext: "png"}},
		}}, "不是微信图床"},
		{"bad ext", DownloadRequest{Pick: &MediaPick{
			PageURL: "https://mp.weixin.qq.com/s/abcdefghij",
			Picked:  []PickItem{{URL: "https://mmbiz.qpic.cn/a/0?wx_fmt=png", Ext: "exe"}},
		}}, "媒体类型不支持"},
		{"bad template", DownloadRequest{NameTemplate: "<bogus>", OutDir: dir, Pick: &MediaPick{
			PageURL: "https://mp.weixin.qq.com/s/abcdefghij",
			Picked:  []PickItem{{URL: "https://mmbiz.qpic.cn/a/0?wx_fmt=png", Ext: "png"}},
		}}, "未知变量"},
	}
	for _, tc := range cases {
		if _, err := m.Start(tc.req); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want containing %q", tc.name, err, tc.want)
		}
	}

	view, err := m.Start(pickTestRequest(dir))
	if err != nil {
		t.Fatalf("valid pick request rejected: %v", err)
	}
	if view.Total != 0 || view.Status == "" {
		t.Fatalf("unexpected job view: %#v", view)
	}
	if len(view.Tasks) != 1 || view.Tasks[0].URL != "https://mp.weixin.qq.com/s/fixtureslug123" {
		t.Fatalf("tasks = %#v", view.Tasks)
	}
	if view.Tasks[0].OutDir != dir || view.Tasks[0].NameTemplate != "<title>-<post_id>-<order>.<ext>" {
		t.Fatalf("task defaults = %#v", view.Tasks[0])
	}
}

func TestPickedMediaKeepsArticleNames(t *testing.T) {
	html, err := os.ReadFile("../../testdata/article-fixture.html")
	if err != nil {
		t.Fatal(err)
	}
	art := wx.ParseHTML(string(html), "https://mp.weixin.qq.com/s/fixtureslug123", wx.ParseOptions{})
	if len(art.Medias) != 3 {
		t.Fatalf("fixture medias = %d", len(art.Medias))
	}

	// The userscript serialises the query in a different order than Go, which
	// is exactly why the match uses the CDN key instead of raw string equality.
	userscriptURLs := map[int]string{
		0: "https://mmbiz.qpic.cn/mmbiz_png/hashA/0?wx_fmt=png&from=appmsg",
	}
	for _, idx := range []int{0, 1, 2} {
		src := art.Medias[idx]
		pushURL := src.URL
		if u, ok := userscriptURLs[idx]; ok {
			pushURL = u
		}
		medias, fallback := selectPickedMedias(art, []PickItem{{URL: pushURL, Ext: src.Ext, Kind: src.Kind}})
		if fallback != 0 || len(medias) != 1 {
			t.Fatalf("media %d: fallback=%d medias=%d", idx, fallback, len(medias))
		}
		want, err := wx.BuildRelPath(src, wx.DefaultNameTemplate)
		if err != nil {
			t.Fatal(err)
		}
		got, err := wx.BuildRelPath(medias[0], wx.DefaultNameTemplate)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("media %d: picked name %q != article name %q", idx, got, want)
		}
	}
}

func TestPickedMediaFallbackSeqAfterArticle(t *testing.T) {
	art := &wx.Article{
		Title:  "测试标题",
		PostID: "fixtureslug123",
		Medias: []*wx.Media{
			{Seq: 1, URL: "https://mmbiz.qpic.cn/mmbiz_png/aaaaaaaaaaaaaaaaaaaaaaaa/0?wx_fmt=png", Ext: "png", Kind: "image"},
			{Seq: 2, URL: "https://mmbiz.qpic.cn/mmbiz_gif/bbbbbbbbbbbbbbbbbbbbbbbb/0?wx_fmt=gif", Ext: "gif", Kind: "gif"},
		},
	}
	video := "https://mpvideo.qpic.cn/0b2eaaaaaaaaaaaaaaaaaaaa.mp4?dis_k=abc"
	medias, fallback := selectPickedMedias(art, []PickItem{
		{URL: "https://mmbiz.qpic.cn/mmbiz_gif/bbbbbbbbbbbbbbbbbbbbbbbb/0?wx_fmt=gif", Ext: "gif", Kind: "gif"},
		{URL: video, Ext: "mp4", Kind: "video"},
	})
	if fallback != 1 || len(medias) != 2 {
		t.Fatalf("fallback=%d medias=%d", fallback, len(medias))
	}
	if medias[0].Seq != 2 || medias[1].Seq != 3 || medias[1].Kind != "video" || medias[1].Ext != "mp4" {
		t.Fatalf("picked medias = %#v", medias)
	}
	got, err := wx.BuildRelPath(medias[1], "<title>-<post_id>-<order>.<ext>")
	if err != nil {
		t.Fatal(err)
	}
	if got != "测试标题-fixtureslug123-3.mp4" {
		t.Fatalf("fallback video name = %q", got)
	}
}

func TestHandleDownloadMediaRoute(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(nil, dir, wx.DefaultNameTemplate, wx.DefaultCollectionTemplate, dir)
	m.Close()
	m.PauseAll()
	s := &Server{mgr: m, cfg: Config{Version: "test"}}
	srv := httptest.NewServer(s.routes())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/download/media", "application/json", strings.NewReader(`{"pick":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty pick status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	body := `{"out_dir":"` + dir + `","name_template":"<post_id>-<order>.<ext>","pick":{"page_url":"https://mp.weixin.qq.com/s/abcdefghij","picked":[{"url":"https://mmbiz.qpic.cn/mmbiz_png/abcdefghijklmnopqrstuvwx/0?wx_fmt=png","ext":"png","kind":"image","width":100,"height":200}]}}`
	resp, err = http.Post(srv.URL+"/api/download/media", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("valid pick status = %d", resp.StatusCode)
	}
	var out struct {
		Job *JobView `json:"job"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Job == nil || len(out.Job.Tasks) != 1 || out.Job.Tasks[0].NameTemplate != "<post_id>-<order>.<ext>" {
		t.Fatalf("job = %#v", out.Job)
	}
}
