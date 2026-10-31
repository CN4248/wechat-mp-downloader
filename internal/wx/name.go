package wx

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// DefaultNameTemplate mirrors the product requirement:
//
//	<文章标题>-<文章id>-<图片序号>.<扩展名>
const DefaultNameTemplate = "<title>-<post_id>-<order>.<ext>"

// DefaultCollectionTemplate is used for every article of an album.
const DefaultCollectionTemplate = "<collection_title>/<collection_order>-<title>/<post_id>-<order>.<ext>"

// NameToken documents one template variable for the WebUI / README.
type NameToken struct {
	Token   string `json:"token"`
	Alias   string `json:"alias"`
	Desc    string `json:"desc"`
	Example string `json:"example"`
}

// NameTokens is the single source of truth for the WebUI token chips and the
// README table. Chinese aliases are accepted in templates and converted to the
// canonical English token before rendering.
var NameTokens = []NameToken{
	{"<title>", "<文章标题>", "文章标题", "哪里搞错了"},
	{"<post_id>", "<文章id>", "文章短链 id（/s/ 后的串）", "tC1hauHuVvqRusojhhXe5g"},
	{"<order>", "<图片序号>", "图片序号，从 1 开始", "3"},
	{"<ext>", "<扩展名>", "扩展名（不含点）", "png"},
	{"<account>", "<公众号>", "公众号名称", "星空3532"},
	{"<author>", "<作者>", "作者", "星空"},
	{"<date>", "<日期>", "发布日期 YYYY-MM-DD", "2026-10-21"},
	{"<time>", "<时间>", "发布时间 HHMMSS", "194243"},
	{"<year>", "<年>", "发布年份", "2026"},
	{"<month>", "<月>", "发布月份", "10"},
	{"<day>", "<日>", "发布日", "21"},
	{"<kind>", "<类型>", "image / gif / video", "image"},
	{"<width>", "<宽>", "原图宽度", "1254"},
	{"<height>", "<高>", "原图高度", "1254"},
	{"<order2>", "<序号2>", "两位补零序号", "03"},
	{"<biz>", "", "公众号 biz", "Mzg3MTgyMTA4Nw=="},
	{"<sn>", "", "文章 sn", "3c28e0d4..."},
	{"<mid>", "", "文章 mid", "2247523869"},
	{"<idx>", "", "文章 idx", "1"},
	{"<file_id>", "", "微信图片 fileid", "100040218"},
	{"<source>", "", "content / picture / cover", "content"},
	{"<collection_title>", "<合集标题>", "文章所属合集标题", "猫咪"},
	{"<collection_id>", "<合集ID>", "合集 album_id", "3186378941922820100"},
	{"<collection_order>", "<合集内文章序号>", "文章在合集内的序号（从 1 开始）", "106"},
}

var tokenAlias = func() map[string]string {
	m := map[string]string{}
	for _, t := range NameTokens {
		canonical := strings.Trim(t.Token, "<>")
		m[canonical] = canonical
		if t.Alias != "" {
			m[strings.Trim(t.Alias, "<>")] = canonical
		}
	}
	// A few extra spellings people commonly type.
	extra := map[string]string{
		"文章ID": "post_id", "文章Id": "post_id", "postid": "post_id", "id": "post_id",
		"序号": "order", "index": "order", "n": "order", "no": "order",
		"扩展": "ext", "extension": "ext", "type": "ext",
		"公众号名称": "account", "昵称": "account", "nickname": "account",
		"发布时间": "date", "datetime": "date",
		"图片类型": "kind", "format": "kind",
		"图片宽度": "width", "图片高度": "height",
		"文件名": "title",
		"合集名": "collection_title", "合集名称": "collection_title", "album": "collection_title",
		"合集id": "collection_id", "album_id": "collection_id",
		"合集序号": "collection_order", "合集文章序号": "collection_order", "collectionindex": "collection_order",
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}()

var reNameToken = regexp.MustCompile(`<([^<>]+)>`)

// CanonicalToken resolves a token or Chinese alias to its canonical name.
func CanonicalToken(name string) (string, bool) {
	name = strings.TrimSpace(strings.Trim(name, "<>"))
	c, ok := tokenAlias[name]
	return c, ok
}

// ValidateTemplate rejects unknown tokens and empty templates.
func ValidateTemplate(tpl string) error {
	tpl = strings.TrimSpace(tpl)
	if tpl == "" {
		return fmt.Errorf("命名规则不能为空")
	}
	seen := false
	for _, m := range reNameToken.FindAllStringSubmatch(tpl, -1) {
		seen = true
		if _, ok := CanonicalToken(m[1]); !ok {
			return fmt.Errorf("未知变量 <%s>", m[1])
		}
	}
	if !seen {
		return fmt.Errorf("命名规则至少需要一个变量，例如 %s", DefaultNameTemplate)
	}
	return nil
}

// HasExtToken reports whether the template already places the extension.
func HasExtToken(tpl string) bool {
	for _, m := range reNameToken.FindAllStringSubmatch(tpl, -1) {
		if c, ok := CanonicalToken(m[1]); ok && c == "ext" {
			return true
		}
	}
	return false
}

func tokenValue(m *Media, canonical, ext string) string {
	switch canonical {
	case "title":
		return m.Title
	case "post_id":
		return m.PostID
	case "order":
		return strconv.Itoa(m.Seq)
	case "order2":
		return fmt.Sprintf("%02d", m.Seq)
	case "ext":
		return ext
	case "account":
		return m.Account
	case "author":
		return m.Author
	case "date":
		if m.PublishTime <= 0 {
			return ""
		}
		return m.publishedAt().Format("2006-01-02")
	case "time":
		if m.PublishTime <= 0 {
			return ""
		}
		return m.publishedAt().Format("150405")
	case "year":
		if m.PublishTime <= 0 {
			return ""
		}
		return m.publishedAt().Format("2006")
	case "month":
		if m.PublishTime <= 0 {
			return ""
		}
		return m.publishedAt().Format("01")
	case "day":
		if m.PublishTime <= 0 {
			return ""
		}
		return m.publishedAt().Format("02")
	case "kind":
		return m.Kind
	case "width":
		return strconv.Itoa(m.Width)
	case "height":
		return strconv.Itoa(m.Height)
	case "biz":
		return m.Biz
	case "sn":
		return m.SN
	case "mid":
		return m.Mid
	case "idx":
		return m.Idx
	case "file_id":
		return m.FileID
	case "source":
		return m.Source
	case "collection_title":
		return m.CollectionTitle
	case "collection_id":
		return m.CollectionID
	case "collection_order":
		if m.CollectionOrder > 0 {
			return strconv.Itoa(m.CollectionOrder)
		}
		return ""
	}
	return ""
}

// renderSegment substitutes every token and sanitizes the resulting path
// segment. Token values that contain path separators are neutralised so a
// malicious article title cannot escape the download root.
func renderSegment(m *Media, seg, ext string) string {
	out := reNameToken.ReplaceAllStringFunc(seg, func(tok string) string {
		name := strings.Trim(tok, "<>")
		c, ok := CanonicalToken(name)
		if !ok {
			return "_"
		}
		return tokenValue(m, c, ext)
	})
	return sanitizeSegment(out)
}

// BuildRelPath renders the template into a slash-separated relative path.
// Literal "/" (and "\") in the template create sub-directories.
func BuildRelPath(m *Media, tpl string) (string, error) {
	tpl = strings.TrimSpace(tpl)
	if tpl == "" {
		tpl = DefaultNameTemplate
	}
	ext := normalizeExt(m.Ext)
	rawSegs := strings.FieldsFunc(tpl, func(r rune) bool { return r == '/' || r == '\\' })
	segs := make([]string, 0, len(rawSegs))
	for _, raw := range rawSegs {
		s := renderSegment(m, raw, ext)
		if s == "" || s == "_" && strings.TrimSpace(raw) == "" {
			continue
		}
		segs = append(segs, s)
	}
	if len(segs) == 0 {
		return "", fmt.Errorf("命名规则渲染结果为空")
	}
	last := segs[len(segs)-1]
	if !HasExtToken(tpl) && ext != "" && !strings.HasSuffix(strings.ToLower(last), "."+ext) {
		last += "." + ext
		segs[len(segs)-1] = last
	}
	rel := strings.Join(segs, "/")
	if len([]rune(rel)) > 240 {
		return "", fmt.Errorf("命名结果过长（>240 字符），请缩短模板")
	}
	return rel, nil
}

// BuildAbsPath joins the rendered relative path to outDir and guarantees the
// result stays inside outDir.
func BuildAbsPath(outDir string, m *Media, tpl string) (string, error) {
	rel, err := BuildRelPath(m, tpl)
	if err != nil {
		return "", err
	}
	root, err := filepath.Abs(outDir)
	if err != nil {
		return "", err
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	relCheck, err := filepath.Rel(root, abs)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("命名结果越界：%s", rel)
	}
	return abs, nil
}

func sanitizeSegment(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '<' || r == '>' || r == ':' || r == '"' || r == '|' || r == '?' || r == '*' ||
			r == '/' || r == '\\' || r < 0x20 || r == 0x7f:
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimRight(b.String(), " .")
	if out == "" || out == "." || out == ".." {
		out = "_"
	}
	if len([]rune(out)) > 120 {
		out = string([]rune(out)[:120])
		out = strings.TrimRight(out, " .")
		if out == "" {
			out = "_"
		}
	}
	if isWindowsReserved(out) {
		out = "_" + out
	}
	return out
}

func isWindowsReserved(name string) bool {
	base := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}
	return false
}

// normalizeExt maps WeChat/HTTP format aliases to a short file extension.
func normalizeExt(ext string) string {
	e := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
	switch e {
	case "jpeg", "jpg", "image/jpeg":
		return "jpg"
	case "png", "image/png":
		return "png"
	case "gif", "image/gif":
		return "gif"
	case "webp", "image/webp":
		return "webp"
	case "bmp":
		return "bmp"
	case "mp4", "video/mp4":
		return "mp4"
	case "m4a":
		return "m4a"
	case "":
		return "bin"
	}
	if len(e) > 8 || strings.ContainsAny(e, `/\<>:"|?*`) {
		return "bin"
	}
	return e
}

// PreviewName renders a template for documentation / UI preview.
func PreviewName(m *Media, tpl string) string {
	if p, err := BuildRelPath(m, tpl); err == nil {
		return p
	}
	return ""
}

// TokenExamples returns example values for the WebUI preview table.
func TokenExamples(m *Media) map[string]string {
	ext := normalizeExt(m.Ext)
	out := map[string]string{}
	for _, t := range NameTokens {
		c := strings.Trim(t.Token, "<>")
		v := tokenValue(m, c, ext)
		out[c] = v
		if t.Alias != "" {
			out[strings.Trim(t.Alias, "<>")] = v
		}
	}
	return out
}
