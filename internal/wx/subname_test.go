package wx

import (
	"strings"
	"testing"
)

func TestVarsOfRoundTrip(t *testing.T) {
	m := &Media{
		Seq: 3, Kind: "image", Ext: "jpeg", Width: 800, Height: 600,
		Title: "标题/带斜杠", PostID: "abc123", Account: "公众号", Author: "作者",
		PublishTime: 1792575000, CollectionTitle: "合集", CollectionOrder: 7,
	}
	vars := VarsOf(m)
	if vars["ext"] != "jpg" {
		t.Fatalf("ext = %q, want normalized jpg", vars["ext"])
	}
	if vars["order2"] != "03" {
		t.Fatalf("order2 = %q", vars["order2"])
	}
	if vars["collection_order"] != "7" {
		t.Fatalf("collection_order = %q", vars["collection_order"])
	}
	direct, err := BuildRelPath(m, DefaultNameTemplate)
	if err != nil {
		t.Fatal(err)
	}
	fromVars, err := RenderRelPath(DefaultNameTemplate, vars)
	if err != nil {
		t.Fatal(err)
	}
	if direct != fromVars {
		t.Fatalf("RenderRelPath mismatch: %q vs %q", direct, fromVars)
	}
	// The slash inside the title must not create a directory level.
	if strings.Count(fromVars, "/") != 0 {
		t.Fatalf("title slash leaked into path: %q", fromVars)
	}
}

func TestRenderRelPathChineseAliasAndSubdirs(t *testing.T) {
	vars := map[string]string{
		"title": "文章", "post_id": "pid", "ext": "png",
		"collection_title": "合集A", "order": "2", "order2": "02",
	}
	got, err := RenderRelPath("<合集标题>/<序号2>-<文章id>.<扩展名>", vars)
	if err != nil {
		t.Fatal(err)
	}
	if got != "合集A/02-pid.png" {
		t.Fatalf("got %q", got)
	}
	if err := ValidateTemplate("<unknown>"); err == nil {
		t.Fatal("unknown token must be rejected by ValidateTemplate")
	}
	if got, _ := RenderRelPath("<unknown>", vars); got != "_.png" {
		t.Fatalf("unknown token renders as %q, want _.png", got)
	}
}

func TestRenderRelPathAppendsExtension(t *testing.T) {
	got, err := RenderRelPath("<title>", map[string]string{"title": "abc", "ext": "gif"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "abc.gif" {
		t.Fatalf("got %q, want abc.gif", got)
	}
}

func TestExtractBizAndAlbumURL(t *testing.T) {
	raw := "https://mp.weixin.qq.com/mp/appmsgalbum?__biz=MzkxMjM0NTY3OA==&action=getalbum&album_id=1234567890"
	if biz := ExtractBiz(raw); biz != "MzkxMjM0NTY3OA==" {
		t.Fatalf("biz = %q", biz)
	}
	if !IsAlbumURL(raw) {
		t.Fatal("album url not detected")
	}
	if id := AlbumIDFromURL(raw); id != "1234567890" {
		t.Fatalf("album id = %q", id)
	}
}
