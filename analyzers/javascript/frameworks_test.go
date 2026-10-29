package javascript

import (
	"slices"
	"testing"

	"github.com/rburns629/urfluency/analysis"
)

func TestFrameworks(t *testing.T) {
	src := "import React from 'react';\nimport { View } from 'react-native';\nimport * as Haptics from 'expo-haptics';\n"
	got := JavaScript{}.Frameworks(analysis.File{Path: "App.jsx", Content: []byte(src)})
	if !slices.Contains(got, "react-native") || !slices.Contains(got, "react") {
		t.Errorf("Frameworks = %v, want react and react-native", got)
	}
	web := JavaScript{}.Frameworks(analysis.File{Path: "web.js", Content: []byte("import React from 'react';\n")})
	if got := web; slices.Contains(got, "react-native") {
		t.Errorf("a React web file was detected as React Native: %v", got)
	}
}

func TestFrameworkLibraries(t *testing.T) {
	src := "import Link from 'next/link';\nimport { Module } from '@nestjs/core/injector';\nconst mongoose = require('mongoose');\n" +
		"import x from './vue';\nimport y from '@/lib/prisma';\n"
	got := TypeScript{}.Frameworks(analysis.File{Path: "a.ts", Content: []byte(src)})
	slices.Sort(got)
	want := []string{"mongoose", "nestjs", "nextjs", "react"}
	if !slices.Equal(got, want) {
		t.Errorf("Frameworks = %v, want %v (local imports are not packages)", got, want)
	}
}
