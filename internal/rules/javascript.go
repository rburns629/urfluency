package rules

func init() {
	jsAndTS := []string{"javascript", "typescript"}
	ts := []string{"typescript"}
	Register(
		Rule{
			ID:        "js.loose-equality",
			Category:  LanguageProficiency,
			Languages: jsAndTS,
			Title:     "Loose equality (== or !=)",
			Why:       "== converts types before comparing, so 0 == \"\" and \"1\" == 1 are true.",
			Focus:     "Use === and !==; == null is fine for null-or-undefined checks.",
		},
		Rule{
			ID:        "js.var-declaration",
			Category:  LanguageProficiency,
			Languages: jsAndTS,
			Title:     "var declaration",
			Why:       "var is function-scoped and hoisted, which causes closure and shadowing bugs that let and const prevent.",
			Focus:     "Use const by default and let when the value changes.",
		},
		Rule{
			ID:        "js.empty-catch",
			Category:  CleanCode,
			Languages: jsAndTS,
			Title:     "Empty catch block",
			Why:       "An empty catch silently discards the error, so failures look like success.",
			Focus:     "Handle, log, or rethrow the error; if ignoring it is intended, say why in a comment.",
		},
		Rule{
			ID:        "js.async-foreach",
			Category:  LanguageProficiency,
			Languages: jsAndTS,
			Title:     "async callback passed to forEach",
			Why:       "forEach ignores returned promises, so the loop finishes before the async work and errors go unhandled.",
			Focus:     "Use for...of with await, or await Promise.all(items.map(...)).",
		},
		Rule{
			ID:        "ts.ts-ignore",
			Category:  TypeSafety,
			Languages: jsAndTS,
			Title:     "@ts-ignore or @ts-nocheck comment",
			Why:       "These suppress type errors silently, and @ts-ignore keeps hiding them after the error is gone.",
			Focus:     "Fix the type, or use @ts-expect-error with a reason so it fails once the error is fixed.",
		},
		Rule{
			ID:        "ts.explicit-any",
			Category:  TypeSafety,
			Languages: ts,
			Title:     "Explicit any",
			Why:       "any switches off type checking for everything it touches and spreads through inference.",
			Focus:     "Use a specific type, a generic, or unknown with narrowing.",
		},
		Rule{
			ID:        "ts.non-null-assertion",
			Category:  TypeSafety,
			Languages: ts,
			Title:     "Non-null assertion (x!)",
			Why:       "x! tells the compiler a value cannot be null without checking, so a null still crashes at runtime.",
			Focus:     "Check for null or undefined, or use optional chaining.",
		},
		Rule{
			ID:        "ts.double-assertion",
			Category:  TypeSafety,
			Languages: ts,
			Title:     "Double type assertion (as unknown as T)",
			Why:       "Asserting through unknown or any forces any type onto a value, bypassing every compatibility check.",
			Focus:     "Validate the value at runtime, for example with a schema, and let the type follow.",
		},
	)
}
