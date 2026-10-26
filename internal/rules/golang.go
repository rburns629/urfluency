package rules

func init() {
	Register(
		Rule{
			ID:       "go.switch-empty-case",
			Category: LanguageProficiency,
			Language: "go",
			Title:    "Empty switch case that looks like intended fall-through",
			Why:      "Go switch cases do not fall through. An empty case does nothing, so values listed in it skip the code in the next case.",
			Focus:    "List values that share behavior in one case (case \"a\", \"b\":), or use fallthrough explicitly.",
		},
		Rule{
			ID:       "go.decode-target-reuse",
			Category: LanguageProficiency,
			Language: "go",
			Title:    "Decoding into a variable declared outside the loop",
			Why:      "json.Unmarshal and similar decoders keep fields missing from the input and reuse existing maps and slices, so records leak values into each other.",
			Focus:    "Declare the decode target inside the loop so every document starts from a zero value.",
		},
		Rule{
			ID:       "go.discarded-result",
			Category: CleanCode,
			Language: "go",
			Title:    "Discarded error or formatted string",
			Why:      "fmt.Errorf, errors.New, and fmt.Sprintf only return values. Calling them as a statement does nothing, so the intended error or log message is lost.",
			Focus:    "Return or wrap the error with %w, or log it with a logger.",
		},
		Rule{
			ID:       "go.unchecked-type-assertion",
			Category: TypeSafety,
			Language: "go",
			Title:    "Single-value type assertion",
			Why:      "x.(T) panics when x holds another type or is nil, and an unrecovered panic ends the whole process.",
			Focus:    "Use v, ok := x.(T) or a type switch, or decode external data into typed structs.",
		},
		Rule{
			ID:       "go.panic-outside-main",
			Category: CleanCode,
			Language: "go",
			Title:    "panic in library code",
			Why:      "Library code that panics takes the whole program down instead of letting callers handle the failure.",
			Focus:    "Return an error and let the caller decide; reserve panic for impossible states.",
		},
		Rule{
			ID:       "go.untyped-map-signature",
			Category: TypeSafety,
			Language: "go",
			Title:    "map[string]any in a function signature",
			Why:      "Untyped maps move type checking to runtime assertions, which hide mistakes until they crash.",
			Focus:    "Define a struct for the data and pass it instead.",
		},
	)
}
