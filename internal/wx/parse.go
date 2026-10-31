package wx

import (
	"fmt"
	stdhtml "html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var cst = time.FixedZone("CST", 8*3600)

// Media is one downloadable item plus all metadata needed for naming.
type Media struct {
	Seq        int    `json:"seq"`
	Kind       string `json:"kind"` // image | gif | video
	URL        string `json:"url"`  // original (/0)
	PreviewURL string `json:"preview_url"`
	Ext        string `json:"ext"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	IsGIF      bool   `json:"is_gif"`
	Source     string `json:"source"` // content | picture | cover
	FileID     string `json:"file_id,omitempty"`

	Title       string `json:"title"`
	PostID      string `json:"post_id"`
	Account     string `json:"account"`
	Author      string `json:"author"`
	Biz         string `json:"biz"`
	SN          string `json:"sn"`
	Mid         string `json:"mid"`
	Idx         string `json:"idx"`
	PublishTime int64  `json:"publish_time"`
	ArticleURL  string `json:"article_url"`

	CollectionID    string `json:"collection_id,omitempty"`
	CollectionTitle string `json:"collection_title,omitempty"`
	CollectionOrder int    `json:"collection_order,omitempty"`
}

func (m *Media) publishedAt() time.Time { return time.Unix(m.PublishTime, 0).In(cst) }

// Article is the parse result for one URL.
type Article struct {
	URL          string   `json:"url"`
	FinalURL     string   `json:"final_url"`
	Title        string   `json:"title"`
	Account      string   `json:"account"`
	Author       string   `json:"author"`
	PostID       string   `json:"post_id"`
	Biz          string   `json:"biz"`
	SN           string   `json:"sn"`
	Mid          string   `json:"mid"`
	Idx          string   `json:"idx"`
	PublishTime  int64    `json:"publish_time"`
	CoverURL     string   `json:"cover_url"`
	Medias       []*Media `json:"medias"`
	Warnings     []string `json:"warnings"`
	VideoCount   int      `json:"video_count"`
	ContentCount int      `json:"content_count"`
	PictureCount int      `json:"picture_count"`

	CollectionID    string `json:"collection_id,omitempty"`
	CollectionTitle string `json:"collection_title,omitempty"`
	CollectionOrder int    `json:"collection_order,omitempty"`
}

// ParseOptions controls optional media classes.
type ParseOptions struct {
	IncludeCover bool
	IncludeVideo bool
}

// ParseHTML turns an article page into an Article. It is deliberately pure so
// it can be unit-tested with a saved fixture.
func ParseHTML(page, finalURL string, opts ParseOptions) *Article {
	a := &Article{URL: finalURL, FinalURL: finalURL}
	a.Title = cleanText(firstNonEmpty(
		jsVar(page, "msg_title"),
		metaContent(page, "og:title"),
		htmlTitle(page),
	))
	a.Account = cleanText(firstNonEmpty(jsVar(page, "nickname"), jsVar(page, "user_name")))
	a.Author = cleanText(firstNonEmpty(jsVar(page, "author"), jsVar(page, "appmsg_author")))
	a.PostID = firstNonEmpty(ArticleIDFromURL(finalURL), ArticleIDFromURL(a.URL), jsVar(page, "appmsgid"), jsVar(page, "mid"))
	a.Biz = jsVar(page, "biz")
	a.SN = jsVar(page, "sn")
	a.Mid = firstNonEmpty(jsVar(page, "mid"), jsVar(page, "appmsgid"))
	a.Idx = jsVar(page, "idx")
	a.CoverURL = normalizeResourceURL(firstNonEmpty(jsVar(page, "msg_cdn_url"), metaContent(page, "og:image")))
	if ts, err := strconv.ParseInt(strings.TrimSpace(firstNonEmpty(jsVar(page, "ct"), jsVar(page, "create_time"))), 10, 64); err == nil {
		a.PublishTime = ts
	}
	if a.Title == "" {
		a.Title = "未命名文章"
	}
	if a.PostID == "" {
		a.PostID = "unknown"
	}

	used := map[string]bool{}
	add := func(m *Media) bool {
		key := cdnKey(m.URL)
		if key == "" || used[key] {
			return false
		}
		used[key] = true
		m.Title = a.Title
		m.PostID = a.PostID
		m.Account = a.Account
		m.Author = a.Author
		m.Biz = a.Biz
		m.SN = a.SN
		m.Mid = a.Mid
		m.Idx = a.Idx
		m.PublishTime = a.PublishTime
		m.ArticleURL = a.URL
		a.Medias = append(a.Medias, m)
		return true
	}

	content := contentRegion(page)
	var contentDims [][2]int
	contentRaw := 0
	for _, in := range contentImages(content) {
		raw := in.URL
		ext := detectExtFromURL(raw, in.Type)
		m := &Media{
			Kind:       kindFor(ext, raw),
			URL:        OriginalURL(raw, ext),
			PreviewURL: ResizeURL(raw, "640"),
			Ext:        ext,
			Width:      in.Width,
			Height:     in.Height,
			Source:     "content",
			IsGIF:      isGIF(raw, ext),
		}
		if m.Width > 0 && m.Height > 0 && m.Width < 50 && m.Height < 50 {
			continue
		}
		contentRaw++
		if add(m) && m.Width > 0 && m.Height > 0 {
			contentDims = append(contentDims, [2]int{m.Width, m.Height})
		}
	}

	pics := pictureItems(page)
	pictureRaw := 0
	// 图集列表有时和正文是同一组图，但 CDN 前缀/文件 hash 不同；当数量与
	// 尺寸逐项一致时，视为正文的镜像，整组忽略，避免“3 张变 6 张”。
	// 有的页面正文 3 张、图集却是 6 条（原图 + 水印图成对出现），
	// 尺寸按 3 条循环重复，同样视为镜像。
	mirror := len(contentDims) > 0 && (len(pics) == len(contentDims) || len(pics) == 2*len(contentDims))
	mirrorIdx := func(i int) int {
		if len(pics) == 2*len(contentDims) {
			return i % len(contentDims)
		}
		return i
	}
	mirrorSkipped := 0
	for pi, p := range pics {
		if p.URL == "" {
			continue
		}
		pictureRaw++
		ext := detectExtFromURL(p.URL, "")
		m := &Media{
			Kind:       kindFor(ext, p.URL),
			URL:        OriginalURL(p.URL, ext),
			PreviewURL: ResizeURL(p.URL, "640"),
			Ext:        ext,
			Width:      p.Width,
			Height:     p.Height,
			Source:     "picture",
			IsGIF:      isGIF(p.URL, ext),
		}
		if m.Width > 0 && m.Height > 0 && m.Width < 50 && m.Height < 50 {
			continue
		}
		if mi := mirrorIdx(pi); mirror && mi < len(contentDims) && p.Width == contentDims[mi][0] && p.Height == contentDims[mi][1] {
			if !used[cdnKey(m.URL)] {
				mirrorSkipped++
			}
			continue
		}
		add(m)
	}
	if mirrorSkipped > 0 {
		a.Warnings = append(a.Warnings, fmt.Sprintf("图集与正文重复，已忽略 %d 张", mirrorSkipped))
	}
	a.ContentCount = contentRaw
	a.PictureCount = pictureRaw

	if opts.IncludeCover && a.CoverURL != "" {
		ext := detectExtFromURL(a.CoverURL, "")
		add(&Media{
			Kind:       kindFor(ext, a.CoverURL),
			URL:        OriginalURL(a.CoverURL, ext),
			PreviewURL: ResizeURL(a.CoverURL, "640"),
			Ext:        ext,
			Source:     "cover",
			IsGIF:      isGIF(a.CoverURL, ext),
		})
	}

	if opts.IncludeVideo {
		vids := directVideoURLs(page)
		if len(vids) > 0 {
			a.Warnings = append(a.Warnings, fmt.Sprintf("检测到 %d 个内嵌视频，已加入下载队列（实验性）", len(vids)))
		} else if n := countVideoSnaps(page); n > 0 {
			a.VideoCount = n
			a.Warnings = append(a.Warnings, fmt.Sprintf("检测到 %d 个微信视频（非动图），当前版本未解析播放地址，已跳过", n))
		}
		for _, v := range vids {
			add(&Media{
				Kind: "video", URL: v, PreviewURL: "", Ext: "mp4", Source: "content", IsGIF: false,
			})
		}
	} else if n := countVideoSnaps(page); n > 0 {
		a.VideoCount = n
		a.Warnings = append(a.Warnings, fmt.Sprintf("文章含 %d 个视频，默认不下载；勾选“包含视频”可尝试", n))
	}

	for i, m := range a.Medias {
		m.Seq = i + 1
	}
	if len(a.Medias) == 0 {
		a.Warnings = append(a.Warnings, "没有解析到图片：可能是纯文字文章、图片懒加载未渲染，或需要登录 Cookie")
	}
	return a
}

func kindFor(ext, raw string) string {
	if isGIF(raw, ext) {
		return "gif"
	}
	return "image"
}

func isGIF(raw, ext string) bool {
	if strings.EqualFold(ext, "gif") {
		return true
	}
	low := strings.ToLower(raw)
	return strings.Contains(low, "wx_fmt=gif") || strings.Contains(low, "mmbiz_gif")
}

func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\u200b", "")
	s = strings.ReplaceAll(s, "\u00a0", " ")
	return strings.TrimSpace(s)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ---------- JS variable extraction ----------

var jsVarCache = map[string]*regexp.Regexp{}

func jsVar(page, name string) string {
	re, ok := jsVarCache[name]
	if !ok {
		re = regexp.MustCompile(`(?:var\s+|window\.)` + regexp.QuoteMeta(name) + `\s*=\s*`)
		jsVarCache[name] = re
	}
	// Minified bundles contain assignments such as `var ct=et(r,G)` before the
	// real page metadata block. Only accept values that start like a literal.
	for _, loc := range re.FindAllStringIndex(page, -1) {
		i := loc[1]
		for i < len(page) && (page[i] == ' ' || page[i] == '\t' || page[i] == '\r' || page[i] == '\n') {
			i++
		}
		if i >= len(page) {
			break
		}
		c := page[i]
		if c == '\'' || c == '"' || (c >= '0' && c <= '9') || c == '-' || strings.HasPrefix(page[i:], "htmlDecode(") {
			if v := jsValueAt(page, i); strings.TrimSpace(v) != "" {
				return v
			}
		}
	}
	return ""
}

func jsValueAt(s string, i int) string {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n') {
		i++
	}
	if i >= len(s) {
		return ""
	}
	if s[i] == '\'' || s[i] == '"' {
		raw, _ := parseJSStringAt(s, i)
		return decodeJSText(raw)
	}
	if strings.HasPrefix(s[i:], "htmlDecode(") {
		j := i + len("htmlDecode(")
		for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
			j++
		}
		if j < len(s) && (s[j] == '\'' || s[j] == '"') {
			raw, _ := parseJSStringAt(s, j)
			return decodeJSText(raw)
		}
	}
	// number or bare identifier
	j := i
	for j < len(s) && (s[j] == '-' || s[j] == '.' || (s[j] >= '0' && s[j] <= '9') || (s[j] >= 'a' && s[j] <= 'z') || (s[j] >= 'A' && s[j] <= 'Z') || s[j] == '_') {
		j++
	}
	return s[i:j]
}

// parseJSStringAt parses a JS single/double quoted string starting at s[i] and
// returns its raw body (escapes intact) plus the index after the closing quote.
func parseJSStringAt(s string, i int) (string, int) {
	if i >= len(s) || (s[i] != '\'' && s[i] != '"') {
		return "", i
	}
	q := s[i]
	i++
	start := i
	for i < len(s) {
		if s[i] == '\\' {
			i += 2
			continue
		}
		if s[i] == q {
			return s[start:i], i + 1
		}
		i++
	}
	return s[start:], len(s)
}

// decodeJSText turns \xNN / \uNNNN escapes and HTML entities into text.
func decodeJSText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'x':
				if i+3 < len(s) {
					if n, err := strconv.ParseUint(s[i+2:i+4], 16, 8); err == nil {
						b.WriteByte(byte(n))
						i += 4
						continue
					}
				}
			case 'u':
				if i+5 < len(s) {
					if n, err := strconv.ParseUint(s[i+2:i+6], 16, 32); err == nil {
						b.WriteRune(rune(n))
						i += 6
						continue
					}
				}
			case 'n':
				b.WriteByte('\n')
				i += 2
				continue
			case 'r':
				b.WriteByte('\r')
				i += 2
				continue
			case 't':
				b.WriteByte('\t')
				i += 2
				continue
			case '\\', '\'', '"', '/':
				b.WriteByte(s[i+1])
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	out := b.String()
	for k := 0; k < 3; k++ {
		next := stdhtml.UnescapeString(out)
		if next == out {
			break
		}
		out = next
	}
	return out
}

func htmlUnescape(s string) string { return stdhtml.UnescapeString(s) }

func metaContent(page, prop string) string {
	re := regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	for _, tag := range re.FindAllString(page, -1) {
		attrs := parseAttrs(tag)
		if strings.EqualFold(attrs["property"], prop) || strings.EqualFold(attrs["name"], prop) {
			return attrs["content"]
		}
	}
	return ""
}

func htmlTitle(page string) string {
	re := regexp.MustCompile(`(?is)<title>(.*?)</title>`)
	if m := re.FindStringSubmatch(page); len(m) > 1 {
		t := cleanText(stdhtml.UnescapeString(m[1]))
		t = strings.TrimSuffix(t, "微信公众平台")
		return strings.TrimSpace(t)
	}
	return ""
}

// ---------- content region ----------

var reDivTag = regexp.MustCompile(`(?i)</?div\b[^>]*>`)

func contentRegion(page string) string {
	for _, marker := range []string{`id="js_content"`, `id='js_content'`, `id=js_content`} {
		i := strings.Index(page, marker)
		if i < 0 {
			continue
		}
		gt := strings.IndexByte(page[i:], '>')
		if gt < 0 {
			return ""
		}
		start := i + gt + 1
		depth := 1
		for _, loc := range reDivTag.FindAllStringIndex(page[start:], -1) {
			tok := strings.ToLower(page[start+loc[0] : start+loc[1]])
			if strings.HasPrefix(tok, "</") {
				depth--
			} else {
				depth++
			}
			if depth == 0 {
				return page[start : start+loc[0]]
			}
		}
		return page[start:]
	}
	return ""
}

// ---------- <img> parsing ----------

var (
	reImgTag = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	reAttr   = regexp.MustCompile("([a-zA-Z_:][-a-zA-Z0-9_:.]*)\\s*=\\s*(?:\"([^\"]*)\"|'([^']*)'|([^\\s\"'=<>`]+))")
)

func parseAttrs(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range reAttr.FindAllStringSubmatch(tag, -1) {
		v := m[2]
		if v == "" {
			v = m[3]
		}
		if v == "" {
			v = m[4]
		}
		out[strings.ToLower(m[1])] = stdhtml.UnescapeString(v)
	}
	return out
}

// imgInfo keeps the attributes needed by both the URL list and the size map.
type imgInfo struct {
	URL    string
	Type   string
	Width  int
	Height int
}

func contentImages(content string) []imgInfo {
	var out []imgInfo
	for _, tag := range reImgTag.FindAllString(content, -1) {
		attrs := parseAttrs(tag)
		src := strings.TrimSpace(firstNonEmpty(attrs["data-src"], attrs["data-original"], attrs["src"]))
		if src == "" || strings.HasPrefix(strings.ToLower(src), "data:") {
			continue
		}
		if strings.Contains(strings.ToLower(src), "mmbiz.qlogo.cn") {
			continue
		}
		if strings.Contains(strings.ToLower(src), "wx_fmt=svg") {
			continue
		}
		w, _ := strconv.Atoi(attrs["data-w"])
		if w == 0 {
			w, _ = strconv.Atoi(attrs["width"])
		}
		h, _ := strconv.Atoi(attrs["height"])
		if h == 0 {
			if ratio, _ := strconv.ParseFloat(attrs["data-ratio"], 64); w > 0 && ratio > 0 {
				h = int(float64(w)*ratio + 0.5)
			}
		}
		out = append(out, imgInfo{URL: normalizeResourceURL(src), Type: attrs["data-type"], Width: w, Height: h})
	}
	return out
}

// ---------- picture_page_info_list ----------

type picItem struct {
	URL           string
	Width, Height int
}

func pictureItems(page string) []picItem {
	idx := strings.Index(page, "picture_page_info_list")
	if idx < 0 {
		return nil
	}
	open := strings.IndexByte(page[idx:], '[')
	if open < 0 {
		return nil
	}
	arr := sliceBrackets(page, idx+open, '[', ']')
	objs := topLevelObjects(arr)
	var out []picItem
	for _, obj := range objs {
		flat := flattenNested(obj)
		u := jsField(flat, "cdn_url")
		if u == "" {
			continue
		}
		it := picItem{URL: normalizeResourceURL(decodeJSText(u))}
		it.Width = int(jsNumber(flat, "width"))
		it.Height = int(jsNumber(flat, "height"))
		if it.Width <= 0 {
			it.Width = int(jsNumber(flat, "cdn_url_width"))
		}
		if it.Height <= 0 {
			it.Height = int(jsNumber(flat, "cdn_url_height"))
		}
		out = append(out, it)
	}
	return out
}

// sliceBrackets returns the substring inside the bracket pair starting at s[i].
func sliceBrackets(s string, i int, open, close byte) string {
	if i >= len(s) || s[i] != open {
		return ""
	}
	depth := 0
	var quote byte
	for j := i; j < len(s); j++ {
		c := s[j]
		if quote != 0 {
			if c == '\\' {
				j++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return s[i+1 : j]
			}
		}
	}
	return s[i+1:]
}

// topLevelObjects splits " {..}, {..} " into object substrings at depth 1.
func topLevelObjects(s string) []string {
	var out []string
	depth := 0
	var quote byte
	start := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			depth--
			if depth == 0 && start >= 0 {
				out = append(out, s[start:i+1])
				start = -1
			}
		}
	}
	return out
}

// flattenNested blanks out nested objects/arrays so a top-level field lookup
// cannot accidentally pick up watermark_info.cdn_url.
func flattenNested(obj string) string {
	var b strings.Builder
	depth := 0
	var quote byte
	for i := 0; i < len(obj); i++ {
		c := obj[i]
		if quote != 0 {
			if depth <= 1 {
				b.WriteByte(c)
			}
			if c == '\\' && i+1 < len(obj) {
				if depth <= 1 {
					b.WriteByte(obj[i+1])
				}
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			if depth <= 1 {
				b.WriteByte(c)
			}
			continue
		case '{', '[':
			depth++
			if depth <= 1 {
				b.WriteByte(c)
			}
			continue
		case '}', ']':
			depth--
			if depth <= 1 {
				b.WriteByte(c)
			}
			continue
		}
		if depth <= 1 {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func jsField(flat, name string) string {
	re := regexp.MustCompile(`(?:^|[,{\s])` + regexp.QuoteMeta(name) + `\s*:\s*(?:'([^']*)'|"([^"]*)")`)
	m := re.FindStringSubmatch(flat)
	if len(m) == 0 {
		return ""
	}
	if m[1] != "" {
		return m[1]
	}
	return m[2]
}

func jsNumber(flat, name string) float64 {
	re := regexp.MustCompile(`(?:^|[,{\s])` + regexp.QuoteMeta(name) + `\s*:\s*'?([0-9]+(?:\.[0-9]+)?)`)
	m := re.FindStringSubmatch(flat)
	if len(m) < 2 {
		return 0
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	return v
}

// ---------- URL helpers ----------

var reSizeSegment = regexp.MustCompile(`^\d+$`)

// ResizeURL replaces the trailing size segment of a WeChat CDN URL.
// size is "0" for the original, or e.g. "640" for a preview.
func ResizeURL(raw, size string) string {
	raw = normalizeResourceURL(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	segs := strings.Split(u.Path, "/")
	if len(segs) >= 1 && reSizeSegment.MatchString(segs[len(segs)-1]) {
		segs[len(segs)-1] = size
	}
	u.Path = strings.Join(segs, "/")
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	return u.String()
}

// OriginalURL builds the /0 original-image URL and normalises wx_fmt.
func OriginalURL(raw, ext string) string {
	raw = normalizeResourceURL(raw)
	ext = normalizeExt(ext)
	u, err := url.Parse(ResizeURL(raw, "0"))
	if err != nil {
		return raw
	}
	q := u.Query()
	if ext != "" && ext != "bin" {
		wx := ext
		if wx == "jpg" {
			wx = "jpeg"
		}
		q.Set("wx_fmt", wx)
	}
	q.Del("tp")
	u.RawQuery = q.Encode()
	return u.String()
}

func detectExtFromURL(raw, dataType string) string {
	low := strings.ToLower(raw)
	if i := strings.Index(low, "wx_fmt="); i >= 0 {
		v := low[i+len("wx_fmt="):]
		if j := strings.IndexAny(v, "&#"); j >= 0 {
			v = v[:j]
		}
		switch v {
		case "jpeg", "jpg":
			return "jpg"
		case "png", "gif", "webp", "bmp", "mp4":
			return v
		}
	}
	if dataType != "" {
		switch strings.ToLower(dataType) {
		case "jpeg", "jpg":
			return "jpg"
		case "png", "gif", "webp", "bmp", "mp4":
			return strings.ToLower(dataType)
		}
	}
	for _, kw := range []string{"mmbiz_gif", "mmbiz_png", "mmbiz_jpg", "mmbiz_webp"} {
		if strings.Contains(low, kw) {
			return strings.TrimPrefix(kw, "mmbiz_")
		}
	}
	if u, err := url.Parse(raw); err == nil {
		p := strings.ToLower(u.Path)
		if i := strings.LastIndexByte(p, '.'); i >= 0 {
			e := p[i+1:]
			if _, ok := map[string]bool{"jpg": true, "jpeg": true, "png": true, "gif": true, "webp": true, "mp4": true}[e]; ok {
				return normalizeExt(e)
			}
		}
	}
	return "jpg"
}

// cdnKey deduplicates URLs that differ only by size segment / query.
func cdnKey(raw string) string {
	raw = normalizeResourceURL(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) > 0 && reSizeSegment.MatchString(segs[len(segs)-1]) {
		segs = segs[:len(segs)-1]
	}
	// 微信同一张图会以 mmbiz_jpg/<hash>、sz_mmbiz_jpg/<hash>、.../<hash>/640
	// 等多种形式出现；hash 才是图片身份，用它去重才能避免正文图与
	// picture_page_info_list 重复计数。
	if len(segs) > 0 {
		last := strings.ToLower(segs[len(segs)-1])
		// 必须用完整文件名：微信 CDN 的不同图片经常共享前 33 个字符，
		// 只取前缀会把不同图片误合并（例如同一篇文章 4 张图只剩 1 张）。
		// 同一张图的正文/图集变体只是目录不同，文件名完全一致。
		if len(last) >= 24 {
			return "h:" + last
		}
	}
	return strings.ToLower(u.Host) + "/" + strings.Join(segs, "/")
}

// ---------- video (best effort) ----------

var (
	reVideoSnap = regexp.MustCompile(`(?is)<mp-common-videosnap\b[^>]*>`)
	reMPVideo   = regexp.MustCompile(`(?is)<mpvideo\b[^>]*>`)
	reMP4       = regexp.MustCompile(`https?://[^\s"'<>\\]+?\.mp4[^\s"'<>\\]*`)
)

func countVideoSnaps(page string) int {
	return len(reVideoSnap.FindAllString(page, -1)) + len(reMPVideo.FindAllString(page, -1))
}

func directVideoURLs(page string) []string {
	var out []string
	seen := map[string]bool{}
	for _, tag := range append(reVideoSnap.FindAllString(page, -1), reMPVideo.FindAllString(page, -1)...) {
		attrs := parseAttrs(tag)
		cand := firstNonEmpty(attrs["data-url"], attrs["data-src"], attrs["src"])
		if strings.Contains(cand, ".mp4") {
			u := normalizeResourceURL(cand)
			if !seen[u] {
				seen[u] = true
				out = append(out, u)
			}
			continue
		}
		for _, m := range reMP4.FindAllString(tag, -1) {
			u := normalizeResourceURL(m)
			if !seen[u] {
				seen[u] = true
				out = append(out, u)
			}
		}
	}
	return out
}
