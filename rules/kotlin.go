package rules

func init() {
	kt := []string{"kotlin"}
	Register(
		Rule{ID: "kotlin.not-null-assertion", Category: TypeSafety, Languages: kt,
			Title: "Not-null assertion (!!)",
			Why:   "!! turns a nullable value into a NullPointerException at runtime, discarding the null safety the type system provides.",
			Focus: "Handle null explicitly with ?., ?:, requireNotNull with a message, or a smart-cast check."},
		Rule{ID: "kotlin.global-scope", Category: LanguageProficiency, Languages: kt,
			Title: "Coroutine launched in GlobalScope",
			Why:   "GlobalScope coroutines are tied to no lifecycle: they are never cancelled with their caller, leak work, and lose failures.",
			Focus: "Launch in a structured scope such as viewModelScope, lifecycleScope, or one created with coroutineScope or an injected CoroutineScope."},
		Rule{ID: "kotlin.run-blocking-in-production", Category: LanguageProficiency, Languages: kt,
			Title: "runBlocking outside main or tests",
			Why:   "runBlocking blocks the calling thread until its coroutines finish, which freezes UI threads and starves server request threads.",
			Focus: "Make the caller a suspend function, or launch in a scope; keep runBlocking for main functions and tests."},
		Rule{ID: "kotlin.empty-catch", Category: CleanCode, Languages: kt,
			Title: "Empty catch block",
			Why:   "An empty catch hides failures, so the program continues in a wrong state with no trace of why.",
			Focus: "Handle, log, or rethrow the exception; if ignoring it is intended, name it ignored or _ or add a comment saying why."},
		Rule{ID: "kotlin.sql-string-built", Category: Security, Languages: kt,
			Title: "SQL built from string templates or concatenation",
			Why:   "Interpolating values into rawQuery, execSQL, createQuery, or JDBC statements allows SQL injection.",
			Focus: "Pass values as bind parameters (?, :name) with selectionArgs, setParameter, or PreparedStatement setters."},
		Rule{ID: "kotlin.command-built", Category: Security, Languages: kt,
			Title: "Shell command built from string templates or concatenation",
			Why:   "Runtime.exec with a built string, or sh -c with an interpolated command, lets values inject extra arguments or shell commands.",
			Focus: "Pass the program and each argument separately to ProcessBuilder, without a shell, and validate inputs."},

		idiom("kotlin.idiom.data-class", "Data classes", "Models values with generated equals, hashCode, toString, and copy instead of hand-written boilerplate.", kt...),
		idiom("kotlin.idiom.sealed-hierarchy", "Sealed classes and interfaces", "Closes a type hierarchy so the compiler knows every case.", kt...),
		idiom("kotlin.idiom.exhaustive-when", "Exhaustive when expressions", "Uses when as a value without else, so the compiler checks every case is handled.", kt...),
		idiom("kotlin.idiom.null-safety", "Safe calls (?.) and elvis (?:)", "Handles null where it can occur instead of asserting it away.", kt...),
		idiom("kotlin.idiom.extension-function", "Extension functions", "Adds behavior to existing types without inheritance or utility classes.", kt...),
		idiom("kotlin.idiom.suspend-function", "suspend functions", "Writes asynchronous code sequentially with coroutines instead of callbacks.", kt...),
		idiom("kotlin.idiom.structured-concurrency", "coroutineScope, supervisorScope, and withContext", "Ties coroutines to a scope that waits for, cancels, and reports failures of its children.", kt...),
		idiom("kotlin.idiom.scope-function", "Scope functions", "Uses let, apply, also, and run to configure objects and handle nullable values concisely.", kt...),
		idiom("kotlin.idiom.collection-operations", "Collection operations", "Transforms collections declaratively with map, filter, fold, and friends instead of index loops.", kt...),
		idiom("kotlin.idiom.reified-generics", "inline functions with reified type parameters", "Keeps type information at runtime without passing Class objects.", kt...),
	)
}
