package rules

// C# rules and idioms.
func init() {
	cs := []string{"csharp"}
	Register(
		Rule{ID: "csharp.sql-string-built", Category: Security, Languages: cs,
			Title: "SQL built from strings",
			Why:   "Interpolating or concatenating values into SqlCommand text, CommandText, or FromSqlRaw and ExecuteSqlRaw allows SQL injection.",
			Focus: "Pass values as SqlParameter or command parameters, or use FromSql and ExecuteSql with an interpolated FormattableString."},
		Rule{ID: "csharp.binary-formatter", Category: Security, Languages: cs,
			Title: "Insecure serializer",
			Why:   "BinaryFormatter, SoapFormatter, NetDataContractSerializer, LosFormatter, and ObjectStateFormatter can run arbitrary code when deserializing untrusted data; .NET has obsoleted and removed BinaryFormatter.",
			Focus: "Use System.Text.Json or another serializer that only creates the types you declare."},
		Rule{ID: "csharp.certificate-validation-disabled", Category: Security, Languages: cs,
			Title: "TLS certificate validation disabled",
			Why:   "A certificate validation callback that always returns true accepts any certificate, so anyone on the network can intercept the connection.",
			Focus: "Remove the callback, or trust only a specific certificate or thumbprint and keep it out of production builds."},
		Rule{ID: "csharp.async-void", Category: LanguageProficiency, Languages: cs,
			Title: "async void method",
			Why:   "Callers cannot await an async void method, and its exceptions crash the process instead of reaching a catch.",
			Focus: "Return Task (or ValueTask); keep async void for event handlers only."},
		Rule{ID: "csharp.blocking-on-task", Category: LanguageProficiency, Languages: cs,
			Title: "Blocking on a task",
			Why:   ".Result, .Wait(), and .GetAwaiter().GetResult() block a thread until the task finishes, which wastes threads and can deadlock under a synchronization context.",
			Focus: "Make the caller async and await the task."},
		Rule{ID: "csharp.empty-catch", Category: CleanCode, Languages: cs,
			Title: "All exceptions silently swallowed",
			Why:   "An empty catch of Exception (or everything) turns every failure into silent wrong behavior.",
			Focus: "Catch the specific exceptions you expect, and log or rethrow what you cannot handle."},
		Rule{ID: "csharp.dynamic-type", Category: TypeSafety, Languages: cs,
			Title: "dynamic type",
			Why:   "dynamic turns off compile-time checking, so typos and wrong types fail at run time.",
			Focus: "Use a declared type, generics, or a typed model for the data."},

		idiom("csharp.idiom.async-await", "async methods returning Task", "Writes non-blocking I/O with async and await and lets callers await the result.", cs...),
		idiom("csharp.idiom.linq", "LINQ", "Queries collections declaratively with query syntax or Where, Select, and friends.", cs...),
		idiom("csharp.idiom.using-disposal", "using statements and declarations", "Disposes files, connections, and other IDisposable resources on every path.", cs...),
		idiom("csharp.idiom.records", "Records", "Models immutable data with value equality in one line.", cs...),
		idiom("csharp.idiom.pattern-matching", "Pattern matching", "Tests and deconstructs values with is patterns and switch expressions instead of casts.", cs...),
		idiom("csharp.idiom.nullable-annotations", "Nullable reference types", "Enables nullable analysis or annotates references with ?, so the compiler catches null misuse.", cs...),
		idiom("csharp.idiom.expression-bodied", "Expression-bodied members", "Writes single-expression members concisely with =>.", cs...),
		idiom("csharp.idiom.init-only", "init setters and required members", "Makes objects immutable after construction and forces callers to set what is needed.", cs...),
		idiom("csharp.idiom.cancellation-token", "CancellationToken parameters", "Lets callers cancel long-running and asynchronous work.", cs...),
	)
}
