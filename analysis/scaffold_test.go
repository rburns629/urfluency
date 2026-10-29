package analysis

import (
	"reflect"
	"sort"
	"testing"
)

func TestScaffolding(t *testing.T) {
	files := map[string]string{
		// A Flutter app: its host projects and untouched starters are generated.
		"app/pubspec.yaml":                   "name: app\ndependencies:\n  flutter:\n    sdk: flutter\n",
		"app/lib/main.dart":                  "// You have pushed the button this many times\n",
		"app/lib/home.dart":                  "class Home {}\n",
		"app/test/widget_test.dart":          "testWidgets('Counter increments smoke test', ...);\n",
		"app/windows/runner/main.cpp":        "int main() {}\n",
		"app/linux/runner/my_application.cc": "",
		"app/android/app/src/main/Main.java": "",
		// A Flutter plugin writes its own native code.
		"plugin/pubspec.yaml":               "name: p\ndependencies:\n  flutter:\n    sdk: flutter\nflutter:\n  plugin:\n    platforms:\n",
		"plugin/android/src/main/Plugin.kt": "",
		"plugin/lib/main.dart":              "void main() {}\n",
		// A Capacitor app's native projects are generated; its web code is not.
		"web/capacitor.config.ts":           "",
		"web/android/app/MainActivity.java": "",
		"web/src/app.ts":                    "",
		// Plugin registrants are generated anywhere.
		"other/ios/Runner/GeneratedPluginRegistrant.m": "",
		// A Dart package that is not Flutter keeps everything.
		"pkg/pubspec.yaml":   "name: pkg\n",
		"pkg/web/index.dart": "",
	}
	var paths []string
	for p := range files {
		paths = append(paths, p)
	}
	read := func(p string) []byte { return []byte(files[p]) }
	got := []string{}
	for p := range scaffolding(paths, read) {
		got = append(got, p)
	}
	sort.Strings(got)
	want := []string{
		"app/android/app/src/main/Main.java", "app/lib/main.dart", "app/linux/runner/my_application.cc",
		"app/test/widget_test.dart", "app/windows/runner/main.cpp",
		"other/ios/Runner/GeneratedPluginRegistrant.m", "web/android/app/MainActivity.java",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scaffolding =\n%v\nwant\n%v", got, want)
	}
}
