package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func listOutputFixture() ([]*types.Issue, map[string][]*types.Dependency) {
	issues := []*types.Issue{
		{
			ID:        "list-a",
			Title:     "Alpha",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeBug,
		},
		{
			ID:        "list-b",
			Title:     "Beta",
			Status:    types.StatusClosed,
			Priority:  2,
			IssueType: types.TypeTask,
		},
	}
	deps := map[string][]*types.Dependency{
		"list-a": {
			{
				IssueID:     "list-a",
				DependsOnID: "list-b",
				Type:        types.DepBlocks,
			},
		},
	}
	return issues, deps
}

func TestOutputDotFormatExactBytes(t *testing.T) {
	t.Parallel()
	issues, deps := listOutputFixture()
	var out bytes.Buffer

	if err := outputDotFormat(&out, issues, deps); err != nil {
		t.Fatalf("outputDotFormat: %v", err)
	}

	const want = `digraph dependencies {
  rankdir=TB;
  node [shape=box, style=rounded];

  "list-a" [label="list-a\n[bug P1]\nAlpha\n(open)", style="rounded,filled", fillcolor="white", fontcolor="black"];
  "list-b" [label="list-b\n[task P2]\nBeta\n(closed)", style="rounded,filled", fillcolor="lightgray", fontcolor="dimgray"];

  "list-a" -> "list-b" [label="blocks", color=red, style=bold];
}
`
	if got := out.String(); got != want {
		t.Fatalf("DOT output bytes differ\ngot:\n%q\nwant:\n%q", got, want)
	}
}

func TestOutputFormattedListExactBytes(t *testing.T) {
	t.Parallel()
	issues, deps := listOutputFixture()

	for _, tc := range []struct {
		name   string
		format string
		want   string
	}{
		{name: "digraph preset", format: "digraph", want: "list-a list-b\n"},
		{name: "custom template", format: "{{.IssueID}} -> {{.DependsOnID}} [{{.Type}}]", want: "list-a -> list-b [blocks]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			if err := outputFormattedList(&out, issues, deps, tc.format); err != nil {
				t.Fatalf("outputFormattedList: %v", err)
			}
			if got := out.String(); got != tc.want {
				t.Fatalf("formatted output bytes differ: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOutputFormattedListWriterErrors(t *testing.T) {
	t.Parallel()
	issues, deps := listOutputFixture()

	for _, tc := range []struct {
		name        string
		format      string
		failAt      int
		wantContext string
	}{
		{name: "DOT", format: "dot", failAt: 3, wantContext: "writing DOT output"},
		{name: "digraph preset", format: "digraph", failAt: 1, wantContext: "writing formatted list output"},
		{name: "custom template", format: "{{.IssueID}} {{.DependsOnID}}", failAt: 1, wantContext: "writing formatted list output"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			writer := &graphFailWriter{err: io.ErrClosedPipe, failAt: tc.failAt}

			err := outputFormattedList(writer, issues, deps, tc.format)
			if !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("outputFormattedList error = %v, want %v", err, io.ErrClosedPipe)
			}
			if !strings.Contains(err.Error(), tc.wantContext) {
				t.Fatalf("outputFormattedList error = %q, want context %q", err, tc.wantContext)
			}
			if writer.writes != writer.failAt {
				t.Fatalf("outputFormattedList made %d writes after failure at %d", writer.writes, writer.failAt)
			}
		})
	}
}

func TestOutputFormattedListPreservesFirstWriterError(t *testing.T) {
	t.Parallel()
	issues := []*types.Issue{{ID: "first"}, {ID: "second"}, {ID: "target"}}
	deps := map[string][]*types.Dependency{
		"first":  {{IssueID: "first", DependsOnID: "target", Type: types.DepBlocks}},
		"second": {{IssueID: "second", DependsOnID: "target", Type: types.DepBlocks}},
	}
	writer := &graphFailWriter{err: io.ErrClosedPipe, failAt: 1}
	format := `{{if eq .IssueID "second"}}{{index .Issue.Labels 0}}{{else}}{{.IssueID}}{{end}}`

	err := outputFormattedList(writer, issues, deps, format)
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("outputFormattedList error = %v, want first writer error %v", err, io.ErrClosedPipe)
	}
	if writer.writes != 1 {
		t.Fatalf("outputFormattedList made %d writes, want one failing write", writer.writes)
	}
}

func TestOutputFormattedListBuffersTemplateExecution(t *testing.T) {
	t.Parallel()
	issues := []*types.Issue{{ID: "source"}, {ID: "target"}}
	deps := map[string][]*types.Dependency{
		"source": {{IssueID: "source", DependsOnID: "target", Type: types.DepBlocks}},
	}
	var out bytes.Buffer

	err := outputFormattedList(&out, issues, deps, "prefix{{index .Issue.Labels 0}}")
	if err == nil || !strings.Contains(err.Error(), "template execution error") {
		t.Fatalf("outputFormattedList error = %v, want template execution error", err)
	}
	if out.Len() != 0 {
		t.Fatalf("failed template leaked partial output %q", out.String())
	}
}

// GH#5722: the --format help promises output scoped to the dependencies whose
// endpoints are both in the listing. These pin the two halves of that promise
// that make empty output at exit 0 correct rather than a broken flag.
func TestOutputFormattedListEdgeScope(t *testing.T) {
	t.Parallel()

	t.Run("edge leaving the listing", func(t *testing.T) {
		t.Parallel()
		issues := []*types.Issue{{ID: "inside"}}
		deps := map[string][]*types.Dependency{
			"inside": {{IssueID: "inside", DependsOnID: "outside", Type: types.DepBlocks}},
		}

		for _, format := range []string{"digraph", "{{.IssueID}} {{.DependsOnID}}"} {
			var out bytes.Buffer
			if err := outputFormattedList(&out, issues, deps, format); err != nil {
				t.Fatalf("outputFormattedList(%q): %v", format, err)
			}
			if out.Len() != 0 {
				t.Errorf("format %q printed %q for an edge leaving the listing, want nothing", format, out.String())
			}
		}
	})

	t.Run("template context is the edge", func(t *testing.T) {
		t.Parallel()
		issues, deps := listOutputFixture()
		var out bytes.Buffer

		if err := outputFormattedList(&out, issues, deps, "{{.ID}}"); err != nil {
			t.Fatalf("outputFormattedList: %v", err)
		}
		if got, want := out.String(), "<no value>\n"; got != want {
			t.Fatalf("{{.ID}} rendered %q, want %q: the template data is the edge, so there is no top-level .ID", got, want)
		}
	})
}

func TestOutputFormattedListWithNoEdgesDoesNotWrite(t *testing.T) {
	t.Parallel()
	writer := &graphFailWriter{err: io.ErrClosedPipe, failAt: 1}

	if err := outputFormattedList(writer, []*types.Issue{{ID: "solo"}}, nil, "digraph"); err != nil {
		t.Fatalf("outputFormattedList: %v", err)
	}
	if writer.writes != 0 {
		t.Fatalf("zero-edge output made %d writes, want 0", writer.writes)
	}
}
