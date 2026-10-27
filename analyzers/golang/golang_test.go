package golang

import (
	"context"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/rburns629/urfluency/analysis"
)

func findings(t *testing.T, path, src string) []string {
	t.Helper()
	got, err := Analyzer{}.Analyze(context.Background(), []analysis.File{{Path: path, Content: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range got {
		if f.Positive {
			continue // idioms are tested separately
		}
		out = append(out, f.Rule+":"+itoa(f.Line))
	}
	sort.Strings(out)
	return out
}

func itoa(n int) string { return string(rune('0'+n/10)) + string(rune('0'+n%10)) }

func TestRules(t *testing.T) {
	tests := []struct {
		name string
		path string
		src  string
		want []string
	}{
		{
			name: "empty case before another case",
			path: "a.go",
			src: `package a
func f(s string) string {
	switch s {
	case "t1":
	case "t2":
		return s
	default:
	}
	return ""
}`,
			want: []string{"go.switch-empty-case:04"},
		},
		{
			name: "empty cases in a tagless switch mean do nothing",
			path: "a.go",
			src: `package a
func f(s string) bool {
	switch {
	case s == "a", s == "b":
	case s == "c":
	default:
		return false
	}
	return true
}`,
		},
		{
			name: "shared case list is fine",
			path: "a.go",
			src: `package a
func f(s string) string {
	switch s {
	case "t1", "t2":
		return s
	}
	return ""
}`,
		},
		{
			name: "decode target declared outside loop",
			path: "a.go",
			src: `package a
import "encoding/json"
type D struct{ A string }
func f(raw [][]byte) []D {
	var d D
	var out []D
	for _, b := range raw {
		json.Unmarshal(b, &d)
		out = append(out, d)
	}
	return out
}`,
			want: []string{"go.decode-target-reuse:08", "go.ignored-error:08"},
		},
		{
			name: "decode target declared or reset inside loop is not reuse",
			path: "a.go",
			src: `package a
import "encoding/json"
type D struct{ A string }
func f(raw [][]byte) {
	var reset D
	for _, b := range raw {
		var d D
		json.Unmarshal(b, &d)
		reset = D{}
		json.Unmarshal(b, &reset)
	}
	for _, v := range raw {
		json.Unmarshal(v, &v)
	}
}`,
			want: []string{"go.ignored-error:08", "go.ignored-error:10", "go.ignored-error:13"},
		},
		{
			name: "discarded constructors, including aliased imports",
			path: "a.go",
			src: `package a
import (
	"errors"
	f "fmt"
)
func g() error {
	f.Errorf("lost %d", 1)
	errors.New("lost")
	f.Println("kept")
	return f.Errorf("kept")
}`,
			want: []string{"go.discarded-result:07", "go.discarded-result:08"},
		},
		{
			name: "unchecked assertions and untyped map signature",
			path: "a.go",
			src: `package a
func g(m map[string]interface{}, n map[string]any) (map[string]any, string) {
	s := m["k"].(string)
	t, ok := n["k"].(string)
	_ = ok
	switch m["v"].(type) {
	}
	return nil, s + t
}`,
			want: []string{"go.unchecked-type-assertion:03", "go.untyped-map-signature:02", "go.untyped-map-signature:02", "go.untyped-map-signature:02"},
		},
		{
			name: "panic in library code but not in init, Must helpers, or main",
			path: "a.go",
			src: `package a
func init() { panic("ok") }
func MustParse() { panic("ok") }
func g() { panic("bad") }`,
			want: []string{"go.panic-outside-main:04"},
		},
		{
			name: "panic and assertions allowed in package main and tests",
			path: "main_test.go",
			src: `package a
func g(x any) { _ = x.(int); panic("ok in tests") }`,
		},
		{
			name: "package-level decode target declared after the function",
			path: "a.go",
			src: `package a
import "encoding/json"
func f(raw [][]byte) {
	for _, b := range raw {
		json.Unmarshal(b, &shared)
	}
}
var shared struct{ A string }`,
			want: []string{"go.decode-target-reuse:05", "go.ignored-error:05"},
		},
		{
			name: "local variable shadowing fmt is not the fmt package",
			path: "a.go",
			src: `package a
import "fmt"
type logger struct{}
func (logger) Errorf(string, ...any) {}
func g() {
	fmt.Println()
	{
		fmt := logger{}
		fmt.Errorf("logged")
	}
}`,
		},
		{
			name: "WaitGroup.Add(n) before a loop that can skip launches",
			path: "a.go",
			src: `package a
import "sync"
func f(items []int) {
	var wg sync.WaitGroup
	wg.Add(len(items))
	for _, it := range items {
		if it < 0 {
			continue
		}
		go func() { defer wg.Done() }()
	}
	wg.Wait()
}`,
			want: []string{"go.waitgroup-add-before-conditional-launch:05"},
		},
		{
			name: "WaitGroup counted per launch, or loop without early exits",
			path: "a.go",
			src: `package a
import "sync"
func g(items []int) {
	wg := &sync.WaitGroup{}
	wg.Add(len(items))
	for _, it := range items {
		switch it {
		case 0:
			break
		}
		go func() { defer wg.Done() }()
	}
	var wg2 sync.WaitGroup
	for range items {
		wg2.Add(1)
		go func() { defer wg2.Done() }()
	}
	wg.Wait()
	wg2.Wait()
}`,
		},
		{
			name: "Add on a WaitGroup the loop's goroutines do not use",
			path: "a.go",
			src: `package a
import "sync"
func worker(wg *sync.WaitGroup) { defer wg.Done() }
func f(n int) {
	var launched, other sync.WaitGroup
	launched.Add(n)
	other.Add(n)
	for i := 0; i < n; i++ {
		if i > 3 {
			break
		}
		go worker(&launched)
	}
}`,
			want: []string{"go.waitgroup-add-before-conditional-launch:06"},
		},
		{
			name: "Add inside goroutine and Done skipped by early return",
			path: "a.go",
			src: `package a
import "sync"
func f(wg *sync.WaitGroup, ok bool) {
	go func() {
		wg.Add(1)
		if !ok {
			return
		}
		wg.Done()
	}()
}`,
			want: []string{"go.waitgroup-add-in-goroutine:05", "go.waitgroup-done-not-deferred:09"},
		},
		{
			name: "return before receiving from an unbuffered channel",
			path: "a.go",
			src: `package a
func work(ch chan int) { ch <- 1 }
func f(fail bool) int {
	ch := make(chan int)
	go work(ch)
	if fail {
		return 0
	}
	return <-ch
}`,
			want: []string{"go.goroutine-send-leak:07"},
		},
		{
			name: "buffered channel or receive before return",
			path: "a.go",
			src: `package a
func g(fail bool) int {
	ch := make(chan int)
	go func() { ch <- 1 }()
	v := <-ch
	if fail {
		return 0
	}
	buf := make(chan int, 1)
	go func() { buf <- 1 }()
	if fail {
		return 0
	}
	return v + <-buf
}`,
		},
		{
			name: "iterator loops that miss either the sentinel or other errors",
			path: "a.go",
			src: `package a
import "google.golang.org/api/iterator"
type it struct{}
func (it) Next() (int, error) { return 0, nil }
func f(i it) error {
	for {
		_, err := i.Next()
		if err != nil {
			break
		}
	}
	for {
		_, err := i.Next()
		if err == iterator.Done {
			break
		}
	}
	for {
		_, err := i.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}`,
			want: []string{"go.iterator-error-handling:06", "go.iterator-error-handling:12"},
		},
		{
			name: "ignored errors from known fallible functions",
			path: "a.go",
			src: `package a
import (
	"encoding/json"
	"strconv"
	"time"
)
func f(b []byte, s string) {
	var v struct{}
	json.Unmarshal(b, &v)
	_ = json.Unmarshal(b, &v)
	n, _ := strconv.Atoi(s)
	t, _ := time.Parse("2006-01-02", s)
	if err := json.Unmarshal(b, &v); err != nil {
		return
	}
	_, _ = n, t
}`,
			want: []string{"go.ignored-error:09", "go.ignored-error:10", "go.ignored-error:11", "go.ignored-error:12"},
		},
		{
			name: "unparsable file is skipped",
			path: "a.go",
			src:  `package a func {`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findings(t, tt.path, tt.src)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSkipGenerated(t *testing.T) {
	gen := analysis.File{Path: "a.go", Content: []byte("// Code generated by x. DO NOT EDIT.\n\npackage a\n")}
	if !(Analyzer{}).Skip(gen) {
		t.Error("generated file not skipped")
	}
	if (Analyzer{}).Skip(analysis.File{Path: "a.go", Content: []byte("package a\n")}) {
		t.Error("ordinary file skipped")
	}
}

func TestReliabilityRules(t *testing.T) {
	src := `package a

import (
	"context"
	"net/http"
	"os"
	"time"
)

func handler(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	go func(ctx context.Context) { _ = context.TODO() }(r.Context())
	_ = ctx
}

func run(ctx context.Context, flush func(context.Context)) {
	select {
	case <-ctx.Done():
		flush(context.Background())
	default:
	}
	if ctx.Err() != nil {
		flush(context.Background())
	}
	// Streaming: no timeout, or a watch would be cut short.
	_ = &http.Client{}
}

func detached() context.Context { return context.Background() }

func readAll(paths []string) error {
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := func() error {
			g, err := os.Open(p)
			if err != nil {
				return err
			}
			defer g.Close()
			return nil
		}(); err != nil {
			return err
		}
	}
	return nil
}

func fetch(urls []string, jobs chan string, work func(string)) *http.Client {
	for j := range jobs {
		go work(j)
	}
	for _, u := range urls {
		go work(u)
	}
	for i := 0; i < 4; i++ {
		go work("")
	}
	sem := make(chan struct{}, 4)
	for _, u := range urls {
		sem <- struct{}{}
		go func(u string) { defer func() { <-sem }(); work(u) }(u)
	}
	_ = &http.Client{Timeout: time.Second}
	_ = http.DefaultClient
	resp, err := http.Get(urls[0])
	if err == nil {
		work(resp.Status)
	}
	return &http.Client{}
}
`
	var got []string
	for _, f := range findings(t, "a.go", src) {
		if strings.HasPrefix(f, "go.context-background") || strings.HasPrefix(f, "go.defer-in-loop") ||
			strings.HasPrefix(f, "go.http-without") || strings.HasPrefix(f, "go.unbounded") {
			got = append(got, f)
		}
	}
	want := []string{
		"go.context-background-in-handler:11", "go.context-background-in-handler:12",
		"go.defer-in-loop:37",
		"go.http-without-timeout:68", "go.http-without-timeout:69", "go.http-without-timeout:73",
		"go.unbounded-goroutines:54",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
	// Test files call httptest servers, so the timeout rule skips them.
	for _, f := range findings(t, "a_test.go", src) {
		if strings.HasPrefix(f, "go.http-without") {
			t.Errorf("timeout rule fired in a test file: %s", f)
		}
	}
}

func TestFrameworks(t *testing.T) {
	src := "package api\n\nimport (\n\t\"github.com/gofiber/fiber/v2\"\n\t\"github.com/gofiber/fiber/v2/middleware/cors\"\n\t\"google.golang.org/grpc\"\n\t\"github.com/gin-gonic/ginx\"\n)\n"
	got := Analyzer{}.Frameworks(analysis.File{Path: "api.go", Content: []byte(src)})
	if !slices.Equal(got, []string{"fiber", "grpc"}) {
		t.Errorf("Frameworks = %v, want [fiber grpc]", got)
	}
}
