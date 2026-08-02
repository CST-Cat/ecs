package app

import (
	"os"
	"path/filepath"
	"testing"
)

// 提交库按月份分子目录存放，只扫一层会什么都找不到——CI 上就是这么红的。
func TestExpandReportPathsRecursesIntoSubdirectories(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "2026-08")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, "top.json"),
		filepath.Join(nested, "a.json"),
		filepath.Join(nested, "b.json"),
		filepath.Join(root, "README.md"),
	} {
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// 隐藏目录不该被收进来。
	hidden := filepath.Join(root, ".git")
	_ = os.MkdirAll(hidden, 0o755)
	_ = os.WriteFile(filepath.Join(hidden, "c.json"), []byte("{}"), 0o600)

	got := expandReportPaths([]string{root})
	if len(got) != 3 {
		t.Fatalf("应收集到 3 个 json（含子目录、不含隐藏目录与非 json），得到 %d 个：%v", len(got), got)
	}
	for _, path := range got {
		if filepath.Ext(path) != ".json" {
			t.Errorf("收集到非 json 文件：%s", path)
		}
		if filepath.Base(filepath.Dir(path)) == ".git" {
			t.Errorf("收集到隐藏目录里的文件：%s", path)
		}
	}
}

func TestExpandReportPathsAcceptsExplicitFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "one.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := expandReportPaths([]string{path}); len(got) != 1 || got[0] != path {
		t.Fatalf("显式文件应原样收集：%v", got)
	}
	if got := expandReportPaths([]string{filepath.Join(root, "missing.json")}); len(got) != 0 {
		t.Fatalf("不存在的路径应被忽略：%v", got)
	}
}
