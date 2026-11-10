package wx

import (
	"os"
	"testing"
)

func TestScratchPicture(t *testing.T) {
	b, _ := os.ReadFile("/tmp/wx.html")
	items := pictureItems(string(b))
	t.Logf("picture items=%d", len(items))
	for i, p := range items {
		t.Logf("%d %dx%d %s key=%s", i+1, p.Width, p.Height, p.URL, cdnKey(p.URL))
	}
	c := contentImages(contentRegion(string(b)))
	t.Logf("content=%d", len(c))
	for i, p := range c {
		t.Logf("%d %dx%d %s key=%s", i+1, p.Width, p.Height, p.URL, cdnKey(p.URL))
	}
}
