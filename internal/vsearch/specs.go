package vsearch

import (
	"sort"
)

// SpecIndexResponse is a search of one repo's spec index.
type SpecIndexResponse struct {
	Slug string
	Resp *Response
}

// SpecPath is a repo spec's result path: <slug>:specs/<capability>/spec.md.
func SpecPath(slug, rel string) string { return slug + ":" + rel }

// MergeSpecs adds the results of the repos' spec indexes (searched with
// the same options) to the vault's, by score (a vault result first on a
// tie), re-ranks them, and applies k and the token budget over the merged
// list. Spec results get their path as <slug>:specs/<capability>/spec.md,
// type spec and project <slug>.
func MergeSpecs(vault *Response, specs []SpecIndexResponse, k, budget int) *Response {
	if len(specs) == 0 {
		return vault
	}
	if k <= 0 {
		k = DefaultK
	}
	all := append([]Result(nil), vault.Results...)
	for _, s := range specs {
		for _, r := range s.Resp.Results {
			r.Path = SpecPath(s.Slug, r.Path)
			r.Type, r.Project = "spec", s.Slug
			all = append(all, r)
		}
	}
	sort.SliceStable(all, func(a, b int) bool { return all[a].Score > all[b].Score })
	out := *vault
	out.Results = []Result{}
	used := 0
	out.Budget = nil
	if budget > 0 {
		out.Budget = &BudgetInfo{Limit: budget}
	}
	for _, r := range all {
		if len(out.Results) >= k {
			break
		}
		if budget > 0 && len(out.Results) > 0 && used+r.Tokens > budget {
			out.Budget.Truncated = true
			break
		}
		used += r.Tokens
		r.Rank = len(out.Results) + 1
		out.Results = append(out.Results, r)
	}
	if out.Budget != nil {
		out.Budget.Used = used
	}
	return &out
}
