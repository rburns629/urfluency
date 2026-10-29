package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// A Flutter app is one project: its platform folders, which hold their own
// build files, belong to it rather than becoming projects of their own.
func TestFindProjectsKeepsFlutterAppsWhole(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"app/pubspec.yaml":                   "name: app\ndependencies:\n  flutter:\n    sdk: flutter\n",
		"app/lib/main.dart":                  "void main() {}\n",
		"app/windows/CMakeLists.txt":         "project(app)\n",
		"app/windows/runner/main.cpp":        "int main() {}\n",
		"app/linux/runner/my_application.cc": "int f() { return 0; }\n",
		"app/android/build.gradle":           "plugins {}\n",
		"app/android/app/src/main/Main.java": "class Main {}\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repos, folders, err := findProjects(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(root, "app")}; len(repos) != 0 || !reflect.DeepEqual(folders, want) {
		t.Errorf("repos %v, folders %v; want one project, %v", repos, folders, want)
	}
}
