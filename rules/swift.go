package rules

func init() {
	sw := []string{"swift"}
	Register(
		Rule{
			ID:        "swift.force-unwrap",
			Category:  TypeSafety,
			Languages: sw,
			Title:     "Force unwrap (!)",
			Why:       "A force unwrap crashes the app when the optional is nil, turning a recoverable case into a runtime trap.",
			Focus:     "Unwrap with guard let, if let, or ?? and handle the nil case.",
		},
		Rule{
			ID:        "swift.force-try",
			Category:  CleanCode,
			Languages: sw,
			Title:     "Force try (try!)",
			Why:       "try! crashes the app when the call throws, so any error becomes a runtime trap instead of being handled.",
			Focus:     "Use do/catch, propagate with try, or use try? when failure is expected.",
		},
		Rule{
			ID:        "swift.force-cast",
			Category:  TypeSafety,
			Languages: sw,
			Title:     "Force cast (as!)",
			Why:       "as! crashes the app when the value has a different type, bypassing the type checker.",
			Focus:     "Cast with as? and handle the failure, or make the type known statically.",
		},
		Rule{
			ID:        "swift.implicitly-unwrapped-optional",
			Category:  TypeSafety,
			Languages: sw,
			Title:     "Implicitly unwrapped optional (T!)",
			Why:       "A T! value is force unwrapped on every use, so a missing value crashes far from where it was left unset.",
			Focus:     "Declare a non-optional set in init, or a plain optional (T?) that callers unwrap; keep T! for @IBOutlet.",
		},
		Rule{
			ID:        "swift.empty-catch",
			Category:  CleanCode,
			Languages: sw,
			Title:     "Empty catch block",
			Why:       "Catching an error and doing nothing hides failures and leaves the program in an unknown state.",
			Focus:     "Handle, log, or rethrow the error, or use try? when ignoring the result is intended.",
		},
		Rule{
			ID:        "swift.http-url-literal",
			Category:  Security,
			Languages: sw,
			Title:     "Plain http:// URL",
			Why:       "Unencrypted HTTP exposes requests and responses to interception and tampering, and App Transport Security blocks it by default.",
			Focus:     "Use https://, or keep http:// only for local development hosts.",
		},
		Rule{
			ID:        "swift.count-instead-of-is-empty",
			Category:  LanguageProficiency,
			Languages: sw,
			Title:     "count compared with zero",
			Why:       "isEmpty states the intent and is O(1) for every collection, while count can walk the whole collection, as it does for String.",
			Focus:     "Use isEmpty or !isEmpty.",
		},

		idiom("swift.idiom.guard-early-exit", "guard for early exit", "Unwraps and validates inputs up front, keeping the main path unindented.", "swift"),
		idiom("swift.idiom.value-type", "Structs as value types", "Models data with value semantics, which avoids shared mutable state.", "swift"),
		idiom("swift.idiom.protocol-extension", "Protocol extensions", "Shares behavior through protocols and their extensions rather than class inheritance.", "swift"),
		idiom("swift.idiom.associated-values", "Enums with associated values", "Models each case with exactly the data it carries, checked exhaustively by switch.", "swift"),
		idiom("swift.idiom.async-await", "async functions", "Writes concurrent code with async and await instead of nested completion handlers.", "swift"),
		idiom("swift.idiom.codable", "Codable types", "Encodes and decodes data through the compiler-generated Codable conformance instead of manual parsing.", "swift"),
		idiom("swift.idiom.constrained-generics", "Generics with constraints", "Writes reusable code whose requirements the compiler checks.", "swift"),
		idiom("swift.idiom.defer", "defer for cleanup", "Releases resources on every exit path of a scope.", "swift"),
		idiom("swift.idiom.actor-isolation", "Actors and @MainActor", "Protects shared state and UI work with compiler-checked isolation instead of manual locks and dispatch.", "swift"),
		idiom("swift.idiom.property-wrapper", "Property wrappers", "Reuses storage behavior such as @State, @Published, or a custom @propertyWrapper.", "swift"),
	)
}
