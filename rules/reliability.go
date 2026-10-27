package rules

// Reliability rules: request lifetimes, resource cleanup, timeouts, bounded
// concurrency, and framework patterns that degrade under load.
func init() {
	goLang := []string{"go"}
	js := []string{"javascript", "typescript"}
	py := []string{"python"}
	Register(
		Rule{ID: "go.context-background-in-handler", Category: LanguageProficiency, Languages: goLang,
			Title: "Fresh context where a request context exists",
			Why:   "context.Background or TODO inside a function that receives a ctx or *http.Request drops the caller's cancellation and deadline, so work continues after the client is gone.",
			Focus: "Pass the received ctx (or r.Context()) down; derive with context.WithTimeout when the work needs its own limit, or context.WithoutCancel to outlive the request while keeping its values."},
		Rule{ID: "go.defer-in-loop", Category: LanguageProficiency, Languages: goLang,
			Title: "defer inside a loop",
			Why:   "Deferred calls run when the function returns, not at the end of each iteration, so files, rows, and locks stay open for the whole loop.",
			Focus: "Move the loop body into a function so each defer runs per iteration, or close the resource explicitly."},
		Rule{ID: "go.http-without-timeout", Category: LanguageProficiency, Languages: goLang,
			Title: "HTTP client without a timeout",
			Why:   "http.Get, http.DefaultClient, and http.Client{} have no timeout, so one stalled server blocks the caller forever.",
			Focus: "Use an http.Client with Timeout set, or requests built with a context that has a deadline. If a client must not time out, such as one that streams, say so in a comment."},
		Rule{ID: "go.unbounded-goroutines", Category: LanguageProficiency, Languages: goLang,
			Title: "One goroutine per channel message without a bound",
			Why:   "Starting a goroutine for every message received from a channel has no upper limit, so a burst of messages exhausts memory, file descriptors, or downstream connections.",
			Focus: "Bound concurrency with errgroup.SetLimit, a semaphore channel, or a fixed worker pool."},
		Rule{ID: "react.effect-without-deps", Category: LanguageProficiency, Languages: js, Framework: "react",
			Title: "Effect without a dependency array",
			Why:   "useEffect without a second argument runs after every render, which easily causes repeated fetches or render loops.",
			Focus: "Pass the values the effect uses as dependencies, or [] to run once on mount."},
		Rule{ID: "clean.cognitive-complexity", Category: CleanCode, Languages: []string{"go", "python", "javascript", "typescript",
			"rust", "c", "cpp", "csharp", "java", "kotlin", "swift", "dart"},
			Title: "Function too complex to follow",
			Why:   "Cognitive complexity above 15 (SonarSource's measure of nesting, branching, and jumps) makes a function hard to read, review, and change safely.",
			Focus: "Extract nested branches into well-named functions, return early instead of nesting, and split functions that do several things."},
		Rule{ID: "django.query-in-loop", Category: LanguageProficiency, Languages: py, Framework: "django",
			Title: "ORM query inside a loop",
			Why:   "A Model.objects query per iteration is the classic N+1 pattern: one round trip per item instead of one for all.",
			Focus: "Query once before the loop with filter(...__in=...), select_related, or prefetch_related."},
	)
}
