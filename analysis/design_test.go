package analysis

import (
	"slices"
	"testing"
)

func TestImports(t *testing.T) {
	tests := []struct {
		language, src string
		want          []string
	}{
		{"go", "package x\nimport (\n\t\"cloud.google.com/go/pubsub\"\n\tfiber \"github.com/gofiber/fiber/v2\"\n)\n", []string{"cloud.google.com/go/pubsub", "github.com/gofiber/fiber/v2"}},
		{"python", "import os, requests as r\nfrom google.cloud import bigquery\nfrom . import local\n", []string{"os", "requests", "google.cloud"}},
		{"typescript", "import axios from 'axios'\nimport { x } from \"./local\"\nconst a = require('@google-cloud/storage')\nexport * from 'react-native'\n", []string{"axios", "@google-cloud/storage", "react-native"}},
		{"java", "import static org.junit.Assert.*;\nimport com.google.cloud.pubsub.v1.Publisher;\n", []string{"org.junit.assert", "com.google.cloud.pubsub.v1.publisher"}},
		{"csharp", "using System;\nusing Amazon.S3;\nusing var x = Open();\n", []string{"system", "amazon.s3"}},
		{"rust", "use axum::Router;\npub(crate) use reqwest::Client;\n", []string{"axum::router", "reqwest::client"}},
		{"c", "#include <curl/curl.h>\n#include \"local.h\"\n", []string{"curl/curl.h", "local.h"}},
		{"swift", "@testable import App\nimport SwiftUI\nimport struct Foundation.URL\n", []string{"app", "swiftui", "foundation.url"}},
		{"dart", "import 'package:flutter/material.dart';\nimport 'src/local.dart';\n", []string{"package:flutter/material.dart", "src/local.dart"}},
	}
	for _, tc := range tests {
		got := Imports(tc.language, File{Path: "f", Content: []byte(tc.src)})
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: Imports = %q, want %q", tc.language, got, tc.want)
		}
	}
}

func TestDesignOf(t *testing.T) {
	tests := []struct {
		imp, area, library string
		want               bool
	}{
		{"react-native", "mobile", "react-native", true},
		{"react-native", "frontend", "react", false}, // a prefix needs a separator
		{"react-dom/client", "frontend", "react", true},
		{"google.cloud.storage", "cloud", "gcp", true},
		{"google.cloud.storage", "data", "object-storage", true},
		{"cloud.google.com/go/pubsub", "messaging", "pubsub", true},
		{"@aws-sdk/client-s3", "cloud", "aws", true},
		{"package:firebase_auth/firebase_auth.dart", "auth", "firebase-auth", true},
		{"package:firebase_auth/firebase_auth.dart", "cloud", "firebase", true},
		{"axum::router", "api", "axum", true},
		{"requestsx", "integration", "http-client", false},
		{"github.com/gofiber/fiber/v2", "integration", "github", false},
		{"github", "integration", "github", true},
		{"docker.io/go-docker", "infrastructure", "docker-sdk", false},
	}
	for _, tc := range tests {
		got := DesignOf([]string{tc.imp})[tc.area][tc.library]
		if got != tc.want {
			t.Errorf("DesignOf(%q)[%s][%s] = %v, want %v", tc.imp, tc.area, tc.library, got, tc.want)
		}
	}
}

func TestDesignInContent(t *testing.T) {
	tests := []struct {
		src, library string
		want         bool
	}{
		{"exports.onPub = functions.pubsub.topic('orders').onPublish(handle)", "pubsub", true},
		{"export const onOrder = onMessagePublished('orders', handle)", "pubsub", true},
		{"data = base64.b64decode(envelope['message']['data'])", "pubsub", true},
		{"const payload = Buffer.from(req.body.message.data, 'base64')", "pubsub", true},
		{"type push struct {\n\tSubscription string `json:\"subscription\"`\n}", "pubsub", true},
		{"exports.nightly = functions.pubsub.schedule('every 24 hours')", "pubsub", false}, // a cron job, not messaging
		{"@functions_framework.cloud_event\ndef handle(event):", "cloud-events", true},
		{"functions.CloudEvent(\"Handle\", handle)", "cloud-events", true},
		{"message = {'data': 1}", "pubsub", false},
	}
	for _, tc := range tests {
		if got := designInContent([]byte(tc.src))["messaging"][tc.library]; got != tc.want {
			t.Errorf("designInContent(%q)[messaging][%s] = %v, want %v", tc.src, tc.library, got, tc.want)
		}
	}
}

func TestPHPAndRubyDesign(t *testing.T) {
	php := Imports("php", File{Path: "a.php", Content: []byte("<?php\nnamespace App;\n\nuse Aws\\S3\\S3Client;\nuse GuzzleHttp\\Client;\nuse function Illuminate\\Support\\Facades\\Route;\n")})
	rb := Imports("ruby", File{Path: "a.rb", Content: []byte("require 'aws-sdk-s3'\nrequire \"sidekiq\"\nrequire_relative 'helper'\n")})
	got := DesignOf(append(php, rb...))
	for area, lib := range map[string]string{"cloud": "aws", "integration": "http-client", "messaging": "background-jobs"} {
		if !got[area][lib] {
			t.Errorf("%s/%s not found in %v (imports %v %v)", area, lib, got, php, rb)
		}
	}
	// Standard-library HTTP in other languages is not an integration.
	for _, imp := range []string{"net/http", "http.server", "http"} {
		if DesignOf([]string{imp})["integration"]["http-client"] {
			t.Errorf("%s counted as an HTTP client integration", imp)
		}
	}
}
