package rules

func init() {
	cpp := []string{"cpp"}
	Register(
		Rule{
			ID:        "cpp.raw-new-delete",
			Category:  LanguageProficiency,
			Languages: cpp,
			Title:     "Manual new or delete",
			Why:       "Owning raw pointers leak when an exception or early return skips the delete, and double-delete or use-after-free when ownership is unclear.",
			Focus:     "Create objects with std::make_unique or std::make_shared, or hold them by value or in a container.",
		},
		Rule{
			ID:        "cpp.c-style-cast",
			Category:  TypeSafety,
			Languages: cpp,
			Title:     "C-style cast",
			Why:       "(T)x silently picks static_cast, const_cast, or reinterpret_cast, so it can strip const or reinterpret memory without the reader noticing.",
			Focus:     "Use static_cast for conversions, and spell out const_cast or reinterpret_cast when you really mean them.",
		},
		Rule{
			ID:        "cpp.using-namespace-in-header",
			Category:  CleanCode,
			Languages: cpp,
			Title:     "using namespace in a header",
			Why:       "A using-directive in a header leaks into every file that includes it, causing name clashes and ambiguous overloads far from the cause.",
			Focus:     "Qualify names in headers, or use a narrow using-declaration inside a function or your own namespace.",
		},
		Rule{
			ID:        "cpp.unsafe-c-string",
			Category:  Security,
			Languages: cpp,
			Title:     "Unbounded C string function",
			Why:       "strcpy, strcat, sprintf, vsprintf, and gets write without checking the destination size, the classic buffer overflow.",
			Focus:     "Use std::string, or bounded functions such as snprintf with the buffer size.",
		},
		Rule{
			ID:        "cpp.catch-by-value",
			Category:  LanguageProficiency,
			Languages: cpp,
			Title:     "Exception caught by value",
			Why:       "Catching a class type by value copies the exception and slices derived types, so the handler loses the real error and its overrides.",
			Focus:     "Catch by const reference: catch (const std::exception& e).",
		},
		Rule{
			ID:        "cpp.missing-override",
			Category:  LanguageProficiency,
			Languages: cpp,
			Title:     "Virtual function overridden without override",
			Why:       "Without override, a signature typo or a base-class change silently creates a new function instead of an error.",
			Focus:     "Mark every overriding member function override (or final).",
		},
		Rule{
			ID:        "cpp.empty-catch",
			Category:  CleanCode,
			Languages: cpp,
			Title:     "Empty catch block",
			Why:       "A catch block that does nothing turns failures into silent wrong behavior.",
			Focus:     "Handle, log, or rethrow the exception, or comment why ignoring it is safe.",
		},
		Rule{
			ID:        "cpp.system-call",
			Category:  Security,
			Languages: cpp,
			Title:     "system() or popen() with a built command",
			Why:       "The command runs through the shell, so data in a non-literal command string can inject extra commands.",
			Focus:     "Run programs with an argument vector (posix_spawn, execv, or a process library) instead of a shell string.",
		},

		idiom("cpp.idiom.smart-pointers", "Smart pointers", "Owns heap objects with std::unique_ptr, std::shared_ptr, and make_unique, so they are freed on every path.", "cpp"),
		idiom("cpp.idiom.range-for", "Range-based for loops", "Iterates containers without index or iterator bookkeeping.", "cpp"),
		idiom("cpp.idiom.constexpr", "constexpr and consteval", "Moves computation and checks to compile time.", "cpp"),
		idiom("cpp.idiom.move-semantics", "Move semantics", "Transfers resources with std::move, std::forward, and rvalue references instead of copying them.", "cpp"),
		idiom("cpp.idiom.lambda", "Lambdas", "Passes behavior inline to algorithms and callbacks instead of writing one-off functors.", "cpp"),
		idiom("cpp.idiom.override", "override and final", "Lets the compiler check that a member function really overrides a virtual one.", "cpp"),
		idiom("cpp.idiom.templates", "Templates and concepts", "Writes generic, type-checked code, constrained with concepts where available.", "cpp"),
		idiom("cpp.idiom.vocabulary-types", "std::optional, std::variant, and std::expected", "Models absent values, alternatives, and errors in the type instead of with sentinels or null pointers.", "cpp"),
		idiom("cpp.idiom.raii-lock", "RAII lock guards", "Holds mutexes with std::lock_guard, std::scoped_lock, or std::unique_lock, so they unlock on every path.", "cpp"),
		idiom("cpp.idiom.structured-bindings", "Structured bindings", "Unpacks pairs, tuples, and structs into named variables at the point of use.", "cpp"),
	)
}
