package wx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// MaxCollectionArticles = 0 means "no fixed cap": an album is enumerated until
// the endpoint reports the end, a page makes no progress, or the caller's
// context expires. Pass max > 0 only when a caller wants a hard limit (CLI
// -max-articles, WebUI 单次篇数).
const MaxCollectionArticles = 0

// underMax reports whether one more page may be collected.
func underMax(n, max int) bool { return max <= 0 || n < max }

// CollectionArticle is one entry in a WeChat album (文章合集).
type CollectionArticle struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	MsgID       string `json:"msgid"`
	ItemIdx     string `json:"itemidx"`
	Order       int    `json:"order"`
	PublishTime int64  `json:"publish_time"`
	Cover       string `json:"cover"`
}

// Collection is an album's metadata plus its article list.
type Collection struct {
	URL          string               `json:"url"`
	FinalURL     string               `json:"final_url"`
	ID           string               `json:"id"`
	Title        string               `json:"title"`
	Account      string               `json:"account"`
	Description  string               `json:"description"`
	ArticleCount int                  `json:"article_count"`
	Articles     []*CollectionArticle `json:"articles"`
	Warnings     []string             `json:"warnings"`
}

// IsAlbumURL reports whether raw points at mp.weixin.qq.com/mp/appmsgalbum.
func IsAlbumURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(ExtractArticleURL(raw)))
	if err != nil {
		return false
	}
	if !strings.EqualFold(u.Host, "mp.weixin.qq.com") {
		return false
	}
	if !strings.Contains(u.Path, "appmsgalbum") {
		return false
	}
	q := u.Query()
	return q.Get("album_id") != ""
}

// AlbumIDFromURL returns the album_id query value.
func AlbumIDFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return u.Query().Get("album_id")
}

// NormalizeAlbumURL canonicalizes an album link, keeping only the parameters
// that identify the album.
func NormalizeAlbumURL(raw string) (string, error) {
	raw = ExtractArticleURL(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("无法识别的合集链接：%s", raw)
	}
	if !strings.EqualFold(u.Host, "mp.weixin.qq.com") {
		return "", fmt.Errorf("只支持 mp.weixin.qq.com 合集链接，收到：%s", u.Host)
	}
	q := u.Query()
	biz, album := q.Get("__biz"), q.Get("album_id")
	if album == "" || biz == "" {
		return "", fmt.Errorf("合集链接缺少 __biz 或 album_id")
	}
	u.Scheme = "https"
	u.Host = "mp.weixin.qq.com"
	u.Fragment = ""
	u.RawQuery = url.Values{
		"action":   {"getalbum"},
		"__biz":    {biz},
		"album_id": {album},
	}.Encode()
	return u.String(), nil
}

// AlbumInfo is the lightweight album metadata used when adding a subscription.
type AlbumInfo struct {
	URL          string `json:"url"`
	ID           string `json:"id"`
	Biz          string `json:"biz"`
	Title        string `json:"title"`
	Account      string `json:"account"`
	ArticleCount int    `json:"article_count"`
}

// AlbumPage is one page of album entries returned by the JSON endpoint.
type AlbumPage struct {
	Articles []*CollectionArticle
	Continue bool
}

func normalizeCollectionArticle(it *CollectionArticle) *CollectionArticle {
	link := strings.TrimSpace(decodeJSText(it.URL))
	if link == "" {
		return nil
	}
	if n, err := NormalizeArticleURL(link); err == nil {
		link = n
	}
	it.URL = link
	it.Title = cleanText(decodeJSText(it.Title))
	it.Cover = strings.TrimSpace(decodeJSText(it.Cover))
	return it
}

// FetchAlbumInfo reads album metadata plus the newest page of entries. It does
// not paginate, so it is cheap enough to use when a subscription is created.
func (c *Client) FetchAlbumInfo(ctx context.Context, raw string) (*AlbumInfo, error) {
	albumURL, err := NormalizeAlbumURL(raw)
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(albumURL)
	q := u.Query()
	info := &AlbumInfo{URL: albumURL, ID: q.Get("album_id"), Biz: q.Get("__biz")}

	page, _, err := c.FetchPage(ctx, albumURL)
	if err != nil {
		return nil, err
	}
	if cgi := jsObject(page, "window.cgiData"); cgi != "" {
		info.Title = cleanText(jsField(cgi, "title"))
		info.Account = cleanText(jsField(cgi, "nick_name"))
		if id := strings.TrimSpace(jsField(cgi, "albumId")); id != "" {
			info.ID = id
		}
		info.ArticleCount = int(jsNumber(cgi, "article_count"))
	}
	if info.Title == "" {
		info.Title = cleanText(htmlTitle(page))
	}
	return info, nil
}

// albumArticleJSON is one entry of the album JSON endpoint.
type albumArticleJSON struct {
	Title      string      `json:"title"`
	URL        string      `json:"url"`
	MsgID      string      `json:"msgid"`
	ItemIdx    string      `json:"itemidx"`
	PosNum     json.Number `json:"pos_num"`
	CreateTime json.Number `json:"create_time"`
	CoverImg   string      `json:"cover_img_1_1"`
}

// albumArticleList accepts the endpoint's two shapes: normally an array, but
// when the final page holds exactly one article WeChat serialises it as an
// object (a PHP-style single-element map). Treating the object as a one-item
// list keeps the oldest article of an album instead of failing the whole page.
type albumArticleList []albumArticleJSON

func (l *albumArticleList) UnmarshalJSON(b []byte) error {
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*l = nil
		return nil
	}
	switch trimmed[0] {
	case '[':
		var items []albumArticleJSON
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return err
		}
		*l = items
		return nil
	case '{':
		var one albumArticleJSON
		if err := json.Unmarshal(trimmed, &one); err != nil {
			return err
		}
		// An empty object means "no more entries", not a malformed article.
		if one.URL == "" && one.MsgID == "" {
			*l = nil
			return nil
		}
		*l = albumArticleList{one}
		return nil
	default:
		return fmt.Errorf("article_list 既不是数组也不是对象：%.40s", trimmed)
	}
}

// albumPageResponse is the getalbum JSON envelope.
type albumPageResponse struct {
	GetAlbum struct {
		ArticleList  albumArticleList `json:"article_list"`
		ContinueFlag json.Number      `json:"continue_flag"`
	} `json:"getalbum_resp"`
	BaseResp struct {
		Ret int `json:"ret"`
	} `json:"base_resp"`
}

// FetchAlbumPage reads one page of an album through the public JSON endpoint.
//
// WeChat returns album entries in the album's own display order by default
// (some albums are oldest-first). Pass reverse=true to get newest-first paging;
// `begin*` then continues towards older entries.
func (c *Client) FetchAlbumPage(ctx context.Context, biz, albumID, beginMsgID, beginItemIdx string, count int, reverse bool) (*AlbumPage, error) {
	if strings.TrimSpace(biz) == "" || strings.TrimSpace(albumID) == "" {
		return nil, fmt.Errorf("缺少 __biz 或 album_id")
	}
	if count <= 0 || count > 40 {
		count = 20
	}
	apiURL := fmt.Sprintf("https://mp.weixin.qq.com/mp/appmsgalbum?action=getalbum&__biz=%s&album_id=%s&count=%d&f=json",
		url.QueryEscape(biz), url.QueryEscape(albumID), count)
	if beginMsgID != "" {
		apiURL += "&begin_msgid=" + url.QueryEscape(beginMsgID)
	}
	if beginItemIdx != "" {
		apiURL += "&begin_itemidx=" + url.QueryEscape(beginItemIdx)
	}
	if reverse {
		apiURL += "&is_reverse=1"
	}
	body, _, err := c.FetchBytes(ctx, apiURL)
	if err != nil {
		return nil, err
	}
	var resp albumPageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("合集分页解析失败：%w", err)
	}
	if resp.BaseResp.Ret != 0 {
		return nil, fmt.Errorf("合集分页返回错误码 %d", resp.BaseResp.Ret)
	}
	out := &AlbumPage{}
	for _, it := range resp.GetAlbum.ArticleList {
		pos, _ := it.PosNum.Int64()
		ts, _ := it.CreateTime.Int64()
		art := normalizeCollectionArticle(&CollectionArticle{
			Title: it.Title, URL: it.URL, MsgID: it.MsgID, ItemIdx: it.ItemIdx,
			Order: int(pos), PublishTime: ts, Cover: it.CoverImg,
		})
		if art == nil {
			continue
		}
		out.Articles = append(out.Articles, art)
	}
	cont, _ := resp.GetAlbum.ContinueFlag.Int64()
	out.Continue = cont != 0
	return out, nil
}

// FetchCollection reads album metadata and paginates its article list.

func (c *Client) FetchCollection(ctx context.Context, raw string, max int) (*Collection, error) {
	albumURL, err := NormalizeAlbumURL(raw)
	if err != nil {
		return nil, err
	}
	if max < 0 {
		max = 0
	}
	u, _ := url.Parse(albumURL)
	q := u.Query()
	biz, albumID := q.Get("__biz"), q.Get("album_id")

	page, final, err := c.FetchPage(ctx, albumURL)
	if err != nil {
		return nil, err
	}
	col := &Collection{URL: albumURL, FinalURL: final, ID: albumID}
	if cgi := jsObject(page, "window.cgiData"); cgi != "" {
		col.Title = cleanText(jsField(cgi, "title"))
		col.Account = cleanText(jsField(cgi, "nick_name"))
		col.Description = cleanText(jsField(cgi, "desc"))
		if id := strings.TrimSpace(jsField(cgi, "albumId")); id != "" {
			col.ID = id
		}
		col.ArticleCount = int(jsNumber(cgi, "article_count"))
	}
	if col.Title == "" {
		col.Title = cleanText(htmlTitle(page))
	}

	seen := map[string]bool{}
	appendItem := func(title, link, msgid, idx, cover string, pos int, ts int64) bool {
		link = strings.TrimSpace(decodeJSText(link))
		if link == "" {
			return false
		}
		if n, err := NormalizeArticleURL(link); err == nil {
			link = n
		}
		key := msgid + "|" + idx
		if seen[key] {
			return false
		}
		seen[key] = true
		col.Articles = append(col.Articles, &CollectionArticle{
			Title: cleanText(decodeJSText(title)), URL: link, MsgID: msgid,
			ItemIdx: idx, Order: pos, PublishTime: ts, Cover: strings.TrimSpace(decodeJSText(cover)),
		})
		return true
	}

	// The server-rendered page contains the first 10 entries.
	if list := jsArray(page, "articleList"); list != "" {
		for _, obj := range topLevelObjects(list) {
			if !underMax(len(col.Articles), max) {
				break
			}
			pos := int(jsNumber(obj, "pos_num"))
			appendItem(jsField(obj, "title"), jsField(obj, "url"), jsField(obj, "msgid"), jsField(obj, "itemidx"), jsField(obj, "cover_img_1_1"), pos, int64(jsNumber(obj, "create_time")))
		}
	}

	// Older entries come from the JSON endpoint, newest page first.
	for len(col.Articles) > 0 && underMax(len(col.Articles), max) {
		last := col.Articles[len(col.Articles)-1]
		if last.MsgID == "" {
			break
		}
		pg, ferr := c.FetchAlbumPage(ctx, biz, albumID, last.MsgID, last.ItemIdx, 20, false)
		if ferr != nil {
			col.Warnings = append(col.Warnings, "合集分页中断："+ferr.Error())
			break
		}
		before := len(col.Articles)
		for _, it := range pg.Articles {
			appendItem(it.Title, it.URL, it.MsgID, it.ItemIdx, it.Cover, it.Order, it.PublishTime)
			if !underMax(len(col.Articles), max) {
				break
			}
		}
		if len(col.Articles) == before || !pg.Continue {
			break
		}
	}

	if col.ArticleCount > 0 && len(col.Articles) < col.ArticleCount {
		col.Warnings = append(col.Warnings, fmt.Sprintf("合集共 %d 篇，本次解析到 %d 篇", col.ArticleCount, len(col.Articles)))
	}
	if max > 0 && len(col.Articles) > max {
		col.Articles = col.Articles[:max]
	}
	// pos_num is authoritative; fill gaps for pages that omit it.
	for i, a := range col.Articles {
		if a.Order <= 0 {
			if col.ArticleCount > 0 {
				a.Order = col.ArticleCount - i
			} else {
				a.Order = len(col.Articles) - i
			}
		}
	}
	if col.ArticleCount == 0 {
		col.ArticleCount = len(col.Articles)
	}
	return col, nil
}

// jsObject returns the brace body after name in page, e.g. window.cgiData = {...}.
func jsObject(page, name string) string {
	i := strings.Index(page, name)
	if i < 0 {
		return ""
	}
	j := i + len(name)
	for j < len(page) && page[j] != '{' && page[j] != ';' {
		j++
	}
	if j >= len(page) || page[j] != '{' {
		return ""
	}
	return sliceBrackets(page, j, '{', '}')
}

// jsArray returns the bracket body after name in page.
func jsArray(page, name string) string {
	re := name + `\s*[:=]\s*\[`
	i := strings.Index(page, name)
	if i < 0 {
		return ""
	}
	_ = re
	j := i + len(name)
	for j < len(page) && page[j] != '[' && page[j] != ';' {
		j++
	}
	if j >= len(page) || page[j] != '[' {
		return ""
	}
	return sliceBrackets(page, j, '[', ']')
}

func atoi(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }

// NormalizeInputURL accepts either a single article or an album link.
func NormalizeInputURL(raw string) (string, error) {
	if IsAlbumURL(raw) {
		return NormalizeAlbumURL(raw)
	}
	return NormalizeArticleURL(raw)
}
