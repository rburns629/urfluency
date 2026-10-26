package rules

// Framework rules apply to files that import the framework.
func init() {
	py := []string{"python"}
	js := []string{"javascript", "typescript"}
	ts := []string{"typescript"}
	Register(
		Rule{ID: "react.dangerously-set-inner-html", Category: Security, Languages: js, Framework: "react",
			Title: "dangerouslySetInnerHTML",
			Why:   "Rendering raw HTML bypasses React's escaping and allows cross-site scripting.",
			Focus: "Render data as JSX, or sanitize with DOMPurify before injecting HTML."},
		Rule{ID: "react.hook-in-condition", Category: LanguageProficiency, Languages: js, Framework: "react",
			Title: "Hook called conditionally or in a loop",
			Why:   "React matches hooks by call order; calling one conditionally shifts state between hooks.",
			Focus: "Call hooks at the top level of the component and move the condition inside the hook."},
		Rule{ID: "react.missing-key", Category: LanguageProficiency, Languages: js, Framework: "react",
			Title: "List rendered without a key",
			Why:   "Without keys, React cannot track items, so it re-renders or reuses state for the wrong ones.",
			Focus: "Give each element a stable key from the data, such as an ID."},
		Rule{ID: "react.index-as-key", Category: LanguageProficiency, Languages: js, Framework: "react",
			Title: "Array index used as a key",
			Why:   "Index keys change when items are inserted, removed, or reordered, which mixes up component state.",
			Focus: "Use a stable identifier from the item."},

		Rule{ID: "angular.bypass-security-trust", Category: Security, Languages: ts, Framework: "angular",
			Title: "Angular sanitizer bypassed",
			Why:   "bypassSecurityTrust* turns off Angular's XSS protection for that value.",
			Focus: "Sanitize the value first, or render it without trusting it."},
		Rule{ID: "angular.nested-subscribe", Category: CleanCode, Languages: ts, Framework: "angular",
			Title: "subscribe nested inside subscribe",
			Why:   "Nested subscriptions leak, race, and cannot be cancelled together.",
			Focus: "Compose streams with switchMap, mergeMap, or concatMap and subscribe once."},

		Rule{ID: "fastify.route-without-schema", Category: TypeSafety, Languages: js, Framework: "fastify",
			Title: "POST, PUT, or PATCH route without a schema",
			Why:   "Without a schema, Fastify passes unvalidated request bodies straight to the handler.",
			Focus: "Add a schema option with body (and params or querystring) definitions."},
		Rule{ID: "fastify.async-reply-send", Category: LanguageProficiency, Languages: js, Framework: "fastify",
			Title: "reply.send() in an async handler without returning it",
			Why:   "An async handler that calls reply.send without returning resolves to undefined, causing double responses or errors.",
			Focus: "Return the value from the handler, or return reply after reply.send()."},

		Rule{ID: "express.reflected-input", Category: Security, Languages: js, Framework: "express",
			Title: "Request input sent straight back in the response",
			Why:   "res.send(req.query.x) reflects user input into the page, which allows cross-site scripting.",
			Focus: "Validate the input and send JSON with res.json, or escape it before sending HTML."},

		Rule{ID: "fastapi.untyped-body", Category: TypeSafety, Languages: py, Framework: "fastapi",
			Title: "Request body typed as dict or Any",
			Why:   "A dict or Any body skips FastAPI's validation and documentation.",
			Focus: "Declare a Pydantic model for the body."},
		Rule{ID: "fastapi.blocking-call-in-async", Category: LanguageProficiency, Languages: py, Framework: "fastapi",
			Title: "Blocking call inside async def",
			Why:   "time.sleep and requests block the event loop, stalling every request on the server.",
			Focus: "Use await asyncio.sleep and an async client such as httpx.AsyncClient, or make the route def."},

		Rule{ID: "django.debug-true", Category: Security, Languages: py, Framework: "django",
			Title: "DEBUG = True in settings",
			Why:   "Debug mode exposes stack traces, settings, and SQL to anyone who triggers an error.",
			Focus: "Read DEBUG from the environment and default it to False."},
		Rule{ID: "django.csrf-exempt", Category: Security, Languages: py, Framework: "django",
			Title: "View exempted from CSRF protection",
			Why:   "csrf_exempt lets other sites submit requests on behalf of logged-in users.",
			Focus: "Keep CSRF protection; for APIs use token authentication that does not rely on cookies."},
		Rule{ID: "django.null-on-text-field", Category: LanguageProficiency, Languages: py, Framework: "django",
			Title: "null=True on a text field",
			Why:   "Text fields with null=True have two empty values, None and \"\", which complicates queries.",
			Focus: "Use blank=True with default \"\" instead."},

		Rule{ID: "flask.debug-mode", Category: Security, Languages: py, Framework: "flask",
			Title: "Flask run with debug=True",
			Why:   "The Werkzeug debugger lets anyone who reaches it execute code on the server.",
			Focus: "Enable debug only through local configuration, never in code that ships."},
	)
}
