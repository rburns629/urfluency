package swift

import (
	"reflect"
	"testing"

	"github.com/rburns629/urfluency/analysis"
)

func TestFrameworks(t *testing.T) {
	tests := []struct {
		src  string
		want []string
	}{
		{"import SwiftUI\nimport Combine\nimport SwiftUI\n", []string{"combine", "swiftui"}},
		{"import UIKit.UIView\nimport CoreData\nimport Foundation\n", []string{"coredata", "uikit"}},
		{"import AppKit\nimport SwiftData\nimport Vapor\n", []string{"appkit", "swiftdata", "vapor"}},
		{"import XCTest\n@testable import App\nimport Testing\n", []string{"swift-testing", "xctest"}},
		{"import Foundation\nlet swiftUI = 1\n", []string{}},
	}
	for _, tt := range tests {
		got := Analyzer{}.Frameworks(analysis.File{Path: "A.swift", Content: []byte(tt.src)})
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Frameworks(%q) = %v, want %v", tt.src, got, tt.want)
		}
	}
}
