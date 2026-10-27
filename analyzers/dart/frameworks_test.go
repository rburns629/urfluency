package dart

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
		{`import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import "package:dio/dio.dart" as http;
import 'package:firebase_auth/firebase_auth.dart';
import 'package:cloud_firestore/cloud_firestore.dart';
import 'package:flutter/widgets.dart';
`, []string{"dio", "firebase", "flutter", "riverpod"}},
		{`import 'package:bloc/bloc.dart';
import 'package:provider/provider.dart';
import 'package:get/get.dart';
export 'package:riverpod/riverpod.dart';
`, []string{"bloc", "get", "provider", "riverpod"}},
		{`import 'dart:async';
import 'package:flutterx/flutterx.dart';
import 'src/flutter/widgets.dart';
import 'package:getter/getter.dart';
`, []string{}},
	}
	for _, tt := range tests {
		got := Analyzer{}.Frameworks(analysis.File{Path: "lib/a.dart", Content: []byte(tt.src)})
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Frameworks = %v, want %v", got, tt.want)
		}
	}
}
