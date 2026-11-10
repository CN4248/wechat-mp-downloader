package webui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJobHistoryPersistsAndRecoversRunning(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "jobs.json")
	raw := `[
	  {"id":"done-1","status":"done","urls":["https://mp.weixin.qq.com/s/a"],"out_dir":"/out","total":2,"done":2,"success":2,"started_at":100,"finished_at":200},
	  {"id":"running-1","status":"running","urls":["https://mp.weixin.qq.com/s/b"],"out_dir":"/out","total":3,"done":1,"started_at":300}
	]`
	if err := os.WriteFile(file, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(nil, "/out", "", "", dir)
	v, ok := m.Get("done-1")
	if !ok || v.Status != "done" || v.Success != 2 {
		t.Fatalf("done job not restored: %#v %v", v, ok)
	}
	v, ok = m.Get("running-1")
	if !ok || v.Status != "failed" || v.Error == "" {
		t.Fatalf("running job should become failed: %#v %v", v, ok)
	}
	if got := len(m.List()); got != 2 {
		t.Fatalf("List()=%d, want 2", got)
	}

	// NewManager 重新读盘仍能看到修复后的失败任务。
	m2 := NewManager(nil, "/out", "", "", dir)
	if v, ok := m2.Get("running-1"); !ok || v.Status != "failed" {
		t.Fatalf("reloaded job missing: %#v %v", v, ok)
	}
}

func TestJobHistorySurvivesRemember(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(nil, "/out", "", "", dir)
	m.remember(&JobView{ID: "j1", Status: "done", URLs: []string{"u"}, OutDir: "/out", Total: 1, Success: 1, StartedAt: 10})
	m2 := NewManager(nil, "/out", "", "", dir)
	v, ok := m2.Get("j1")
	if !ok || v.Status != "done" || v.Total != 1 {
		t.Fatalf("remember/load failed: %#v %v", v, ok)
	}
}
