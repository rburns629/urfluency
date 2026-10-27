package cpp

import (
	"reflect"
	"testing"

	"github.com/rburns629/urfluency/analysis"
)

func TestFrameworks(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"qt module path", "#include <QtWidgets/QWidget>\n", []string{"qt"}},
		{"qt class header", "#include <QString>\n", []string{"qt"}},
		{"many, sorted and unique", `#include <boost/asio.hpp>
#include "gtest/gtest.h"
#include <gmock/gmock.h>
#include <catch2/catch_test_macros.hpp>
#include "CoreMinimal.h"
#include <opencv2/core.hpp>
#include <Eigen/Dense>
#include <grpcpp/grpcpp.h>
#include "absl/strings/str_cat.h"
#ifdef USE_BOOST
#include <boost/optional.hpp>
#endif
`, []string{"abseil", "boost", "catch2", "eigen", "grpc", "gtest", "opencv", "unreal"}},
		{"standard and local headers", "#include <vector>\n#include <queue>\n#include \"Quaternion.hpp\"\n#include \"widget.h\"\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Analyzer{}.Frameworks(analysis.File{Path: "src/a.cpp", Content: []byte(tt.src)})
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Frameworks = %v, want %v", got, tt.want)
			}
		})
	}
}
