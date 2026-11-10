package wx

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Result is the outcome of one media item.
type Result struct {
	Media   *Media `json:"media"`
	RelPath string `json:"rel_path"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	MD5     string `json:"md5,omitempty"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Status  string `json:"status"` // ok | skipped | failed | canceled
	Err     error  `json:"-"`
	Error   string `json:"error,omitempty"`
	Warning string `json:"warning,omitempty"`
}

// Event is emitted after each item finishes.
type Event struct {
	Done   int     `json:"done"`
	Total  int     `json:"total"`
	Result *Result `json:"result"`
}

// Downloader downloads media concurrently.
type Downloader struct {
	Client       *Client
	OutDir       string
	Jobs         int
	Force        bool
	NameTemplate string
	OnEvent      func(Event)

	mu      sync.Mutex
	used    map[string]bool
	digests map[string]*Result
	done    int
	total   int
}

// Run downloads all medias and returns results in input order.
func (d *Downloader) Run(ctx context.Context, medias []*Media) []*Result {
	if d.Jobs <= 0 {
		d.Jobs = 4
	}
	if d.Jobs > 16 {
		d.Jobs = 16
	}
	if d.OutDir == "" {
		d.OutDir = "downloads"
	}
	if d.NameTemplate == "" {
		d.NameTemplate = DefaultNameTemplate
	}
	_ = os.MkdirAll(d.OutDir, 0o755)
	d.mu.Lock()
	d.used = map[string]bool{}
	d.digests = map[string]*Result{}
	d.done = 0
	d.total = len(medias)
	d.mu.Unlock()

	results := make([]*Result, len(medias))
	sem := make(chan struct{}, d.Jobs)
	var wg sync.WaitGroup
	for i, m := range medias {
		wg.Add(1)
		go func(idx int, media *Media) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = &Result{Media: media, Status: "canceled", Err: ctx.Err(), Error: "已取消"}
				return
			}
			r := d.downloadOne(ctx, media)
			results[idx] = r
			d.mu.Lock()
			d.done++
			done, total := d.done, d.total
			d.mu.Unlock()
			if d.OnEvent != nil {
				d.OnEvent(Event{Done: done, Total: total, Result: r})
			}
		}(i, m)
	}
	wg.Wait()
	return results
}

func (d *Downloader) downloadOne(ctx context.Context, m *Media) *Result {
	res := &Result{Media: m, Status: "failed"}
	ext := normalizeExt(m.Ext)
	m.Ext = ext
	declaredW, declaredH := m.Width, m.Height

	abs, err := BuildAbsPath(d.OutDir, m, d.NameTemplate)
	if err != nil {
		res.Err = err
		res.Error = err.Error()
		return res
	}
	if !d.Force {
		if st, statErr := os.Stat(abs); statErr == nil && st.Size() > 0 {
			res.Status = "skipped"
			res.Path = abs
			res.Size = st.Size()
			res.RelPath = d.relPath(abs)
			res.Width, res.Height = m.Width, m.Height
			return res
		}
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		res.Err = err
		res.Error = "创建目录失败：" + err.Error()
		return res
	}
	target := d.claim(abs)
	tmp := target + ".part"
	_ = os.Remove(tmp)

	var (
		size    int64
		lastErr error
		sum     string
	)
	for attempt := 1; attempt <= 3; attempt++ {
		if ctx.Err() != nil {
			res.Status = "canceled"
			res.Err = ctx.Err()
			res.Error = "已取消"
			_ = os.Remove(tmp)
			return res
		}
		f, createErr := os.Create(tmp)
		if createErr != nil {
			lastErr = createErr
			break
		}
		h := md5.New()
		n, _, dlErr := d.Client.StreamTo(ctx, m.URL, io.MultiWriter(f, h))
		closeErr := f.Close()
		if dlErr == nil && closeErr != nil {
			dlErr = closeErr
		}
		if dlErr == nil && n <= 0 {
			dlErr = fmt.Errorf("空文件")
		}
		if dlErr == nil {
			size = n
			sum = hex.EncodeToString(h.Sum(nil))
			break
		}
		lastErr = dlErr
		_ = os.Remove(tmp)
		if attempt < 3 {
			select {
			case <-ctx.Done():
			case <-time.After(time.Duration(attempt) * 700 * time.Millisecond):
			}
		}
	}
	if size <= 0 {
		res.Err = lastErr
		if lastErr == nil {
			res.Error = "下载失败"
		} else {
			res.Error = lastErr.Error()
		}
		return res
	}

	// Inspect the real format / dimensions, then fix the extension and the
	// final path when the server returned a different format than declared.
	actualExt, w, hgt := probeFile(tmp)
	if actualExt != "" && actualExt != m.Ext {
		if m.Kind == "video" {
			res.Warning = fmt.Sprintf("服务器返回 %s，与声明 %s 不一致", actualExt, m.Ext)
		} else {
			res.Warning = fmt.Sprintf("实际格式 %s，已按真实格式纠正扩展名", actualExt)
			m.Ext = actualExt
			if actualExt == "gif" {
				m.IsGIF = true
				m.Kind = "gif"
			}
		}
	}
	if w > 0 && hgt > 0 {
		m.Width, m.Height = w, hgt
		res.Width, res.Height = w, hgt
	} else {
		res.Width, res.Height = m.Width, m.Height
	}
	if m.IsGIF && actualExt != "" && actualExt != "gif" {
		res.Warning = strings.TrimSpace(res.Warning + fmt.Sprintf("；声明为动图但实际是 %s", actualExt))
	}

	finalAbs := target
	if (actualExt != "" && actualExt != ext) || w != declaredW || hgt != declaredH {
		if abs2, buildErr := BuildAbsPath(d.OutDir, m, d.NameTemplate); buildErr == nil && abs2 != target {
			d.release(target)
			if !d.Force {
				if st, statErr := os.Stat(abs2); statErr == nil && st.Size() > 0 {
					_ = os.Remove(tmp)
					res.Status = "skipped"
					res.Path = abs2
					res.Size = st.Size()
					res.RelPath = d.relPath(abs2)
					return res
				}
			}
			if mkErr := os.MkdirAll(filepath.Dir(abs2), 0o755); mkErr == nil {
				finalAbs = d.claim(abs2)
			}
		}
	}
	if !d.Force {
		if st, statErr := os.Stat(finalAbs); statErr == nil && st.Size() > 0 {
			_ = os.Remove(tmp)
			res.Status = "skipped"
			res.Path = finalAbs
			res.Size = st.Size()
			res.RelPath = d.relPath(finalAbs)
			return res
		}
	}
	if d.Force {
		_ = os.Remove(finalAbs)
	}
	if err := os.Rename(tmp, finalAbs); err != nil {
		// Windows cannot replace an existing target atomically.
		_ = os.Remove(finalAbs)
		if err2 := os.Rename(tmp, finalAbs); err2 != nil {
			_ = os.Remove(tmp)
			res.Err = err2
			res.Error = "写入文件失败：" + err2.Error()
			return res
		}
	}
	// 同一篇文章里，不同 CDN 地址可能返回完全相同的字节（例如正文与图集
	// 的镜像图）。按 MD5 去重，避免用户看到“后三张和前三张一模一样”。
	d.mu.Lock()
	if first, dup := d.digests[sum]; dup {
		d.mu.Unlock()
		_ = os.Remove(finalAbs)
		d.release(finalAbs)
		res.Status = "skipped"
		res.Warning = fmt.Sprintf("与第 %d 项内容相同，已跳过重复文件", first.Media.Seq)
		res.Path = first.Path
		res.RelPath = first.RelPath
		res.Size = size
		res.MD5 = sum
		return res
	}
	d.digests[sum] = res
	d.mu.Unlock()

	res.Status = "ok"
	res.Path = finalAbs
	res.Size = size
	res.MD5 = sum
	res.RelPath = d.relPath(finalAbs)
	return res
}

func (d *Downloader) relPath(abs string) string {
	root, err := filepath.Abs(d.OutDir)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}

func (d *Downloader) claim(abs string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	ext := filepath.Ext(abs)
	stem := strings.TrimSuffix(abs, ext)
	cand := abs
	for i := 2; ; i++ {
		key := strings.ToLower(cand)
		if !d.used[key] {
			if st, err := os.Stat(cand); err == nil && st.Size() > 0 && !d.Force {
				cand = fmt.Sprintf("%s-%d%s", stem, i, ext)
				continue
			}
			d.used[key] = true
			return cand
		}
		cand = fmt.Sprintf("%s-%d%s", stem, i, ext)
	}
}

func (d *Downloader) release(abs string) {
	d.mu.Lock()
	delete(d.used, strings.ToLower(abs))
	d.mu.Unlock()
}

// ---------- format probing ----------

func probeFile(path string) (string, int, int) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, 0
	}
	defer f.Close()
	head := make([]byte, 32)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	ext := extFromMagic(head)

	switch ext {
	case "jpg", "png", "gif":
		if _, err := f.Seek(0, io.SeekStart); err == nil {
			if cfg, _, decErr := image.DecodeConfig(f); decErr == nil {
				return ext, cfg.Width, cfg.Height
			}
		}
	case "webp":
		w, h := webpSize(f)
		return ext, w, h
	}
	return ext, 0, 0
}

func extFromMagic(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "jpg"
	case len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return "png"
	case len(b) >= 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a"):
		return "gif"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "webp"
	case len(b) >= 12 && string(b[4:8]) == "ftyp":
		return "mp4"
	case len(b) >= 4 && b[0] == 0x1A && b[1] == 0x45 && b[2] == 0xDF && b[3] == 0xA3:
		return "webm"
	case len(b) >= 3 && string(b[:3]) == "ID3":
		return "mp3"
	}
	return ""
}

// webpSize parses the VP8X / VP8 / VP8L headers without external packages.
func webpSize(f *os.File) (int, int) {
	buf := make([]byte, 30)
	if _, err := f.Seek(12, io.SeekStart); err != nil {
		return 0, 0
	}
	n, _ := io.ReadFull(f, buf)
	if n < 10 {
		return 0, 0
	}
	chunk := string(buf[0:4])
	switch chunk {
	case "VP8X":
		if n < 18 {
			return 0, 0
		}
		w := int(buf[12]) | int(buf[13])<<8 | int(buf[14])<<16
		h := int(buf[15]) | int(buf[16])<<8 | int(buf[17])<<16
		return w + 1, h + 1
	case "VP8 ":
		if n < 20 {
			return 0, 0
		}
		w := (int(buf[14]) | int(buf[15])<<8) & 0x3FFF
		h := (int(buf[16]) | int(buf[17])<<8) & 0x3FFF
		return w, h
	case "VP8L":
		if n < 15 {
			return 0, 0
		}
		bits := uint32(buf[9]) | uint32(buf[10])<<8 | uint32(buf[11])<<16 | uint32(buf[12])<<24
		w := int(bits&0x3FFF) + 1
		h := int((bits>>14)&0x3FFF) + 1
		return w, h
	}
	return 0, 0
}

// ---------- misc ----------

// ParseSize is a tiny helper used by CLI/WebUI tests.
func ParseSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
