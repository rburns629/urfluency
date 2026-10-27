package rust

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/rburns629/urfluency/analysis"
)

func findings(t *testing.T, path, src string) []string {
	t.Helper()
	got, err := Analyzer{}.Analyze(context.Background(), []analysis.File{{Path: path, Content: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, f := range got {
		if f.Positive {
			continue // idioms are tested separately
		}
		out = append(out, fmt.Sprintf("%s:%d", f.Rule, f.Line))
	}
	sort.Strings(out)
	return out
}

func TestRules(t *testing.T) {
	tests := []struct {
		name string
		path string
		src  string
		want []string
	}{
		{
			name: "unwrap and expect in library code",
			path: "src/lib.rs",
			src: `pub fn f(a: Option<u8>, m: &Mutex<u8>) -> u8 {
    let x = a.unwrap();
    let y = a.expect("set by caller");
    let z = a.unwrap_or(0);
    let g = m.lock().unwrap();
    let t = parser.expect(Token::Comma);
    x
}
`,
			want: []string{"rust.unwrap-in-library:2", "rust.unwrap-in-library:3"},
		},
		{
			name: "unwrap allowed in tests, main, binaries, and documented panics",
			path: "src/lib.rs",
			src: `/// Returns the value.
///
/// # Panics
///
/// Panics if a is None.
pub fn f(a: Option<u8>) -> u8 { a.unwrap() }

fn main() { run().unwrap(); }

#[cfg(test)]
mod tests {
    #[test]
    fn t() { Some(1).unwrap(); panic!("x"); }
}

#[tokio::test]
async fn standalone() { Some(1).expect("one"); }
`,
			want: []string{},
		},
		{
			name: "unwrap allowed in main.rs, bin, examples, tests dir",
			path: "src/bin/tool.rs",
			src:  "fn run() { a.unwrap(); todo!(); }\n",
			want: []string{},
		},
		{
			name: "panic macros in library code",
			path: "src/lib.rs",
			src: `pub fn f(x: u8) -> u8 {
    if x > 3 { panic!("too big"); }
    if x == 2 { std::panic!("two"); }
    if x == 1 { todo!() }
    unimplemented!();
    unreachable!()
}
/// # Panics
/// When x is zero.
pub fn g(x: u8) { if x == 0 { panic!("zero") } todo!() }
`,
			want: []string{"rust.panic-in-library:10", "rust.panic-in-library:2", "rust.panic-in-library:3", "rust.panic-in-library:4", "rust.panic-in-library:5"},
		},
		{
			name: "unsafe blocks need a SAFETY comment",
			path: "src/lib.rs",
			src: `pub fn f(p: *const u8) -> u8 {
    let a = unsafe { *p };
    // SAFETY: p is valid for reads, checked by the caller.
    let b = unsafe { *p };
    // Safety: p is non-null and aligned
    // because of the check above.
    unsafe { std::ptr::read(p) };
    let c = /* SAFETY: valid */ unsafe { *p };
    let d = compute(
        // SAFETY: valid
        unsafe { *p },
    );
    // unrelated comment
    unsafe { *p }
}
`,
			want: []string{"rust.unsafe-without-safety-comment:14", "rust.unsafe-without-safety-comment:2"},
		},
		{
			name: "shell commands built with format!",
			path: "src/lib.rs",
			src: `use std::process::Command;
pub fn f(name: &str) {
    Command::new("sh").arg("-c").arg(format!("ls {}", name)).output();
    std::process::Command::new("/bin/bash").args(["-c", &format!("echo {name}")]).status();
    Command::new("ls").arg(format!("{}/dir", name)).output();
    Command::new("sh").arg("-c").arg("ls -la").output();
    Command::new("cmd").arg("/C").arg(name).output();
}
`,
			want: []string{"rust.command-shell:3", "rust.command-shell:4"},
		},
		{
			name: "narrowing casts with known source types",
			path: "src/lib.rs",
			src: `pub fn f(big: u64, small: u8, v: &[u8]) {
    let a = big as u32;
    let b = v.len() as u32;
    let c = 1.5 as i32;
    let d = small as u32;
    let e = big as usize;
    let w: i64 = 5;
    let g = w as i16;
    let h = unknown as u8;
    let k = (big as u16) as u8;
    let big = 3;
    let m = big as u8;
}
`,
			want: []string{"rust.lossy-cast:2", "rust.lossy-cast:3", "rust.lossy-cast:4", "rust.lossy-cast:8", "rust.lossy-cast:10", "rust.lossy-cast:10"},
		},
		{
			name: "&Vec, &String, and &PathBuf parameters",
			path: "src/lib.rs",
			src: `pub fn f(a: &Vec<u8>, b: &String, c: &PathBuf, d: &mut Vec<u8>, e: &[u8], f: &str, g: Vec<u8>) {}
impl Visitor for V { fn visit(&self, items: &Vec<u8>) {} }
impl V { fn new(items: &Vec<u8>) -> Self { V } }
`,
			want: []string{"rust.ptr-arg:1", "rust.ptr-arg:1", "rust.ptr-arg:1", "rust.ptr-arg:3"},
		},
		{
			name: "index loops",
			path: "src/lib.rs",
			src: `pub fn f(v: &[u8], w: &[u8]) {
    for i in 0..v.len() { total += v[i]; }
    for i in 0..v.len() { if v[i] < v[i + 1] { swap(); } }
    for i in 0..v.len() { println!("{}", i); }
    for i in 1..v.len() { total += v[i]; }
    for i in 0..w.len() { total += v[i]; }
    for x in v { total += x; }
}
`,
			want: []string{"rust.index-loop:2"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := findings(t, tc.path, tc.src)
			want := append([]string{}, tc.want...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

func TestComplexity(t *testing.T) {
	nested := `fn deep(a: i32, b: i32) -> i32 {
    for i in 0..a {
        if i > b {
            while b > 0 {
                if a && b || c {
                    match i { 1 => {}, _ => {} }
                } else if b == 2 {
                    loop { break; }
                } else {
                    return 1;
                }
            }
        }
    }
    0
}
`
	simple := "fn simple(a: i32) -> i32 { if a > 0 { 1 } else { 2 } }\n"
	tests := []struct {
		name string
		path string
		src  string
		want []string
	}{
		{name: "nested function reported", path: "src/lib.rs", src: nested + simple, want: []string{"clean.cognitive-complexity:1"}},
		{name: "test file excluded", path: "tests/it.rs", src: nested, want: []string{}},
		{name: "cfg(test) module excluded", path: "src/lib.rs", src: "#[cfg(test)]\nmod tests {\n" + nested + "}\n", want: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := findings(t, tc.path, tc.src); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSkip(t *testing.T) {
	tests := []struct {
		path    string
		content string
		want    bool
	}{
		{path: "src/lib.rs", content: "fn f() {}\n", want: false},
		{path: "target/debug/build/out.rs", content: "fn f() {}\n", want: true},
		{path: "src/proto.rs", content: "// @generated by prost-build\nfn f() {}\n", want: true},
		{path: "src/gen.rs", content: "// Code generated by bindgen. DO NOT EDIT.\nfn f() {}\n", want: true},
		{path: "src/targets.rs", content: "fn f() {}\n", want: false},
	}
	for _, tc := range tests {
		if got := (Analyzer{}).Skip(analysis.File{Path: tc.path, Content: []byte(tc.content)}); got != tc.want {
			t.Errorf("Skip(%s) = %v, want %v", tc.path, got, tc.want)
		}
	}
	if !(Analyzer{}).SkipPath("crate/target/release/x.rs") || (Analyzer{}).SkipPath("src/target.rs") {
		t.Error("SkipPath should exclude only target/ directories")
	}
}

func TestFrameworks(t *testing.T) {
	src := `use tokio::sync::Mutex;
use ::serde::{Deserialize, Serialize};
use actix_web::{web, App};
use axum as ax;
use sqlx::*;
use std::collections::HashMap;
use crate::bevy::Thing;

#[rocket::launch]
fn rocket() {}
`
	got := Analyzer{}.Frameworks(analysis.File{Path: "src/main.rs", Content: []byte(src)})
	want := []string{"actix", "axum", "rocket", "serde", "sqlx", "tokio"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Frameworks = %v, want %v", got, want)
	}
	if got := (Analyzer{}).Frameworks(analysis.File{Path: "a.rs", Content: []byte("use std::io;\n")}); len(got) != 0 {
		t.Errorf("Frameworks = %v, want none", got)
	}
}

func TestTestAttribute(t *testing.T) {
	for attr, want := range map[string]bool{
		"test": true, "tokio::test": true, "cfg(test)": true, "cfg(all(test, feature = \"x\"))": true,
		"cfg(not(test))": false, "cfg(feature = \"testing\")": false, "derive(Debug)": false,
	} {
		if got := isTestAttribute(attr); got != want {
			t.Errorf("isTestAttribute(%q) = %v, want %v", attr, got, want)
		}
	}
}
