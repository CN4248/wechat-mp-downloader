package wx

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestDownloaderSkipsIdenticalBytes(t *testing.T) {
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
	defer srv.Close()
	dir := t.TempDir()
	d := &Downloader{Client: NewClient(), OutDir: dir, Jobs: 2, NameTemplate: "<order>.<ext>"}
	medias := []*Media{
		{Seq: 1, URL: srv.URL + "/a.png", Ext: "png", Kind: "image"},
		{Seq: 2, URL: srv.URL + "/b.png", Ext: "png", Kind: "image"},
	}
	res := d.Run(context.Background(), medias)
	ok, skipped := 0, 0
	for _, r := range res {
		switch r.Status {
		case "ok":
			ok++
		case "skipped":
			skipped++
		}
	}
	if ok != 1 || skipped != 1 {
		t.Fatalf("ok=%d skipped=%d", ok, skipped)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("文件数 %d", len(entries))
	}
}
