package wx

import (
	"strings"
	"testing"
)

const fixtureHTML = `<!DOCTYPE html><html><head>
<meta property="og:title" content="测试文章">
<script>
var msg_title = '测试文章'.html(false);
var nickname = htmlDecode("测试公众号");
var ct = "1792575000";
var msg_cdn_url = "https://mmbiz.qpic.cn/mmbiz_jpg/coverhash/0?wx_fmt=jpeg";
var appmsgid = "1234567890";
var biz = "Mzg3MTgyMTA4Nw==";
var sn = "abc123";
var mid = "1234567890";
var idx = "1";
</script></head><body>
<div class="rich_media_content" id="js_content">
<p>正文</p>
<img data-src="https://mmbiz.qpic.cn/sz_mmbiz_png/hashA/640?wx_fmt=png&amp;from=appmsg" data-type="png" data-w="1080" data-ratio="1" data-imgfileid="111">
<img data-src="https://mmbiz.qpic.cn/mmbiz_gif/hashB/640?wx_fmt=gif" data-type="gif" data-w="300" data-ratio="0.5" data-imgfileid="222">
<img data-src="https://mmbiz.qpic.cn/sz_mmbiz_jpg/hashC/640?wx_fmt=jpeg&amp;from=appmsg" data-type="jpeg" data-w="800" data-ratio="1.5" data-imgfileid="333">
<img data-src="https://mmbiz.qpic.cn/sz_mmbiz_png/hashA/640?wx_fmt=png&amp;from=appmsg" data-type="png" data-w="1080" data-ratio="1">
</div>
<script>
picture_page_info_list: [
  { cdn_url: 'https://mmbiz.qpic.cn/sz_mmbiz_png/hashA/640?wx_fmt=png\x26amp;amp;from=appmsg', width: '1080'*1, height: '1080'*1,
    watermark_info: { cdn_url: 'http://mmbiz.qpic.cn/mmbiz_png/watermarkhash/0?wx_fmt=png' } },
  { cdn_url: 'https://mmbiz.qpic.cn/sz_mmbiz_jpg/hashD/640?wx_fmt=jpeg\x26amp;amp;from=appmsg', width: '900'*1, height: '600'*1,
    watermark_info: { cdn_url: 'http://mmbiz.qpic.cn/mmbiz_jpg/watermarkhash2/0?wx_fmt=jpeg' } }
];
</script>
</body></html>`

func TestParseHTMLFixture(t *testing.T) {
	art := ParseHTML(fixtureHTML, "https://mp.weixin.qq.com/s/abcdefghij", ParseOptions{})
	if art.Title != "测试文章" {
		t.Errorf("title = %q", art.Title)
	}
	if art.Account != "测试公众号" {
		t.Errorf("account = %q", art.Account)
	}
	if art.PostID != "abcdefghij" {
		t.Errorf("post id = %q, want URL slug", art.PostID)
	}
	// 3 张正文图 + 1 张仅出现在 picture 列表里的图；重复 hashA 需去重。
	if len(art.Medias) != 4 {
		for _, m := range art.Medias {
			t.Logf("media %d %s %s", m.Seq, m.Kind, m.URL)
		}
		t.Fatalf("media count = %d, want 4", len(art.Medias))
	}
	if art.Medias[1].Kind != "gif" || art.Medias[1].Ext != "gif" {
		t.Errorf("gif media = %+v", art.Medias[1])
	}
	for _, m := range art.Medias {
		if !strings.Contains(m.URL, "/0?") {
			t.Errorf("原图 URL 未替换 /0: %s", m.URL)
		}
		if strings.Contains(m.URL, "watermark") {
			t.Errorf("误抓到水印图: %s", m.URL)
		}
	}
}

func TestBuildRelPath(t *testing.T) {
	m := &Media{
		Title: "哪 里/搞错:了?", PostID: "2247523869", Seq: 3,
		Ext: "jpg", Kind: "image", Width: 1080, Height: 1525,
		PublishTime: 1792575000, Account: "星空3532",
	}
	cases := []struct{ tpl, want string }{
		{"<title>-<post_id>-<order>.<ext>", "哪 里_搞错_了_-2247523869-3.jpg"},
		{"<文章标题>/<文章id>-<图片序号>.<扩展名>", "哪 里_搞错_了_/2247523869-3.jpg"},
		{"<title>/<order2>-<kind>", "哪 里_搞错_了_/03-image.jpg"},
		{"<date>/<title>", "2026-10-21/哪 里_搞错_了_.jpg"},
	}
	for _, c := range cases {
		got, err := BuildRelPath(m, c.tpl)
		if err != nil {
			t.Fatalf("BuildRelPath(%q): %v", c.tpl, err)
		}
		if got != c.want {
			t.Errorf("BuildRelPath(%q) = %q, want %q", c.tpl, got, c.want)
		}
	}
	abs, err := BuildAbsPath("/tmp/wxmp-test-out", m, "../<title>")
	if err != nil {
		t.Fatalf("BuildAbsPath: %v", err)
	}
	if !strings.HasPrefix(abs, "/tmp/wxmp-test-out/") {
		t.Errorf("路径越界: %s", abs)
	}
}

func TestOriginalURL(t *testing.T) {
	cases := map[string]string{
		"https://mmbiz.qpic.cn/sz_mmbiz_png/h/640?wx_fmt=png&from=appmsg": "https://mmbiz.qpic.cn/sz_mmbiz_png/h/0?from=appmsg&wx_fmt=png",
		"https://mmbiz.qpic.cn/mmbiz_gif/h/300?wx_fmt=gif":                "https://mmbiz.qpic.cn/mmbiz_gif/h/0?wx_fmt=gif",
	}
	for in, want := range cases {
		ext := "png"
		if strings.Contains(in, "gif") {
			ext = "gif"
		}
		if got := OriginalURL(in, ext); got != want {
			t.Errorf("OriginalURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseDedupesCDNVariants(t *testing.T) {
	const hash = "leqgpMdQQJgA9PRGpwwuicooZ7ym8spA9R7ntHQnqp5j0OQRnCHoWbTl8xtLdISJ2POBVprRVhC8OLocFl3TGZSFMQyaZx8tjKShopl2S5fs"
	page := `<html><body><div class="rich_media_content" id="js_content">
<img data-src="https://mmbiz.qpic.cn/mmbiz_jpg/` + hash + `/640?wx_fmt=jpeg" data-type="jpeg" data-w="849" data-ratio="1.41">
</div><script>picture_page_info_list: [
  { cdn_url: 'https://mmbiz.qpic.cn/sz_mmbiz_jpg/` + hash + `/0?wx_fmt=jpeg', width: '849'*1, height: '1197'*1 }
];</script></body></html>`
	art := ParseHTML(page, "https://mp.weixin.qq.com/s/tC1hauHuVvqRusojhhXe5g", ParseOptions{})
	if art.PostID != "tC1hauHuVvqRusojhhXe5g" {
		t.Fatalf("post id = %q", art.PostID)
	}
	if len(art.Medias) != 1 {
		for _, m := range art.Medias {
			t.Logf("%d %s", m.Seq, m.URL)
		}
		t.Fatalf("同一张图的 CDN 变体未去重: %d", len(art.Medias))
	}
}

func TestParseSkipsMirroredPictureList(t *testing.T) {
	h1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	h2 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	h3 := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	h4 := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	page := `<html><body><div class="rich_media_content" id="js_content">
<img data-src="https://mmbiz.qpic.cn/mmbiz_jpg/` + h1 + `/640?wx_fmt=jpeg" data-type="jpeg" data-w="849" data-ratio="1.41">
<img data-src="https://mmbiz.qpic.cn/mmbiz_png/` + h2 + `/640?wx_fmt=png" data-type="png" data-w="900" data-ratio="1">
</div><script>picture_page_info_list: [
  { cdn_url: 'https://mmbiz.qpic.cn/sz_mmbiz_jpg/` + h3 + `/0?wx_fmt=jpeg', width: '849'*1, height: '1197'*1 },
  { cdn_url: 'https://mmbiz.qpic.cn/sz_mmbiz_png/` + h4 + `/0?wx_fmt=png', width: '900'*1, height: '900'*1 }
];</script></body></html>`
	art := ParseHTML(page, "https://mp.weixin.qq.com/s/tC1hauHuVvqRusojhhXe5g", ParseOptions{})
	if len(art.Medias) != 2 {
		t.Fatalf("镜像图集未去重: %d", len(art.Medias))
	}
	if len(art.Warnings) == 0 || !strings.Contains(art.Warnings[0], "重复") {
		t.Fatalf("缺少重复提示: %v", art.Warnings)
	}
}

func TestParseKeepsDistinctHashesWithSharedPrefix(t *testing.T) {
	// 微信 CDN 的不同图片经常共享前 33 个字符，例如同一篇 4 图文章。
	base := "YbHDJ4Zk0FgVt3libtwPz0fcvI41uFWoo"
	suffixes := []string{
		"UN2QrviaaPqj6OVuHnnib9ED9rGOd0wWic24kbTtAUXGcvSz7Eft6mfHQ",
		"ibUaljWZB9GLe14moTAzDuB7eYDdYMbibQcuUJSmW71dYTvicK5bicLrww",
		"pDlBO7RkJjGLoWp6J6srRr2FoAxy4gmpP0XRJ3nVicmqyPbd558lGzA",
		"KjD0X4C8pSoQdCQdDv70icka1HF0PPicxXsEFGOia2cfgnB317pZkFVxg",
	}
	var imgs string
	for _, suf := range suffixes {
		imgs += `<img data-src="https://mmbiz.qpic.cn/sz_mmbiz_png/` + base + suf + `/640?wx_fmt=png" data-type="png" data-w="1080" data-ratio="1">`
	}
	page := `<html><body><div class="rich_media_content" id="js_content">` + imgs + `</div></body></html>`
	art := ParseHTML(page, "https://mp.weixin.qq.com/s/tC1hauHuVvqRusojhhXe5g", ParseOptions{})
	if len(art.Medias) != 4 {
		t.Fatalf("共享前缀的不同图片被误合并: %d", len(art.Medias))
	}
}

func TestParseKeepsDistinctContentImages(t *testing.T) {
	mk := func(prefix, suffix string) string {
		return `<img data-src="https://mmbiz.qpic.cn/mmbiz_jpg/` + prefix + suffix + `/640?wx_fmt=jpeg" data-type="jpeg" data-w="849" data-ratio="1.41">`
	}
	page := `<html><body><div class="rich_media_content" id="js_content">` +
		mk("aaaaaaaaaaaaaaaa", "1111111111111111111111111111111111111111111") +
		mk("bbbbbbbbbbbbbbbb", "2222222222222222222222222222222222222222222") +
		mk("cccccccccccccccc", "3333333333333333333333333333333333333333333") +
		mk("aaaaaaaaaaaaaaaa", "9999999999999999999999999999999999999999999") +
		mk("bbbbbbbbbbbbbbbb", "8888888888888888888888888888888888888888888") +
		mk("cccccccccccccccc", "7777777777777777777777777777777777777777777") +
		`</div></body></html>`
	art := ParseHTML(page, "https://mp.weixin.qq.com/s/tC1hauHuVvqRusojhhXe5g", ParseOptions{})
	if len(art.Medias) != 6 {
		t.Fatalf("共享前缀的不同正文图被误合并: %d", len(art.Medias))
	}
}

func TestParseSkipsDoubledMirroredPictureList(t *testing.T) {
	page := `<html><body><div class="rich_media_content" id="js_content">
<img data-src="https://mmbiz.qpic.cn/mmbiz_jpg/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/640?wx_fmt=jpeg" data-type="jpeg" data-w="849" data-ratio="1.41">
<img data-src="https://mmbiz.qpic.cn/mmbiz_png/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb/640?wx_fmt=png" data-type="png" data-w="900" data-ratio="1">
</div><script>picture_page_info_list: [
 { cdn_url: 'https://mmbiz.qpic.cn/sz_mmbiz_jpg/cccccccccccccccccccccccccccccccccccccc/0?wx_fmt=jpeg', width: '849'*1, height: '1197'*1 },
 { cdn_url: 'https://mmbiz.qpic.cn/sz_mmbiz_png/dddddddddddddddddddddddddddddddddddddd/0?wx_fmt=png', width: '900'*1, height: '900'*1 },
 { cdn_url: 'https://mmbiz.qpic.cn/sz_mmbiz_jpg/eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee/0?wx_fmt=jpeg', width: '849'*1, height: '1197'*1 },
 { cdn_url: 'https://mmbiz.qpic.cn/sz_mmbiz_png/ffffffffffffffffffffffffffffffffffffff/0?wx_fmt=png', width: '900'*1, height: '900'*1 }
];</script></body></html>`
	art := ParseHTML(page, "https://mp.weixin.qq.com/s/tC1hauHuVvqRusojhhXe5g", ParseOptions{})
	if len(art.Medias) != 2 {
		t.Fatalf("成对镜像图集未去重: %d", len(art.Medias))
	}
}
