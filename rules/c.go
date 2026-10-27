package rules

func init() {
	c := []string{"c"}
	Register(
		Rule{
			ID:        "c.unsafe-string-function",
			Category:  Security,
			Languages: c,
			Title:     "Unbounded string function",
			Why:       "gets, strcpy, strcat, sprintf, and vsprintf write until the source ends, not until the buffer does, so long input overflows the buffer.",
			Focus:     "Use snprintf, or strlcpy and strlcat where available, passing the destination size; read lines with fgets.",
		},
		Rule{
			ID:        "c.format-string-not-literal",
			Category:  Security,
			Languages: c,
			Title:     "printf-style call with a non-literal format",
			Why:       "When the format comes from data, any % in it is interpreted, so input such as %s or %n can read or write memory.",
			Focus:     "Pass a literal format and the data as an argument: printf(\"%s\", s).",
		},
		Rule{
			ID:        "c.system-call",
			Category:  Security,
			Languages: c,
			Title:     "system or popen with a built command",
			Why:       "The command runs through the shell, so data in it can inject further commands.",
			Focus:     "Run the program directly with fork and execv or posix_spawn, passing arguments as an array.",
		},
		Rule{
			ID:        "c.void-pointer-arithmetic",
			Category:  TypeSafety,
			Languages: c,
			Title:     "Arithmetic on a void pointer",
			Why:       "void has no size, so ISO C forbids arithmetic on void *; it compiles only as a GNU extension that treats the size as 1.",
			Focus:     "Cast to unsigned char * (or the real element type) before offsetting.",
		},
		Rule{
			ID:        "c.empty-parameter-list",
			Category:  TypeSafety,
			Languages: c,
			Title:     "Function declared with ()",
			Why:       "Before C23, int f(); declares a function with unspecified parameters, so calls with the wrong arguments are not checked.",
			Focus:     "Declare functions without parameters as f(void).",
		},
		Rule{
			ID:        "c.assignment-in-condition",
			Category:  CleanCode,
			Languages: c,
			Title:     "Assignment used as a condition",
			Why:       "if (x = y) is usually a mistyped ==, and when intended it reads like one.",
			Focus:     "Compare with ==, or assign first and test separately; wrap a deliberate assignment in extra parentheses.",
		},
		Rule{
			ID:        "c.realloc-self-assign",
			Category:  LanguageProficiency,
			Languages: c,
			Title:     "realloc result assigned to its own argument",
			Why:       "When realloc fails it returns NULL and keeps the old block, so p = realloc(p, n) loses the only pointer and leaks it.",
			Focus:     "Assign to a temporary, check it for NULL, then replace the original pointer.",
		},

		idiom("c.idiom.bounded-string", "Bounded string functions", "Uses snprintf, strlcpy, and strlcat, which never write past the size given.", "c"),
		idiom("c.idiom.sizeof-object", "sizeof *p in allocations", "Sizes allocations by the object, which stays correct when the pointer's type changes.", "c"),
		idiom("c.idiom.const-pointer-param", "Pointer-to-const parameters", "Promises callers the function will not modify what they pass, checked by the compiler.", "c"),
		idiom("c.idiom.static-function", "static file-local functions", "Keeps helpers private to their file, out of the global namespace.", "c"),
		idiom("c.idiom.designated-initializer", "Designated initializers", "Names the fields being set, so initializers survive reordering and read clearly.", "c"),
		idiom("c.idiom.enum-constants", "Enums for named constants", "Groups related constants under a type instead of loose #define values.", "c"),
		idiom("c.idiom.goto-cleanup", "goto cleanup error handling", "Releases resources on every error path through one exit, the standard C pattern.", "c"),
		idiom("c.idiom.include-guard", "Include guards", "Makes headers safe to include more than once with #ifndef guards or #pragma once.", "c"),
		idiom("c.idiom.static-assert", "Static assertions", "Checks layout and size assumptions at compile time with _Static_assert.", "c"),
		idiom("c.idiom.fixed-width-types", "Fixed-width integer types", "Uses stdint.h types such as uint32_t where the size of an integer matters.", "c"),
	)
}
