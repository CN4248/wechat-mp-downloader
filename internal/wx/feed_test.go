package wx

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestParseFeedPage(t *testing.T) {
	inner := `{"list":[{"comm_msg_info":{"id":1,"datetime":1793000000,"type":49},` +
		`"app_msg_ext_info":{"title":"第一篇","digest":"摘要","content_url":"http://mp.weixin.qq.com/s?__biz=MzA=&mid=2247499001&idx=1&sn=abc&chksm=1#rd","cover":"http://mmbiz.qpic.cn/a.jpg",` +
		`"multi_app_msg_item_list":[{"title":"第二篇","content_url":"http://mp.weixin.qq.com/s?__biz=MzA=&mid=2247499001&idx=2&sn=def","cover":""}]}}]}`
	payload := `{"ret":0,"errmsg":"ok","msg_count":2,"can_msg_continue":1,"next_offset":10,"general_msg_list":` +
		strconv.Quote(inner) + `}`
	pg, err := ParseFeedPage([]byte(payload))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pg.Articles) != 2 {
		t.Fatalf("articles = %d, want 2 (主图文 + 次条)", len(pg.Articles))
	}
	first := pg.Articles[0]
	if first.Key != "2247499001_1" || first.Mid != "2247499001" || first.Idx != "1" {
		t.Fatalf("first key = %+v", first)
	}
	if first.PublishTime != 1793000000 {
		t.Fatalf("publish = %d", first.PublishTime)
	}
	if !strings.HasPrefix(first.URL, "https://mp.weixin.qq.com/s?") {
		t.Fatalf("url = %q (should be normalised to https)", first.URL)
	}
	if pg.Articles[1].Key != "2247499001_2" {
		t.Fatalf("second key = %q", pg.Articles[1].Key)
	}
	if !pg.Continue || pg.NextOffset != 10 {
		t.Fatalf("continue=%v next=%d", pg.Continue, pg.NextOffset)
	}
}

func TestParseFeedNeedsCookie(t *testing.T) {
	cases := [][]byte{
		[]byte(`{"ret":-3,"errmsg":"no session"}`),
		[]byte(`<html><body>请在微信客户端打开链接</body></html>`),
		[]byte(`<html><body>环境异常，请完成验证</body></html>`),
	}
	for _, body := range cases {
		if _, err := ParseFeedPage(body); err != ErrFeedNeedsCookie {
			t.Fatalf("body %q -> err %v, want ErrFeedNeedsCookie", string(body), err)
		}
	}
}

func TestCookieStatusFindsUinAndKey(t *testing.T) {
	c := NewClient()
	if set, uin, key := c.CookieStatus(); set || uin || key {
		t.Fatal("new client must have no cookie")
	}
	c.SetCookie("wap_sid2=abc; uin=o1234567890; key=deadbeef; pass_ticket=x")
	set, uin, key := c.CookieStatus()
	if !set || !uin || !key {
		t.Fatalf("cookie status = %v %v %v", set, uin, key)
	}
	if got := CookieParam(c.CookieValue(), "key"); got != "deadbeef" {
		t.Fatalf("key = %q", got)
	}
	c.SetCookie("")
	if set, _, _ := c.CookieStatus(); set {
		t.Fatal("cookie not cleared")
	}
}

func TestFeedKeyOfURL(t *testing.T) {
	if got := FeedKeyOfURL("https://mp.weixin.qq.com/s?__biz=MzA=&mid=123&idx=2&sn=x"); got != "123_2" {
		t.Fatalf("key = %q", got)
	}
	if got := FeedKeyOfURL("https://mp.weixin.qq.com/s/abc"); got != "https://mp.weixin.qq.com/s/abc" {
		t.Fatalf("short link key = %q", got)
	}
}

// feedTransport answers the account-feed endpoint with a fixed payload.
type feedTransport struct {
	body string
	got  string
}

func (f *feedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.got = req.URL.String()
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Header:     http.Header{"Content-Type": {"application/json"}},
		Request:    req,
	}, nil
}

func TestFetchAccountFeedSendsUinKey(t *testing.T) {
	inner := `{"list":[{"comm_msg_info":{"datetime":1793000000},` +
		`"app_msg_ext_info":{"title":"一篇","content_url":"http://mp.weixin.qq.com/s?__biz=MzA=&mid=42&idx=1&sn=x","cover":""}}]}`
	body := `{"ret":0,"errmsg":"ok","can_msg_continue":0,"general_msg_list":` + strconv.Quote(inner) + `}`
	rt := &feedTransport{body: body}
	c := NewClient()
	c.HTTP = &http.Client{Transport: rt}
	c.SetCookie("uin=o123; key=secret")

	pg, err := c.FetchAccountFeed(context.Background(), "Mzkz==", 0, 10)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(pg.Articles) != 1 || pg.Articles[0].Key != "42_1" {
		t.Fatalf("articles = %+v", pg.Articles)
	}
	if !strings.Contains(rt.got, "uin=o123") || !strings.Contains(rt.got, "key=secret") {
		t.Fatalf("request missing uin/key: %s", rt.got)
	}
	if !strings.Contains(rt.got, "action=getmsg") || !strings.Contains(rt.got, "__biz=Mzkz%3D%3D") {
		t.Fatalf("request url = %s", rt.got)
	}
}

func TestFetchAccountFeedWithoutCookie(t *testing.T) {
	c := NewClient()
	if _, err := c.FetchAccountFeed(context.Background(), "Mzkz==", 0, 10); err != ErrFeedNeedsCookie {
		t.Fatalf("err = %v, want ErrFeedNeedsCookie", err)
	}
}
