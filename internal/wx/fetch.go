// Package wx implements WeChat official-account (公众号) article parsing and
// original image downloading with the Go standard library only.
package wx

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// BrowserUA is a current desktop Chrome UA. WeChat serves the normal
	// article page (instead of the "environment abnormal" page) to it.
	BrowserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"

	// ArticleReferer is required by mmbiz.qpic.cn to avoid hot-link denial.
	ArticleReferer = "https://mp.weixin.qq.com/"
)

// ErrVerification is returned when WeChat answers with its security
// verification page instead of the article.
var ErrVerification = errors.New("微信返回了环境异常/验证页面，请在下载设置里填入浏览器 Cookie 后重试")

// ErrArticleGone is returned for deleted / blocked articles.
type ArticleGoneError struct{ Reason string }

func (e *ArticleGoneError) Error() string { return "文章不可访问：" + e.Reason }

// Client is a small HTTP client tuned for mp.weixin.qq.com and mmbiz.qpic.cn.
type Client struct {
	HTTP   *http.Client
	Cookie string
	UA     string

	resolveMu    sync.Mutex
	resolveCache map[string]resolveEntry
}

type resolveEntry struct {
	ip    string
	until time.Time
}

// NewClient builds a client with a 60s timeout and an IPv4 DoH fallback for
// networks where the system resolver returns unusable AAAA records.
func NewClient() *Client {
	c := &Client{resolveCache: map[string]resolveEntry{}}
	dialer := &net.Dialer{Timeout: 12 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   12 * time.Second,
		ResponseHeaderTimeout: 25 * time.Second,
		ForceAttemptHTTP2:     true,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, network, addr)
		if err == nil {
			return conn, nil
		}
		host, port, splitErr := net.SplitHostPort(addr)
		if splitErr != nil {
			return nil, err
		}
		ip := c.resolveIPv4(ctx, host)
		if ip == "" {
			return nil, err
		}
		return dialer.DialContext(ctx, "tcp4", net.JoinHostPort(ip, port))
	}
	c.HTTP = &http.Client{Transport: tr, Timeout: 90 * time.Second}
	c.UA = BrowserUA
	return c
}

// resolveIPv4 asks AliDNS over HTTPS for an A record. Failures are silent: the
// normal dialer error is returned to the caller and the UI shows it.
func (c *Client) resolveIPv4(ctx context.Context, host string) string {
	c.resolveMu.Lock()
	if e, ok := c.resolveCache[host]; ok && time.Now().Before(e.until) {
		c.resolveMu.Unlock()
		return e.ip
	}
	c.resolveMu.Unlock()

	dctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(dctx, http.MethodGet,
		"https://223.5.5.5/resolve?name="+url.QueryEscape(host)+"&type=A", nil)
	if err != nil {
		return ""
	}
	req.Host = "223.5.5.5"
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // IP endpoint, no matching SAN
	hc := &http.Client{Transport: tr, Timeout: 6 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var out struct {
		Answer []struct {
			Type int    `json:"type"`
			Data string `json:"data"`
		} `json:"Answer"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out); err != nil {
		return ""
	}
	for _, a := range out.Answer {
		if a.Type == 1 && net.ParseIP(a.Data) != nil {
			c.resolveMu.Lock()
			c.resolveCache[host] = resolveEntry{ip: a.Data, until: time.Now().Add(10 * time.Minute)}
			c.resolveMu.Unlock()
			return a.Data
		}
	}
	return ""
}

func (c *Client) newRequest(ctx context.Context, method, rawURL, referer string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	ua := c.UA
	if ua == "" {
		ua = BrowserUA
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.6")
	if referer == "" {
		referer = ArticleReferer
	}
	req.Header.Set("Referer", referer)
	if c.Cookie != "" {
		req.Header.Set("Cookie", c.Cookie)
	}
	return req, nil
}

// FetchPage downloads an article and returns its HTML and final URL.
func (c *Client) FetchPage(ctx context.Context, raw string) (string, string, error) {
	target, err := NormalizeArticleURL(raw)
	if err != nil {
		return "", "", err
	}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		req, reqErr := c.newRequest(ctx, http.MethodGet, target, "")
		if reqErr != nil {
			return "", "", reqErr
		}
		resp, doErr := c.HTTP.Do(req)
		if doErr != nil {
			lastErr = doErr
		} else {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
			final := resp.Request.URL.String()
			resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
			} else if resp.StatusCode == http.StatusOK {
				html := string(body)
				if reason := detectBlockedPage(html); reason != "" {
					if reason == "verification" {
						return "", final, ErrVerification
					}
					return "", final, &ArticleGoneError{Reason: reason}
				}
				if !strings.Contains(html, "js_content") && !strings.Contains(html, "msg_title") {
					lastErr = fmt.Errorf("页面不是公众号文章（长度 %d 字节）", len(body))
				} else {
					return html, final, nil
				}
			} else if resp.StatusCode == http.StatusNotFound {
				return "", final, &ArticleGoneError{Reason: "HTTP 404，链接可能已失效"}
			} else {
				lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			}
		}
		if attempt < 3 {
			select {
			case <-ctx.Done():
				return "", "", ctx.Err()
			case <-time.After(time.Duration(attempt) * 800 * time.Millisecond):
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("未知错误")
	}
	return "", "", lastErr
}

// FetchBytes downloads one binary resource (image / gif / video) with retries.
func (c *Client) FetchBytes(ctx context.Context, rawURL string) ([]byte, string, error) {
	rawURL = normalizeResourceURL(rawURL)
	if rawURL == "" {
		return nil, "", errors.New("空资源链接")
	}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := c.newRequest(ctx, http.MethodGet, rawURL, ArticleReferer)
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
		} else {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 200<<20))
			ct := resp.Header.Get("Content-Type")
			resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
			} else if resp.StatusCode == http.StatusOK {
				if len(body) == 0 {
					lastErr = errors.New("服务器返回空文件")
				} else if looksLikeHTML(body) {
					lastErr = errors.New("服务器返回 HTML（防盗链或链接失效）")
				} else {
					return body, ct, nil
				}
			} else {
				lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			}
		}
		if attempt < 3 {
			select {
			case <-ctx.Done():
				return nil, "", ctx.Err()
			case <-time.After(time.Duration(attempt) * 700 * time.Millisecond):
			}
		}
	}
	return nil, "", lastErr
}

func looksLikeHTML(b []byte) bool {
	i := 0
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		i = 3
	}
	for i < len(b) && i < 512 {
		switch b[i] {
		case ' ', '\t', '\r', '\n':
			i++
			continue
		case '<':
			return true
		}
		break
	}
	return false
}

func detectBlockedPage(html string) string {
	switch {
	case strings.Contains(html, "环境异常") && strings.Contains(html, "完成验证"):
		return "verification"
	case strings.Contains(html, "该内容已被发布者删除"):
		return "该内容已被发布者删除"
	case strings.Contains(html, "此内容因违规无法查看"):
		return "此内容因违规无法查看"
	case strings.Contains(html, "该公众号已迁移"):
		return "该公众号已迁移"
	case strings.Contains(html, "链接已过期"):
		return "链接已过期"
	}
	return ""
}

var reArticleURL = regexp.MustCompile(`https?://mp\.weixin\.qq\.com/[^\s"'<>，。；）)】\]]+`)

// ExtractArticleURL pulls the first mp.weixin.qq.com URL out of arbitrary
// pasted text (WeChat share text often has a title before the link).
func ExtractArticleURL(s string) string {
	s = strings.TrimSpace(s)
	if m := reArticleURL.FindString(s); m != "" {
		return m
	}
	return s
}

// NormalizeArticleURL validates and canonicalizes an article link.
func NormalizeArticleURL(raw string) (string, error) {
	raw = ExtractArticleURL(raw)
	raw = strings.TrimRight(raw, ".,;，。；")
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("无法识别的链接：%s", raw)
	}
	host := strings.ToLower(u.Host)
	if host != "mp.weixin.qq.com" && host != "weixin.qq.com" {
		return "", fmt.Errorf("只支持 mp.weixin.qq.com 文章链接，收到：%s", u.Host)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		u.Scheme = "https"
	}
	u.Fragment = ""
	if host == "weixin.qq.com" {
		u.Host = "mp.weixin.qq.com"
	}
	return u.String(), nil
}

// normalizeResourceURL fixes protocol-relative and HTML-escaped CDN links.
func normalizeResourceURL(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = htmlUnescape(raw)
	raw = strings.ReplaceAll(raw, `\/`, `/`)
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	return raw
}

// ArticleIDFromURL extracts the short id from /s/<id> links, used as a
// fallback post_id when the page metadata lacks appmsgid.
var reArticleSlug = regexp.MustCompile(`^[A-Za-z0-9_-]{6,128}$`)

func ArticleIDFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	p := strings.Trim(u.Path, "/")
	if !strings.HasPrefix(p, "s/") {
		return ""
	}
	slug := strings.Trim(strings.TrimPrefix(p, "s/"), "/")
	if !reArticleSlug.MatchString(slug) {
		return ""
	}
	return slug
}

// StreamTo downloads one resource into w (one attempt). The first 512 bytes are
// sniffed so an anti-hotlink HTML error page is rejected instead of saved.
func (c *Client) StreamTo(ctx context.Context, rawURL string, w io.Writer) (int64, string, error) {
	rawURL = normalizeResourceURL(rawURL)
	if rawURL == "" {
		return 0, "", errors.New("空资源链接")
	}
	req, err := c.newRequest(ctx, http.MethodGet, rawURL, ArticleReferer)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	buf := make([]byte, 512)
	n, err := io.ReadFull(resp.Body, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return 0, ct, err
	}
	if n == 0 {
		return 0, ct, errors.New("服务器返回空文件")
	}
	if looksLikeHTML(buf[:n]) {
		return 0, ct, errors.New("服务器返回 HTML（防盗链或链接失效）")
	}
	if _, err := w.Write(buf[:n]); err != nil {
		return 0, ct, err
	}
	copied, err := io.Copy(w, resp.Body)
	if err != nil {
		return int64(n) + copied, ct, err
	}
	return int64(n) + copied, ct, nil
}
