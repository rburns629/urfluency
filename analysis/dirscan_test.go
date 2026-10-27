package analysis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanDir(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package a\n\nfunc f() { panic(1) }\n")
	write("node_modules/x/index.js", "eval(input)\n")
	write("skip/b.go", "package b\n\nfunc g() { panic(2) }\n")
	write("secrets/k.go", "package k\n\nfunc h() { panic(3) }\n")
	write("binary.go", "package c\x00\n")
	write("big.go", "package d\n"+strings.Repeat("// filler\n", maxScanBytes/10+1))

	res, err := ScanDir(context.Background(), dir, Options{SkipDir: func(d string) bool { return filepath.Base(d) == "skip" }})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Declared || res.Repos != 1 || len(res.Languages) == 0 {
		t.Fatalf("result = %+v", res)
	}
	goLang := res.Languages[0]
	if goLang.Language != "go" || res.OwnedLines != 3 || goLang.Files != 1 || goLang.OwnedLines != 3 || len(goLang.Findings) != 1 || goLang.Findings[0].Path != "a.go" {
		t.Errorf("go = %+v", goLang)
	}
	if res.AILines != 0 || res.MarkedLines != 0 {
		t.Errorf("history measures set without Git: %+v", res)
	}
}

func TestProjectFiles(t *testing.T) {
	dir := t.TempDir()
	if m, s := ProjectFiles(dir); m || s {
		t.Error("empty folder has project files")
	}
	for _, name := range []string{"requirements.txt", "main.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if m, s := ProjectFiles(dir); !m || !s {
		t.Errorf("ProjectFiles = %v, %v; want manifest and source", m, s)
	}
}
