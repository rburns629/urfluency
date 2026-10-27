package kotlin

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/rules"
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
			name: "not-null assertion, but not in tests",
			path: "app/src/main/kotlin/A.kt",
			src: `fun f(x: String?, y: Int?) {
    val a = x!!.length
    val b = y ?: 0
    val c = x?.length
    if (a != b) println(c)
}
`,
			want: []string{"kotlin.not-null-assertion:2"},
		},
		{
			name: "not-null assertion in a test file",
			path: "app/src/test/kotlin/ATest.kt",
			src:  "fun f(x: String?) = x!!.length\n",
			want: []string{},
		},
		{
			name: "GlobalScope launch and async, but not structured scopes",
			path: "A.kt",
			src: `fun f(scope: CoroutineScope) {
    GlobalScope.launch { work() }
    kotlinx.coroutines.GlobalScope.async(Dispatchers.IO) { work() }
    scope.launch { work() }
    viewModelScope.async { work() }
}
`,
			want: []string{"kotlin.global-scope:2", "kotlin.global-scope:3"},
		},
		{
			name: "runBlocking outside main",
			path: "A.kt",
			src: `fun main() = runBlocking { serve() }
fun handler() {
    runBlocking { load() }
}
val cached = runBlocking { load() }
`,
			want: []string{"kotlin.run-blocking-in-production:3", "kotlin.run-blocking-in-production:5"},
		},
		{
			name: "runBlocking in tests and at script top level",
			path: "build.gradle.kts",
			src:  "val x = runBlocking { load() }\n",
			want: []string{},
		},
		{
			name: "runBlocking in a test path",
			path: "src/test/kotlin/Repo.kt",
			src:  "fun t() = runBlocking { load() }\n",
			want: []string{},
		},
		{
			name: "empty catch, but not with a comment, a body, or an ignored name",
			path: "A.kt",
			src: `fun f() {
    try { a() } catch (e: Exception) { }
    try { a() } catch (e: IOException) {
        // the cache is optional
    }
    try { a() } catch (e: Exception) { log(e) }
    try { a() } catch (ignored: Exception) { }
    try { a() } catch (_: Exception) { }
}
`,
			want: []string{"kotlin.empty-catch:2"},
		},
		{
			name: "SQL built with templates and concatenation",
			path: "A.kt",
			src: `fun f(db: SQLiteDatabase, em: EntityManager, id: String, user: User) {
    db.rawQuery("SELECT * FROM t WHERE id = $id", null)
    db.execSQL("DELETE FROM t WHERE id = " + id)
    em.createQuery("select u from U u where u.n = '${user.name}'")
    conn.prepareStatement(sql = """SELECT * FROM t WHERE id = $id""")
}
`,
			want: []string{"kotlin.sql-string-built:2", "kotlin.sql-string-built:3", "kotlin.sql-string-built:4", "kotlin.sql-string-built:5"},
		},
		{
			name: "SQL with bind parameters, constants, or escaped dollars",
			path: "A.kt",
			src: `const val TABLE = "t"
fun f(db: SQLiteDatabase, em: EntityManager, id: String) {
    db.rawQuery("SELECT * FROM t WHERE id = ?", arrayOf(id))
    db.execSQL("DELETE FROM $TABLE WHERE id = 1")
    db.execSQL("DELETE FROM " + TABLE + " WHERE id = " + 1)
    em.createQuery("select u from U u where u.n = :name").setParameter("name", id)
    db.rawQuery("SELECT '\$5' FROM t", null)
    val s = "SELECT * FROM t WHERE id = $id"
}
`,
			want: []string{},
		},
		{
			name: "commands built for exec or a shell",
			path: "A.kt",
			src: `fun f(dir: String) {
    Runtime.getRuntime().exec("ls $dir")
    Runtime.getRuntime().exec("rm -rf " + dir)
    ProcessBuilder("sh", "-c", "tar czf ${dir}.tgz $dir").start()
}
`,
			want: []string{"kotlin.command-built:2", "kotlin.command-built:3", "kotlin.command-built:4"},
		},
		{
			name: "commands with separate arguments",
			path: "A.kt",
			src: `fun f(dir: String) {
    Runtime.getRuntime().exec(arrayOf("ls", dir))
    Runtime.getRuntime().exec("ls -la")
    ProcessBuilder("ls", "$dir/sub").start()
    ProcessBuilder(listOf("git", "status")).start()
}
`,
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := findings(t, tt.path, tt.src); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRulesAreInCatalog(t *testing.T) {
	ids := []string{"kotlin.not-null-assertion", "kotlin.global-scope", "kotlin.run-blocking-in-production",
		"kotlin.empty-catch", "kotlin.sql-string-built", "kotlin.command-built", "clean.cognitive-complexity"}
	for _, id := range ids {
		r, ok := rules.Lookup(id)
		if !ok || r.Category == rules.Idiom {
			t.Errorf("%s is not a rule in the catalog", id)
			continue
		}
		found := false
		for _, l := range r.Languages {
			found = found || l == "kotlin"
		}
		if !found {
			t.Errorf("%s does not list kotlin", id)
		}
	}
}

func TestCognitiveComplexity(t *testing.T) {
	// Each nested if adds 1 plus its depth: 1+2+3+4+5 = 15 is at the
	// threshold; the && in the deepest condition takes it over.
	body := func(cond string) string {
		return `fun deep(a: Boolean, b: Boolean) {
    if (a) {
        for (i in 0..3) {
            while (b) {
                when (i) {
                    1 -> if (` + cond + `) { println(i) }
                }
            }
        }
    }
}
fun simple(a: Boolean) = if (a) 1 else 2
`
	}
	if got := findings(t, "A.kt", body("a && b")); !reflect.DeepEqual(got, []string{"clean.cognitive-complexity:1"}) {
		t.Errorf("complex function: got %v", got)
	}
	if got := findings(t, "A.kt", body("a")); len(got) != 0 {
		t.Errorf("function at the threshold: got %v", got)
	}
	if got := findings(t, "src/test/kotlin/DeepTest.kt", body("a && b")); len(got) != 0 {
		t.Errorf("test file: got %v", got)
	}
}

func TestSkip(t *testing.T) {
	tests := []struct {
		path, src string
		want      bool
	}{
		{"app/src/main/kotlin/A.kt", "package a\nfun f() {}\n", false},
		{"app/build/generated/source/kapt/A.kt", "package a\n", true},
		{"app/src/main/generated/A.kt", "package a\n", true},
		{".gradle/kotlin/A.kt", "package a\n", true},
		{"A.kt", "// Code generated by protoc. DO NOT EDIT.\npackage a\n", true},
		{"A.kt", "/* Automatically generated file. */\npackage a\n", true},
		{"A.kt", "package a\nimport javax.persistence.GeneratedValue\n@Entity class E(@Id @GeneratedValue val id: Long)\n", false},
		{"A.kt", "package a\n" + strings.Repeat("\n", 600) + "// DO NOT EDIT below\n", false},
	}
	for _, tt := range tests {
		if got := (Analyzer{}).Skip(analysis.File{Path: tt.path, Content: []byte(tt.src)}); got != tt.want {
			t.Errorf("Skip(%s, %.30q) = %v, want %v", tt.path, tt.src, got, tt.want)
		}
	}
	if !(Analyzer{}).SkipPath("build/tmp/A.kt") || (Analyzer{}).SkipPath("src/main/kotlin/builder/A.kt") {
		t.Error("SkipPath must exclude build/ and only build/")
	}
}

func TestFrameworks(t *testing.T) {
	src := `package a
import android.os.Bundle
import androidx.compose.runtime.Composable
import io.ktor.server.application.*
import org.springframework.stereotype.Service
import kotlinx.coroutines.launch
import org.jetbrains.exposed.sql.Table
import org.koin.core.module.Module
import dagger.hilt.android.AndroidEntryPoint
import androidx.lifecycle.ViewModel as VM
import androidxfake.Thing
import java.io.File
`
	got := Analyzer{}.Frameworks(analysis.File{Path: "A.kt", Content: []byte(src)})
	want := []string{"android", "coroutines", "exposed", "hilt", "jetpack-compose", "koin", "ktor", "spring"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := (Analyzer{}).Frameworks(analysis.File{Path: "B.kt", Content: []byte("import kotlin.math.max\n")}); len(got) != 0 {
		t.Errorf("plain Kotlin: got %v", got)
	}
}

func TestRegistered(t *testing.T) {
	if a := analysis.ForPath("app/build.gradle.kts"); a == nil || a.Language() != "kotlin" {
		t.Errorf("ForPath(.kts) = %v", a)
	}
	if a := analysis.ForPath("src/Main.kt"); a == nil || a.Language() != "kotlin" {
		t.Errorf("ForPath(.kt) = %v", a)
	}
}
