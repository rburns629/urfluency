package rules

// AI engineering rules, graded per task from what happened in local agent
// sessions. Prompt text is never stored; only whether each task met the rule.
func init() {
	prompting := []string{"prompting"}
	Register(
		Rule{ID: "prompt.unverified-change", Category: AIEngineering, Languages: prompting,
			Title: "Agent code changes never tested or checked",
			Why:   "The agent edited code but no tests, linters, builds, or type checks ran in the task, so nothing confirmed the change works.",
			Focus: "Ask for the tests or checks that prove the change, or require them in the project's agent instructions."},
		Rule{ID: "prompt.needed-correction", Category: AIEngineering, Languages: prompting,
			Title: "Tasks that needed a correction",
			Why:   "A follow-up such as \"no, that's not it\" means the first prompt left out something the agent needed.",
			Focus: "When you correct an agent, note what was missing and include it in the first prompt next time."},
	)
}
