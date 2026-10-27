package gates

import (
	"reflect"
	"testing"
)

func TestRun(t *testing.T) {
	tests := map[string][]string{
		"go test ./... && go vet ./...":                   {Lint, Tests},
		"cargo fmt --check && cargo clippy && cargo test": {Format, Lint, Tests},
		"dotnet build && dotnet test":                     {Build, Tests},
		"./gradlew build && ./gradlew test":               {Build, Tests},
		"mvn verify":                                      {Build, Tests},
		"flutter analyze && flutter test":                 {Lint, Tests},
		"swiftlint && swift test":                         {Lint, Tests},
		"clang-tidy src/*.cc && ctest":                    {Lint, Tests},
		"cargo audit":                                     {Security},
		"npx prettier --check . && npx tsc":               {Format, Types},
		"pnpm run build":                                  {Build},
		"pip-audit -r requirements.txt":                   {Security},
		"make check":                                      {Other},
		"cat main.go":                                     nil,
		"ruff check . && ruff format --check":             {Format, Lint},
	}
	for in, want := range tests {
		if got := Run(in); !reflect.DeepEqual(got, want) {
			t.Errorf("Run(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestImplied(t *testing.T) {
	got := Implied("go", map[string]bool{Tests: true})
	if !got[Types] || !got[Build] || !got[Tests] {
		t.Errorf("go tests should imply types and build: %v", got)
	}
	if got := Implied("typescript", map[string]bool{Tests: true}); got[Types] {
		t.Error("TypeScript tests do not imply type checking")
	}
}

func TestSet(t *testing.T) {
	s := Set(0).With(Tests, Lint)
	if !s.Has(Tests) || !s.Has(Lint) || s.Has(Types) || s.Has("unknown") {
		t.Errorf("set = %b", s)
	}
	if s.With(Tests) != s {
		t.Error("adding a present gate changed the set")
	}
}
