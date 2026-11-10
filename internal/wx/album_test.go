package wx

import (
	"encoding/json"
	"testing"
)

func TestAlbumPageResponseArrayForm(t *testing.T) {
	raw := `{"base_resp":{"ret":0},"getalbum_resp":{"article_list":[
		{"title":"新一篇","url":"http://mp.weixin.qq.com/s?a","msgid":"2247496735","itemidx":"1","pos_num":"221","create_time":"1793766896","cover_img_1_1":"c1"},
		{"title":"下一篇","url":"http://mp.weixin.qq.com/s?b","msgid":"2247496723","itemidx":"1","pos_num":"220","create_time":"1793590200","cover_img_1_1":"c2"}
	],"continue_flag":1}}`
	var resp albumPageResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.GetAlbum.ArticleList) != 2 {
		t.Fatalf("articles = %d", len(resp.GetAlbum.ArticleList))
	}
	if got := resp.GetAlbum.ArticleList[1].Title; got != "下一篇" {
		t.Fatalf("title = %q", got)
	}
	if cont, _ := resp.GetAlbum.ContinueFlag.Int64(); cont != 1 {
		t.Fatalf("continue = %d", cont)
	}
}

// WeChat serialises a final page with exactly one article as an object instead
// of a one-element array. Dropping it used to fail the whole page and hide the
// oldest article (album with 221 entries stopped at 220).
func TestAlbumPageResponseSingleObjectForm(t *testing.T) {
	raw := `{"base_resp":{"ret":0},"getalbum_resp":{"article_list":{
		"title":"最老一篇","url":"http://mp.weixin.qq.com/s?__biz=x&mid=2247485175&idx=1&sn=y#rd",
		"msgid":"2247485175","itemidx":"1","pos_num":"1","create_time":"1698201000","cover_img_1_1":"cover"
	},"continue_flag":0}}`
	var resp albumPageResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	list := resp.GetAlbum.ArticleList
	if len(list) != 1 {
		t.Fatalf("articles = %d, want 1", len(list))
	}
	if list[0].MsgID != "2247485175" || list[0].ItemIdx != "1" {
		t.Fatalf("entry = %#v", list[0])
	}
	if pos, _ := list[0].PosNum.Int64(); pos != 1 {
		t.Fatalf("pos = %d", pos)
	}
	art := normalizeCollectionArticle(&CollectionArticle{
		Title: list[0].Title, URL: list[0].URL, MsgID: list[0].MsgID, ItemIdx: list[0].ItemIdx,
		Order: 1, PublishTime: 1698201000,
	})
	if art == nil || art.Title != "最老一篇" {
		t.Fatalf("normalized = %#v", art)
	}
	if got := art.URL; got != "http://mp.weixin.qq.com/s?__biz=x&mid=2247485175&idx=1&sn=y" {
		t.Fatalf("url = %q", got)
	}
	if cont, _ := resp.GetAlbum.ContinueFlag.Int64(); cont != 0 {
		t.Fatalf("continue = %d", cont)
	}
}

func TestAlbumPageResponseEmptyObjectAndNull(t *testing.T) {
	for _, raw := range []string{
		`{"getalbum_resp":{"article_list":{},"continue_flag":0}}`,
		`{"getalbum_resp":{"article_list":null,"continue_flag":0}}`,
		`{"getalbum_resp":{"continue_flag":0}}`,
	} {
		var resp albumPageResponse
		if err := json.Unmarshal([]byte(raw), &resp); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if len(resp.GetAlbum.ArticleList) != 0 {
			t.Fatalf("%s: articles = %d", raw, len(resp.GetAlbum.ArticleList))
		}
	}
}

func TestAlbumPageResponseRejectsScalarList(t *testing.T) {
	raw := `{"getalbum_resp":{"article_list":"oops","continue_flag":0}}`
	var resp albumPageResponse
	if err := json.Unmarshal([]byte(raw), &resp); err == nil {
		t.Fatal("scalar article_list accepted")
	}
}
