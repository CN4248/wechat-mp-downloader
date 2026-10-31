package wx

import (
	"os"
	"testing"
)

func TestScratchArticle(t *testing.T) {
	b, err := os.ReadFile("/tmp/wx.html")
	if err != nil {
		t.Skip(err)
	}
	a := ParseHTML(string(b), "https://mp.weixin.qq.com/s/y9op-1LgaEJ5hEYcFJyFJw", ParseOptions{})
	t.Logf("title=%q account=%q post=%q ct=%d cover=%s", a.Title, a.Account, a.PostID, a.PublishTime, a.CoverURL)
	t.Logf("medias=%d warnings=%v", len(a.Medias), a.Warnings)
	for _, m := range a.Medias {
		t.Logf("%d %s %dx%d %s src=%s url=%s", m.Seq, m.Kind, m.Width, m.Height, m.Ext, m.Source, m.URL)
		t.Logf("   name=%s", PreviewName(m, DefaultNameTemplate))
	}
}
