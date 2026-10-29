package analysis

import "sort"

// Merge combines the results of several repositories, or of already merged
// results. Finding paths are prefixed with each part's label so local
// locations stay unambiguous. The result is declared only if every part is.
func Merge(labels []string, parts []Result) Result {
	if len(parts) == 1 {
		return parts[0]
	}
	out := Result{MarkedByTool: map[string]int{}, Declared: len(parts) > 0, Tools: map[string]ToolUse{}, Design: map[string]DesignUse{}}
	byLanguage := map[string]*LanguageResult{}
	frameworks := map[string]map[string]bool{}
	for i, p := range parts {
		out.Repos += p.Repos
		out.Declared = out.Declared && p.Declared
		out.OwnedLines += p.OwnedLines
		out.AILines += p.AILines
		out.AIFindings += p.AIFindings
		out.Findings += p.Findings
		out.MarkedLines += p.MarkedLines
		out.SkippedSlow += p.SkippedSlow
		out.Dismissed += p.Dismissed
		out.Git.Commits += p.Git.Commits
		out.Git.ShortSubject += p.Git.ShortSubject
		out.Git.Descriptive += p.Git.Descriptive
		out.Git.Conventional += p.Git.Conventional
		out.Git.WithBody += p.Git.WithBody
		for name, u := range p.Tools {
			sum := out.Tools[name]
			sum.Repos += u.Repos
			sum.Files += u.Files
			sum.Commits += u.Commits
			sum.Lines += u.Lines
			if u.Last.After(sum.Last) {
				sum.Last = u.Last
			}
			out.Tools[name] = sum
		}
		for area, u := range p.Design {
			sum := out.Design[area]
			if sum.Libraries == nil {
				sum.Libraries = map[string]int{}
			}
			sum.Repos += u.Repos
			sum.Files += u.Files
			for lib, n := range u.Libraries {
				sum.Libraries[lib] += n
			}
			out.Design[area] = sum
		}
		out.Durability.AssistedAdded += p.Durability.AssistedAdded
		out.Durability.AssistedFixed += p.Durability.AssistedFixed
		out.Durability.OtherAdded += p.Durability.OtherAdded
		out.Durability.OtherFixed += p.Durability.OtherFixed
		out.Tests.AssistedCommits += p.Tests.AssistedCommits
		out.Tests.AssistedWithTests += p.Tests.AssistedWithTests
		out.Tests.OtherCommits += p.Tests.OtherCommits
		out.Tests.OtherWithTests += p.Tests.OtherWithTests
		for _, r := range p.PerRepo {
			r.Label = labels[i]
			out.PerRepo = append(out.PerRepo, r)
		}
		for tool, n := range p.MarkedByTool {
			out.MarkedByTool[tool] += n
		}
		for _, lr := range p.Languages {
			m := byLanguage[lr.Language]
			if m == nil {
				m = &LanguageResult{Language: lr.Language, AssistedByRule: map[string]int{}, Idioms: map[string]IdiomUse{}, FrameworkUse: map[string]Usage{}}
				byLanguage[lr.Language] = m
				frameworks[lr.Language] = map[string]bool{}
			}
			m.Files += lr.Files
			m.OwnedLines += lr.OwnedLines
			m.AssistedLines += lr.AssistedLines
			m.AssistedFindings += lr.AssistedFindings
			m.TestLines += lr.TestLines
			for rule, n := range lr.AssistedByRule {
				m.AssistedByRule[rule] += n
			}
			for rule, u := range lr.Idioms {
				sum := m.Idioms[rule]
				sum.Uses += u.Uses
				sum.Files += u.Files
				m.Idioms[rule] = sum
			}
			for _, fd := range lr.Findings {
				fd.Path = labels[i] + "/" + fd.Path
				m.Findings = append(m.Findings, fd)
			}
			for _, fw := range lr.Frameworks {
				frameworks[lr.Language][fw] = true
			}
			for fw, u := range lr.FrameworkUse {
				sum := m.FrameworkUse[fw]
				m.FrameworkUse[fw] = Usage{Repos: sum.Repos + u.Repos, Files: sum.Files + u.Files}
			}
		}
	}
	for language, m := range byLanguage {
		for fw := range frameworks[language] {
			m.Frameworks = append(m.Frameworks, fw)
		}
		sort.Strings(m.Frameworks)
		out.Languages = append(out.Languages, *m)
	}
	sort.Slice(out.Languages, func(i, j int) bool { return out.Languages[i].Language < out.Languages[j].Language })
	return out
}
