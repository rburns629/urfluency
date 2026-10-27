package kotlin

import (
	"context"
	"testing"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/rules"
)

func TestIdioms(t *testing.T) {
	src := `package a

data class User(val id: Long, val name: String?)

sealed interface Shape
sealed class Result { object Ok : Result() }
class Plain(val x: Int)

fun String.shout(): String = uppercase()
fun <T> List<T>.second(): T = this[1]

inline fun <reified T> Any.isA(): Boolean = this is T
inline fun twice(f: () -> Unit) { f(); f() }

suspend fun load(repo: Repo): List<String> = coroutineScope {
    withContext(Dispatchers.IO) { repo.all() }
}

fun area(s: Shape): Double = when (s) {
    is Circle -> 3.14
    is Square -> 1.0
}

fun label(n: Int): String {
    when (n) { 1 -> println("one") }
    return when (n) { 1 -> "one"; else -> "many" }
}

fun names(users: List<User>): List<String> {
    val len = users.firstOrNull()?.name?.length ?: 0
    users.first().name?.let { println(it) }
    return users.filter { it.id > len }.map { it.name.orEmpty() }
}

fun build() = StringBuilder().apply { append("x") }.toString()
`
	findings, err := Analyzer{}.Analyze(context.Background(), []analysis.File{{Path: "src/main/kotlin/A.kt", Content: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, f := range findings {
		if f.Positive {
			got[f.Rule]++
		}
	}
	want := map[string]int{
		"kotlin.idiom.data-class": 1, "kotlin.idiom.sealed-hierarchy": 2, "kotlin.idiom.extension-function": 3,
		"kotlin.idiom.reified-generics": 1, "kotlin.idiom.suspend-function": 1, "kotlin.idiom.structured-concurrency": 2,
		"kotlin.idiom.exhaustive-when": 1, "kotlin.idiom.null-safety": 4, "kotlin.idiom.scope-function": 2,
		"kotlin.idiom.collection-operations": 2,
	}
	for rule, n := range want {
		if got[rule] != n {
			t.Errorf("%s = %d, want %d", rule, got[rule], n)
		}
	}
	for rule := range got {
		if _, ok := want[rule]; !ok {
			t.Errorf("unexpected idiom %s = %d", rule, got[rule])
		}
		if r, ok := rules.Lookup(rule); !ok || r.Category != rules.Idiom {
			t.Errorf("%s is not an idiom in the catalog", rule)
		}
	}
}
