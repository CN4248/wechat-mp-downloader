package wx

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// AlbumRef is one album (合集) discovered on a public-account page.
type AlbumRef struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Biz          string `json:"biz"`
	ArticleCount int    `json:"article_count,omitempty"`
}

// AccountRef identifies one official account (公众号).
type AccountRef struct {
	Biz   string `json:"biz"`
	Name  string `json:"name"`
	GhID  string `json:"gh_id"`
	Cover string `json:"cover"`
}

// AccountInfo is what the WebUI shows when a subscription is created from a
// link: the account identity plus every album the seed article exposes.
type AccountInfo struct {
	AccountRef
	SeedURL      string      `json:"seed_url"`
	Kind         string      `json:"kind"` // article | album | profile
	Albums       []*AlbumRef `json:"albums"`
	Title        string      `json:"title"`
	ArticleCount int         `json:"article_count,omitempty"`
}

var (
	reAlbumIDField = regexp.MustCompile(`album_id\s*:\s*['"](\d{6,})['"]`)
	reAlbumTitle   = regexp.MustCompile(`title\s*:\s*['"]([^'"]{0,120})['"]`)
	reBizParam     = regexp.MustCompile(`[?&]__biz=([A-Za-z0-9+/=_-]{4,})`)
)

// ExtractBiz accepts an article link, an album link or a profile_ext link and
// returns its __biz value.
func ExtractBiz(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(ExtractArticleURL(raw)); err == nil {
		if biz := u.Query().Get("__biz"); biz != "" {
			return biz
		}
	}
	if m := reBizParam.FindStringSubmatch(raw); m != nil {
		return m[1]
	}
	return ""
}

// ResolveAccount turns a public link into account identity + album list.
//
// WeChat only exposes article/album pages to anonymous callers, so this reads
// the seed page itself: the article page carries `var biz`, `var nickname`,
// `var user_name` and an `album_info` block per album the article belongs to.
func (c *Client) ResolveAccount(ctx context.Context, raw string) (*AccountInfo, error) {
	raw = strings.TrimSpace(ExtractArticleURL(raw))
	if raw == "" {
		return nil, fmt.Errorf("请填写文章链接或公众号链接")
	}
	info := &AccountInfo{SeedURL: raw}
	if biz := ExtractBiz(raw); biz != "" {
		info.Biz = biz
	}
	switch {
	case IsAlbumURL(raw):
		info.Kind = "album"
		ai, err := c.FetchAlbumInfo(ctx, raw)
		if err != nil {
			return nil, err
		}
		info.Biz = firstNonEmpty(info.Biz, ai.Biz)
		info.Name = ai.Account
		info.Title = ai.Title
		info.ArticleCount = ai.ArticleCount
		if ai.ID != "" {
			info.Albums = append(info.Albums, &AlbumRef{ID: ai.ID, Title: ai.Title, Biz: info.Biz, ArticleCount: ai.ArticleCount})
		}
		return info, nil
	case strings.Contains(raw, "/mp/profile_ext"), strings.Contains(raw, "/mp/homepage"):
		info.Kind = "profile"
		if info.Biz == "" {
			return nil, fmt.Errorf("链接里没有 __biz，无法识别公众号")
		}
		info.Name = "公众号 " + shortBiz(info.Biz)
		return info, nil
	}
	info.Kind = "article"
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("无法识别的链接：%s", raw)
	}
	if host := strings.ToLower(u.Host); host != "mp.weixin.qq.com" && host != "weixin.qq.com" {
		return nil, fmt.Errorf("只支持 mp.weixin.qq.com 链接，收到：%s", u.Host)
	}
	page, final, err := c.FetchPage(ctx, raw)
	if err != nil {
		return nil, err
	}
	art := ParseHTML(page, final, ParseOptions{})
	parsed := AccountOf(page, final, art)
	parsed.Biz = firstNonEmpty(parsed.Biz, info.Biz)
	parsed.Kind = "article"
	parsed.Title = art.Title
	if parsed.Biz == "" {
		return nil, fmt.Errorf("文章页面没有 __biz，无法定位公众号（可能需要在下载设置里填入 Cookie）")
	}
	return parsed, nil
}

// AccountOf builds the account identity from an already parsed article page, so
// the WebUI can remember every account it parses without fetching twice.
func AccountOf(page, final string, art *Article) *AccountInfo {
	info := &AccountInfo{SeedURL: final, Kind: "article"}
	if art != nil {
		info.Biz = art.Biz
		info.Name = art.Account
		info.Title = art.Title
	}
	if info.Name == "" {
		info.Name = cleanText(jsVar(page, "nickname"))
	}
	info.GhID = cleanText(jsVar(page, "user_name"))
	if info.Biz == "" {
		info.Biz = cleanText(jsVar(page, "biz"))
	}
	info.Albums = discoverAlbums(page, info.Biz)
	return info
}

// discoverAlbums extracts the `album_info` blocks of an article page.
func discoverAlbums(page, biz string) []*AlbumRef {
	locs := reAlbumIDField.FindAllStringSubmatchIndex(page, -1)
	if len(locs) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]*AlbumRef, 0, len(locs))
	for _, loc := range locs {
		id := page[loc[2]:loc[3]]
		if seen[id] {
			continue
		}
		seen[id] = true
		title := ""
		// The title sits in the same album_info object, usually < 600 bytes away.
		tail := page[loc[1]:]
		if len(tail) > 600 {
			tail = tail[:600]
		}
		if m := reAlbumTitle.FindStringSubmatch(tail); m != nil {
			title = cleanText(decodeJSText(m[1]))
		}
		if title == "" {
			title = "合集 " + id
		}
		out = append(out, &AlbumRef{ID: id, Title: title, Biz: biz})
	}
	return out
}

// AlbumURL builds the canonical album link for an album id.
func AlbumURL(biz, albumID string) string {
	return fmt.Sprintf("https://mp.weixin.qq.com/mp/appmsgalbum?action=getalbum&__biz=%s&album_id=%s",
		url.QueryEscape(biz), url.QueryEscape(albumID))
}

func shortBiz(biz string) string {
	if len(biz) <= 10 {
		return biz
	}
	return biz[:10] + "…"
}
