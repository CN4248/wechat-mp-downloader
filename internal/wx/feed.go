package wx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// ErrFeedNeedsCookie is returned when the account feed endpoint refuses an
// anonymous request. WeChat only exposes an account's "every article" list to a
// logged-in WeChat session, so a real cookie (uin/key) is required.
var ErrFeedNeedsCookie = fmt.Errorf("微信只对登录态开放公众号文章列表：请在「下载设置」里填入微信 Cookie（需含 uin、key），或改用合集追更")

// FeedArticle is one article of an account feed.
type FeedArticle struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Digest      string `json:"digest"`
	Cover       string `json:"cover"`
	PublishTime int64  `json:"publish_time"`
	Key         string `json:"key"`
	Mid         string `json:"mid"`
	Idx         string `json:"idx"`
}

// FeedPage is one page of an account feed.
type FeedPage struct {
	Articles   []*FeedArticle
	Continue   bool
	NextOffset int
}

// CookieParam returns one cookie value, unescaped.
func CookieParam(cookie, name string) string {
	m := regexp.MustCompile(`(?:^|;\s*)` + regexp.QuoteMeta(name) + `=([^;]+)`).FindStringSubmatch(cookie)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// accountFeedURL builds the getmsg request. uin/key are taken from the cookie
// when present (that is what WeChat requires), and may also be supplied by the
// caller for links that carry them.
func accountFeedURL(biz, uin, key string, offset, count int) string {
	v := url.Values{}
	v.Set("action", "getmsg")
	v.Set("__biz", biz)
	v.Set("f", "json")
	v.Set("offset", strconv.Itoa(offset))
	v.Set("count", strconv.Itoa(count))
	v.Set("is_ok", "1")
	v.Set("scene", "124")
	if uin != "" {
		v.Set("uin", uin)
	}
	if key != "" {
		v.Set("key", key)
	}
	return "https://mp.weixin.qq.com/mp/profile_ext?" + v.Encode()
}

// FetchAccountFeed reads one page of an account's article feed.
//
// Requires a logged-in WeChat cookie (uin + key). Returns ErrFeedNeedsCookie
// when WeChat answers with its verification page instead of JSON.
func (c *Client) FetchAccountFeed(ctx context.Context, biz string, offset, count int) (*FeedPage, error) {
	if strings.TrimSpace(biz) == "" {
		return nil, fmt.Errorf("缺少公众号 __biz")
	}
	if count <= 0 || count > 20 {
		count = 10
	}
	ck := c.CookieValue()
	uin := CookieParam(ck, "uin")
	key := CookieParam(ck, "key")
	if uin == "" || key == "" {
		return nil, ErrFeedNeedsCookie
	}
	body, _, err := c.FetchBytes(ctx, accountFeedURL(biz, uin, key, offset, count))
	if err != nil {
		return nil, err
	}
	return ParseFeedPage(body)
}

// ParseFeedPage parses the getmsg JSON payload.
func ParseFeedPage(body []byte) (*FeedPage, error) {
	trimmed := strings.TrimSpace(string(body))
	if !strings.HasPrefix(trimmed, "{") {
		if strings.Contains(trimmed, "环境异常") || strings.Contains(trimmed, "完成验证") {
			return nil, ErrFeedNeedsCookie
		}
		if strings.Contains(trimmed, "请在微信客户端打开") || strings.Contains(trimmed, "验证") {
			return nil, ErrFeedNeedsCookie
		}
		return nil, fmt.Errorf("公众号文章列表返回了非 JSON 内容（长度 %d）", len(body))
	}
	var resp struct {
		Ret         int             `json:"ret"`
		ErrMsg      string          `json:"errmsg"`
		CanContinue json.Number     `json:"can_msg_continue"`
		NextOffset  json.Number     `json:"next_offset"`
		GeneralMsg  json.RawMessage `json:"general_msg_list"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("公众号文章列表解析失败：%w", err)
	}
	if resp.Ret != 0 {
		if resp.Ret == -3 || strings.Contains(strings.ToLower(resp.ErrMsg), "session") {
			return nil, ErrFeedNeedsCookie
		}
		return nil, fmt.Errorf("公众号文章列表返回错误码 %d：%s", resp.Ret, resp.ErrMsg)
	}
	// general_msg_list is a JSON *string* holding {"list":[...]}.
	raw := strings.TrimSpace(string(resp.GeneralMsg))
	if strings.HasPrefix(raw, `"`) {
		var inner string
		if err := json.Unmarshal(resp.GeneralMsg, &inner); err == nil {
			raw = inner
		}
	}
	out := &FeedPage{}
	if raw != "" && raw != "null" {
		var list struct {
			List []struct {
				CommMsgInfo struct {
					ID       json.Number `json:"id"`
					Datetime json.Number `json:"datetime"`
					Type     json.Number `json:"type"`
				} `json:"comm_msg_info"`
				AppMsgExt struct {
					Title      string `json:"title"`
					Digest     string `json:"digest"`
					ContentURL string `json:"content_url"`
					Cover      string `json:"cover"`
					Multi      []struct {
						Title      string `json:"title"`
						Digest     string `json:"digest"`
						ContentURL string `json:"content_url"`
						Cover      string `json:"cover"`
					} `json:"multi_app_msg_item_list"`
				} `json:"app_msg_ext_info"`
			} `json:"list"`
		}
		if err := json.Unmarshal([]byte(raw), &list); err != nil {
			return nil, fmt.Errorf("公众号文章列表解析失败：%w", err)
		}
		for _, item := range list.List {
			ts, _ := item.CommMsgInfo.Datetime.Int64()
			ext := item.AppMsgExt
			if ext.ContentURL != "" {
				if art := feedArticle(ext.Title, ext.Digest, ext.ContentURL, ext.Cover, ts); art != nil {
					out.Articles = append(out.Articles, art)
				}
			}
			for _, sub := range ext.Multi {
				if art := feedArticle(sub.Title, sub.Digest, sub.ContentURL, sub.Cover, ts); art != nil {
					out.Articles = append(out.Articles, art)
				}
			}
		}
	}
	if n, _ := resp.NextOffset.Int64(); n > 0 {
		out.NextOffset = int(n)
	}
	if c, _ := resp.CanContinue.Int64(); c != 0 {
		out.Continue = true
	}
	return out, nil
}

// feedArticle normalises one feed entry: URL, publish time and dedupe key.
func feedArticle(title, digest, contentURL, cover string, ts int64) *FeedArticle {
	contentURL = strings.TrimSpace(htmlUnescape(strings.ReplaceAll(contentURL, `\/`, "/")))
	if contentURL == "" {
		return nil
	}
	art := &FeedArticle{
		Title: cleanText(decodeJSText(title)), Digest: cleanText(decodeJSText(digest)),
		Cover: strings.TrimSpace(htmlUnescape(cover)), PublishTime: ts,
	}
	if u, err := url.Parse(contentURL); err == nil {
		if u.Host != "" {
			if u.Scheme == "" || u.Scheme == "http" {
				u.Scheme = "https"
			}
			u.Fragment = ""
			art.URL = u.String()
			q := u.Query()
			art.Mid, art.Idx = q.Get("mid"), q.Get("idx")
		}
	}
	if art.URL == "" {
		art.URL = contentURL
	}
	if art.Mid != "" {
		if art.Idx == "" {
			art.Idx = "1"
		}
		art.Key = art.Mid + "_" + art.Idx
	} else {
		art.Key = art.URL
	}
	return art
}

// FeedKeyOfURL extracts the mid_idx key from an article URL (used by tests and
// by the subscription scanner to dedupe feed entries).
func FeedKeyOfURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	q := u.Query()
	mid, idx := q.Get("mid"), q.Get("idx")
	if mid == "" {
		return raw
	}
	if idx == "" {
		idx = "1"
	}
	return mid + "_" + idx
}

// FeedArticleURLOf builds a canonical article URL from mid/idx/sn.
func FeedArticleURLOf(biz, mid, idx, sn string) string {
	v := url.Values{}
	v.Set("__biz", biz)
	v.Set("mid", mid)
	if idx == "" {
		idx = "1"
	}
	v.Set("idx", idx)
	if sn != "" {
		v.Set("sn", sn)
	}
	return "https://mp.weixin.qq.com/s?" + v.Encode()
}
