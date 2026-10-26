package rules

// Prompting rules, graded per task from local agent sessions. Prompt text is never
// stored; only whether each task met the rule.
func init() {
	prompting := []string{"prompting"}
	Register(
		Rule{ID: "prompt.no-context", Category: Prompting, Languages: prompting,
			Title: "Task prompts without concrete context",
			Why:   "Without files, symbols, or the error to start from, the agent searches blind and is more likely to change the wrong code.",
			Focus: "Name the files or functions involved, paste the exact error, or point to an existing pattern to follow."},
		Rule{ID: "prompt.no-acceptance-criteria", Category: Prompting, Languages: prompting,
			Title: "Feature and refactor prompts without acceptance criteria",
			Why:   "Ambiguous work without a definition of done leaves the agent to guess scope, which is where rework concentrates.",
			Focus: "State what should happen when it is done: inputs and outputs, edge cases, and what must not change."},
		Rule{ID: "prompt.no-verification", Category: Prompting, Languages: prompting,
			Title: "Code-change prompts that never ask for verification",
			Why:   "Agents report success without running anything unless asked, so untested changes reach review.",
			Focus: "Ask for the tests or checks that prove the change, or keep that requirement in the project's agent instructions."},
		Rule{ID: "prompt.needed-correction", Category: Prompting, Languages: prompting,
			Title: "Tasks that needed a correction",
			Why:   "A follow-up such as \"no, that's not it\" means the first prompt left out something the agent needed.",
			Focus: "When you correct an agent, note what was missing and include it in the first prompt next time."},
	)
}
