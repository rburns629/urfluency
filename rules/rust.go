package rules

func init() {
	rs := []string{"rust"}
	Register(
		Rule{
			ID:        "rust.unwrap-in-library",
			Category:  CleanCode,
			Languages: rs,
			Title:     "unwrap or expect in library code",
			Why:       ".unwrap() and .expect() panic on None or Err, crashing the caller's program instead of letting it handle the failure.",
			Focus:     "Return the error with ?, or handle the None or Err case; keep unwrap for tests, main, and documented invariants (a # Panics doc section).",
		},
		Rule{
			ID:        "rust.panic-in-library",
			Category:  CleanCode,
			Languages: rs,
			Title:     "panic!, todo!, or unimplemented! in library code",
			Why:       "Panicking in a library takes the decision away from the caller; todo! and unimplemented! are unfinished code that crashes when reached.",
			Focus:     "Return a Result for failures the caller can handle, finish or remove placeholder code, and document deliberate panics in a # Panics section.",
		},
		Rule{
			ID:        "rust.unsafe-without-safety-comment",
			Category:  Security,
			Languages: rs,
			Title:     "unsafe block without a SAFETY comment",
			Why:       "The compiler cannot check an unsafe block; without a written justification, reviewers cannot verify the invariants it relies on.",
			Focus:     "Write a // SAFETY: comment above each unsafe block explaining why its invariants hold, or use a safe API.",
		},
		Rule{
			ID:        "rust.command-shell",
			Category:  Security,
			Languages: rs,
			Title:     "Shell command built with format!",
			Why:       "Passing a format!-built string to sh, bash, or cmd lets values containing shell syntax run arbitrary commands.",
			Focus:     "Run the program directly with Command::new(program).arg(value), passing each value as its own argument, without a shell.",
		},
		Rule{
			ID:        "rust.lossy-cast",
			Category:  TypeSafety,
			Languages: rs,
			Title:     "Narrowing as cast",
			Why:       "as silently truncates wider integers and saturates floats when casting to a narrower integer, producing wrong values without an error.",
			Focus:     "Use TryFrom (u32::try_from(x)) and handle the overflow, or document why the value always fits.",
		},
		Rule{
			ID:        "rust.ptr-arg",
			Category:  LanguageProficiency,
			Languages: rs,
			Title:     "Parameter takes &Vec, &String, or &PathBuf",
			Why:       "A shared reference to an owning container accepts fewer callers and adds an indirection; &[T], &str, and &Path accept the same values and more.",
			Focus:     "Take &[T] instead of &Vec<T>, &str instead of &String, and &Path instead of &PathBuf.",
		},
		Rule{
			ID:        "rust.index-loop",
			Category:  LanguageProficiency,
			Languages: rs,
			Title:     "Index loop over a collection",
			Why:       "for i in 0..v.len() with v[i] adds bounds checks and off-by-one risk that iterating the collection directly avoids.",
			Focus:     "Iterate with for x in &v, or v.iter().enumerate() when the index is needed.",
		},

		idiom("rust.idiom.question-mark", "? error propagation", "Propagates errors to the caller concisely instead of matching or unwrapping.", rs...),
		idiom("rust.idiom.result-return", "Functions returning Result", "Makes failure part of the signature so callers must handle it.", rs...),
		idiom("rust.idiom.iterator-chain", "Iterator chains", "Transforms collections with map, filter, and fold instead of index loops and mutable accumulators.", rs...),
		idiom("rust.idiom.enum-match", "match on enum variants", "Handles each variant explicitly, with the compiler checking that none is missed.", rs...),
		idiom("rust.idiom.traits", "Trait definitions and implementations", "Models shared behavior with traits rather than duplicated code.", rs...),
		idiom("rust.idiom.trait-bounds", "Generics with trait bounds", "Writes reusable code constrained by behavior, with bounds, where clauses, or impl Trait.", rs...),
		idiom("rust.idiom.combinators", "Option and Result combinators", "Transforms optional and fallible values with map_or, and_then, ok_or, and map_err instead of nested matches.", rs...),
		idiom("rust.idiom.test-module", "#[cfg(test)] test modules", "Keeps unit tests next to the code they test, compiled only for tests.", rs...),
		idiom("rust.idiom.error-type", "Custom error types", "Gives callers typed errors by implementing std::error::Error or deriving it with thiserror.", rs...),
		idiom("rust.idiom.lifetimes", "Explicit lifetimes", "Names how long borrowed data lives where the compiler cannot infer it.", rs...),
	)
}
