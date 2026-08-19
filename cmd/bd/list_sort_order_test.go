package main

import (
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// sortOrderFixture returns four same-priority issues whose slice order is not
// their ID order, so an assertion on rendered order discriminates "the caller's
// order survived" from "the tree re-sorted by ID". Same priority is the point:
// compareIssuesByPriority falls through to the ID comparison, which is exactly
// the case GH#5811 reported.
func sortOrderFixture() []*types.Issue {
	return []*types.Issue{
		{ID: "so-yzp", Title: "Beta", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask},
		{ID: "so-30v", Title: "Delta", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask},
		{ID: "so-ezd", Title: "Alpha", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask},
		{ID: "so-gfo", Title: "Charlie", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask},
	}
}

// renderedIDOrder returns the fixture IDs in the order they appear in out.
func renderedIDOrder(t *testing.T, out string, ids []string) []string {
	t.Helper()
	type pos struct {
		id string
		at int
	}
	var found []pos
	for _, id := range ids {
		at := strings.Index(out, id)
		if at < 0 {
			t.Fatalf("issue %s missing from output:\n%s", id, out)
		}
		found = append(found, pos{id: id, at: at})
	}
	for i := 1; i < len(found); i++ {
		for j := i; j > 0 && found[j].at < found[j-1].at; j-- {
			found[j], found[j-1] = found[j-1], found[j]
		}
	}
	order := make([]string, 0, len(found))
	for _, f := range found {
		order = append(order, f.id)
	}
	return order
}

// GH#5811: `bd list --sort updated` exited 0 and printed a normal-looking list
// in ID order, because the tree renderer re-sorted the page it was handed. The
// sort machinery was never the problem — --json proved it correct — so this
// asserts the actual rendered ORDER, not just that the issues appear.
func TestPrettyTreeHonorsCallerOrder(t *testing.T) {
	issues := sortOrderFixture()
	want := []string{"so-yzp", "so-30v", "so-ezd", "so-gfo"}

	out := captureStdout(t, func() error {
		displayPrettyListWithDepsModeOrder(issues, false, nil, "", false, false, true)
		return nil
	})

	got := renderedIDOrder(t, out, want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("preserveOrder tree must render the caller's order\n got: %v\nwant: %v\noutput:\n%s", got, want, out)
	}
}

// The default is unchanged: with no --sort the tree still imposes its own
// stable priority-then-ID order, which is what keeps a bare `bd list` from
// shuffling between runs.
func TestPrettyTreeDefaultStillSortsByID(t *testing.T) {
	issues := sortOrderFixture()
	want := []string{"so-30v", "so-ezd", "so-gfo", "so-yzp"}

	out := captureStdout(t, func() error {
		displayPrettyListWithDepsMode(issues, false, nil, "", false, false)
		return nil
	})

	got := renderedIDOrder(t, out, want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("default tree must keep its priority/ID order\n got: %v\nwant: %v\noutput:\n%s", got, want, out)
	}
}

// Dependency-backed children are collected by ranging over the allDeps map, so
// their slice order is Go's randomized map order before anything sorts them.
// Preserving the caller's order therefore cannot mean "skip the sort" -- it has
// to mean "sort by the caller's position". Repeated so one lucky map iteration
// cannot pass this.
func TestTreeChildrenFromDepsFollowCallerOrder(t *testing.T) {
	parent := &types.Issue{ID: "so-p", Title: "Parent", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeEpic}
	c1 := &types.Issue{ID: "so-c1", Title: "One", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	c2 := &types.Issue{ID: "so-c2", Title: "Two", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	c3 := &types.Issue{ID: "so-c3", Title: "Three", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	// Caller order is the reverse of ID order, so an ID sort and a map-order
	// accident are both distinguishable from the right answer.
	issues := []*types.Issue{parent, c3, c2, c1}
	deps := map[string][]*types.Dependency{
		"so-c1": {{IssueID: "so-c1", DependsOnID: "so-p", Type: types.DepParentChild}},
		"so-c2": {{IssueID: "so-c2", DependsOnID: "so-p", Type: types.DepParentChild}},
		"so-c3": {{IssueID: "so-c3", DependsOnID: "so-p", Type: types.DepParentChild}},
	}
	want := []string{"so-c3", "so-c2", "so-c1"}

	for i := 0; i < 50; i++ {
		_, childrenMap := buildIssueTreeWithDepsOrder(issues, deps, true)
		var got []string
		for _, child := range childrenMap["so-p"] {
			got = append(got, child.ID)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("iteration %d: dependency-backed children must follow caller order\n got: %v\nwant: %v", i, got, want)
		}
	}
}

// Nesting is where the tree sorts a second time (printPrettyTree re-sorts each
// parent's children), so the fix has to hold one level down too.
func TestPrettyTreeHonorsCallerOrderForChildren(t *testing.T) {
	parent := &types.Issue{ID: "so-root", Title: "Root", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeEpic}
	childB := &types.Issue{ID: "so-root.2", Title: "Second", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	childA := &types.Issue{ID: "so-root.1", Title: "First", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	// Caller order puts .2 before .1; the ID sort would flip them.
	issues := []*types.Issue{parent, childB, childA}
	want := []string{"so-root.2", "so-root.1"}

	out := captureStdout(t, func() error {
		displayPrettyListWithDepsModeOrder(issues, false, nil, "", false, false, true)
		return nil
	})

	got := renderedIDOrder(t, out, want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("preserveOrder must reach nested children\n got: %v\nwant: %v\noutput:\n%s", got, want, out)
	}
}
