package rules

func init() {
	py := []string{"python"}
	Register(
		Rule{
			ID:        "py.mutable-default-argument",
			Category:  LanguageProficiency,
			Languages: py,
			Title:     "Mutable default argument",
			Why:       "Default values are created once, when the function is defined, so a list or dict default is shared and mutated across calls.",
			Focus:     "Default to None and create the list or dict inside the function.",
		},
		Rule{
			ID:        "py.bare-except",
			Category:  CleanCode,
			Languages: py,
			Title:     "Bare except clause",
			Why:       "except: also catches KeyboardInterrupt and SystemExit, hiding shutdown requests and real bugs.",
			Focus:     "Catch the specific exceptions you expect, or except Exception at most.",
		},
		Rule{
			ID:        "py.swallowed-exception",
			Category:  CleanCode,
			Languages: py,
			Title:     "Broad exception silently ignored",
			Why:       "Catching Exception (or everything) and doing nothing turns every failure into silent wrong behavior.",
			Focus:     "Catch narrower exceptions, and log or re-raise what you cannot handle.",
		},
		Rule{
			ID:        "py.is-literal-comparison",
			Category:  LanguageProficiency,
			Languages: py,
			Title:     "Identity comparison with a literal",
			Why:       "is compares object identity, not value; x is 1 or x is \"a\" depends on interpreter caching and can be false for equal values.",
			Focus:     "Use == for values; keep is for None, True, and False.",
		},
		Rule{
			ID:        "py.open-without-with",
			Category:  CleanCode,
			Languages: py,
			Title:     "File opened without a with block",
			Why:       "Without with, the file stays open if an exception occurs before close, leaking handles.",
			Focus:     "Open files with with open(...) as f:.",
		},
		Rule{
			ID:        "py.type-ignore",
			Category:  TypeSafety,
			Languages: py,
			Title:     "type: ignore comment",
			Why:       "Suppressing the type checker hides the mismatch instead of fixing it.",
			Focus:     "Fix the types, or use a narrow ignore with an error code and a reason.",
		},
		Rule{
			ID:        "py.unannotated-public-function",
			Category:  TypeSafety,
			Languages: py,
			Title:     "Public function without type annotations",
			Why:       "Unannotated parameters and returns are invisible to type checkers and to readers.",
			Focus:     "Annotate parameters and the return type of public functions.",
		},
	)
}
