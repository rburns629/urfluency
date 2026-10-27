package analysis

import "sort"

// Merge combines the results of several repositories. Finding paths are prefixed
// with each repository's label so local locations stay unambiguous.
func Merge(labels []string, parts []Result) Result {
	if len(parts) == 1 {
		return parts[0]
	}
	out := Result{Repos: len(parts), MarkedByTool: map[string]int{}}
	byLanguage := map[string]*LanguageResult{}
	frameworks := map[string]map[string]bool{}
	for i, p := range parts {
		out.OwnedLines += p.OwnedLines
		out.AILines += p.AILines
		out.AIFindings += p.AIFindings
		out.Findings += p.Findings
		out.MarkedLines += p.MarkedLines
		out.SkippedSlow += p.SkippedSlow
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
				m = &LanguageResult{Language: lr.Language, AssistedByRule: map[string]int{}}
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
			for _, fd := range lr.Findings {
				fd.Path = labels[i] + "/" + fd.Path
				m.Findings = append(m.Findings, fd)
			}
			for _, fw := range lr.Frameworks {
				frameworks[lr.Language][fw] = true
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
