package analysis

import (
	"bytes"
	"path"
	"regexp"
	"slices"
	"strings"
)

// Generated project scaffolding is code a framework writes when a project is
// created, which Git then credits to whoever committed it: Flutter's
// desktop and mobile host projects (C++, Java, Swift, Kotlin) and starter
// files, and Capacitor's native projects. It is left out like vendored code.

// flutterPlatformDirs hold a Flutter app's generated host projects. A plugin
// package keeps its own native code there, so they are kept for plugins.
var flutterPlatformDirs = []string{"android", "ios", "linux", "macos", "windows", "web"}

// capacitorConfigs mark a Capacitor project, whose android and ios folders are
// generated native projects.
var capacitorConfigs = []string{"capacitor.config.json", "capacitor.config.ts", "capacitor.config.js"}

// generatedNames are generated wherever they appear.
var generatedNames = map[string]bool{
	"GeneratedPluginRegistrant.java": true, "GeneratedPluginRegistrant.kt": true, "GeneratedPluginRegistrant.swift": true,
	"GeneratedPluginRegistrant.h": true, "GeneratedPluginRegistrant.m": true,
	"generated_plugin_registrant.cc": true, "generated_plugin_registrant.h": true, "generated_plugin_registrant.dart": true,
}

// flutterStarters are Flutter's starter files, recognized by text that stays
// until the starter app is replaced.
var flutterStarters = map[string]string{
	"lib/main.dart":         "You have pushed the button this many times",
	"test/widget_test.dart": "Counter increments smoke test",
}

var (
	flutterSDK    = regexp.MustCompile(`(?m)^\s+sdk:\s*flutter\s*$`)
	flutterPlugin = regexp.MustCompile(`(?m)^\s+plugin:\s*$`)
)

// isScaffoldManifest reports whether a file name marks a project whose
// scaffolding is generated, so a folder scan keeps it for scaffolding.
func isScaffoldManifest(name string) bool {
	return name == "pubspec.yaml" || slices.Contains(capacitorConfigs, name)
}

// scaffolding returns the paths among paths that are generated scaffolding.
// read returns a path's content; it is called only for pubspec.yaml files
// and Flutter's two starter files.
func scaffolding(paths []string, read func(string) []byte) map[string]bool {
	out := map[string]bool{}
	type root struct {
		dir     string // with a trailing slash, or "" for the top
		dirs    []string
		flutter bool
	}
	var roots []root
	for _, p := range paths {
		dir, name := path.Split(p)
		switch {
		case name == "pubspec.yaml":
			if c := read(p); flutterSDK.Match(c) && !flutterPlugin.Match(c) {
				roots = append(roots, root{dir: dir, dirs: flutterPlatformDirs, flutter: true})
			}
		case slices.Contains(capacitorConfigs, name):
			roots = append(roots, root{dir: dir, dirs: []string{"android", "ios"}})
		}
	}
	have := map[string]bool{}
	for _, p := range paths {
		have[p] = true
		if generatedNames[path.Base(p)] {
			out[p] = true
			continue
		}
		for _, r := range roots {
			for _, d := range r.dirs {
				if strings.HasPrefix(p, r.dir+d+"/") {
					out[p] = true
				}
			}
		}
	}
	for _, r := range roots {
		if !r.flutter {
			continue
		}
		for rel, marker := range flutterStarters {
			if p := r.dir + rel; have[p] && bytes.Contains(read(p), []byte(marker)) {
				out[p] = true
			}
		}
	}
	return out
}
