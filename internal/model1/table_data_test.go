// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package model1

import (
	"log/slog"
	"testing"

	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/config"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/sets"
)

func init() {
	slog.SetDefault(slog.New(slog.DiscardHandler))
}

// TestTableData_MultiContextNoDedup asserts that rows with the same Row.ID
// but different Source no longer collide on the rowEvents storage key.
// Regression for the post-P1 "duplicates shrink over a few ticks" bug:
// before StoreKey-based indexing, three identically-named resources from
// different kubeconfig contexts collapsed into one slot and were
// progressively removed by the Delete-by-ID pass.
func TestTableData_MultiContextNoDedup(t *testing.T) {
	header := Header{HeaderColumn{Name: "NAME"}}
	rows := Rows{
		{ID: "kube-system/coredns", Fields: Fields{"coredns"}, Source: "ctxA"},
		{ID: "kube-system/coredns", Fields: Fields{"coredns"}, Source: "ctxB"},
		{ID: "kube-system/coredns", Fields: Fields{"coredns"}, Source: "ctxC"},
		{ID: "infra/cert-manager", Fields: Fields{"cert-manager"}, Source: "ctxA"},
	}
	td := NewTableDataWithRows(client.PodGVR, header, NewRowEvents(0))

	// First refresh: empty-table bulk-add path.
	td.Update(rows)
	assert.Equal(t, 4, td.RowCount(), "all 4 rows must survive initial Update")
	gotSources := collectSources(td, "kube-system/coredns")
	assert.ElementsMatch(t, []string{"ctxA", "ctxB", "ctxC"}, gotSources)

	// Second refresh with the same set: dedup pass must not drop any row.
	td.Update(rows)
	assert.Equal(t, 4, td.RowCount(), "no rows dropped after second tick")

	// Third refresh: drop ctxB's coredns. Only the ctxB row should disappear.
	without := Rows{
		{ID: "kube-system/coredns", Fields: Fields{"coredns"}, Source: "ctxA"},
		{ID: "kube-system/coredns", Fields: Fields{"coredns"}, Source: "ctxC"},
		{ID: "infra/cert-manager", Fields: Fields{"cert-manager"}, Source: "ctxA"},
	}
	td.Update(without)
	assert.Equal(t, 3, td.RowCount())
	gotSources = collectSources(td, "kube-system/coredns")
	assert.ElementsMatch(t, []string{"ctxA", "ctxC"}, gotSources)
}

func collectSources(td *TableData, fqn string) []string {
	var out []string
	td.GetRowEvents().Range(func(_ int, re RowEvent) bool {
		if re.Row.ID == fqn {
			out = append(out, re.Row.Source)
		}
		return true
	})
	return out
}

func TestTableDataFilter_ContextSelector(t *testing.T) {
	header := Header{
		HeaderColumn{Name: "NAME"},
	}
	rows := NewRowEventsWithEvts(
		RowEvent{Row: Row{ID: "a", Fields: Fields{"pod-a"}, Source: "ctxA"}},
		RowEvent{Row: Row{ID: "b", Fields: Fields{"pod-b"}, Source: "ctxB"}},
		RowEvent{Row: Row{ID: "c", Fields: Fields{"pod-c"}, Source: "ctxA"}},
	)

	uu := map[string]struct {
		filter   string
		wantIDs  []string
	}{
		"keep_only_ctxA": {filter: "ctx=ctxA", wantIDs: []string{"a", "c"}},
		"keep_only_ctxB": {filter: "ctx=ctxB", wantIDs: []string{"b"}},
		"drop_ctxA":      {filter: "!ctx=ctxA", wantIDs: []string{"b"}},
		"unknown_ctx":    {filter: "ctx=ctxZ", wantIDs: nil},
	}
	for k := range uu {
		u := uu[k]
		t.Run(k, func(t *testing.T) {
			td := NewTableDataWithRows(client.PodGVR, header, rows)
			out := td.Filter(FilterOpts{Filter: u.filter})
			var got []string
			out.GetRowEvents().Range(func(_ int, re RowEvent) bool {
				got = append(got, re.Row.ID)
				return true
			})
			assert.Equal(t, u.wantIDs, got)
		})
	}
}

func TestTableDataComputeSortCol(t *testing.T) {
	uu := map[string]struct {
		t1           *TableData
		vs           config.ViewSetting
		sc           SortColumn
		wide, manual bool
		e            SortColumn
	}{
		"same": {
			t1: NewTableDataWithRows(
				client.NewGVR("test"),
				Header{
					HeaderColumn{Name: "A"},
					HeaderColumn{Name: "B"},
					HeaderColumn{Name: "C"},
				},
				NewRowEventsWithEvts(
					RowEvent{Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
					RowEvent{Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
					RowEvent{Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
				),
			),
			vs: config.ViewSetting{Columns: []string{"A", "B", "C"}, SortColumn: "A:asc"},
			e:  SortColumn{Name: "A", ASC: true},
		},
		"wide-col": {
			t1: NewTableDataWithRows(
				client.NewGVR("test"),
				Header{
					HeaderColumn{Name: "A"},
					HeaderColumn{Name: "B", Attrs: Attrs{Wide: true}},
					HeaderColumn{Name: "C"},
				},
				NewRowEventsWithEvts(
					RowEvent{Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
					RowEvent{Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
					RowEvent{Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
				),
			),
			vs: config.ViewSetting{Columns: []string{"A", "B", "C"}, SortColumn: "B:desc"},
			e:  SortColumn{Name: "B"},
		},

		"wide": {
			t1: NewTableDataWithRows(
				client.NewGVR("test"),
				Header{
					HeaderColumn{Name: "A"},
					HeaderColumn{Name: "B", Attrs: Attrs{Wide: true}},
					HeaderColumn{Name: "C"},
				},
				NewRowEventsWithEvts(
					RowEvent{Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
					RowEvent{Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
					RowEvent{Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
				),
			),
			wide: true,
			vs:   config.ViewSetting{Columns: []string{"A", "C"}, SortColumn: ""},
			e:    SortColumn{Name: ""},
		},
	}

	for k := range uu {
		u := uu[k]
		t.Run(k, func(t *testing.T) {
			sc := u.t1.ComputeSortCol(&u.vs, u.sc, u.manual)
			assert.Equal(t, u.e, sc)
		})
	}
}

func TestTableDataDiff(t *testing.T) {
	uu := map[string]struct {
		t1, t2 *TableData
		e      bool
	}{
		"empty": {
			t1: NewTableDataWithRows(
				client.NewGVR("test"),
				Header{
					HeaderColumn{Name: "A"},
					HeaderColumn{Name: "B"},
					HeaderColumn{Name: "C"},
				},
				NewRowEventsWithEvts(
					RowEvent{Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
					RowEvent{Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
					RowEvent{Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
				),
			),
			e: true,
		},
		"same": {
			t1: NewTableDataWithRows(
				client.NewGVR("test"),
				Header{
					HeaderColumn{Name: "A"},
					HeaderColumn{Name: "B"},
					HeaderColumn{Name: "C"},
				},
				NewRowEventsWithEvts(
					RowEvent{Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
					RowEvent{Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
					RowEvent{Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
				),
			),
			t2: NewTableDataWithRows(
				client.NewGVR("test"),
				Header{
					HeaderColumn{Name: "A"},
					HeaderColumn{Name: "B"},
					HeaderColumn{Name: "C"},
				},
				NewRowEventsWithEvts(
					RowEvent{Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
					RowEvent{Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
					RowEvent{Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
				),
			),
		},
		"ns-diff": {
			t1: NewTableDataFull(
				client.NewGVR("test"),
				"ns1",
				Header{
					HeaderColumn{Name: "A"},
					HeaderColumn{Name: "B"},
					HeaderColumn{Name: "C"},
				},
				NewRowEventsWithEvts(
					RowEvent{Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
					RowEvent{Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
					RowEvent{Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
				),
			),
			t2: NewTableDataFull(
				client.NewGVR("test"),
				"ns-2",
				Header{
					HeaderColumn{Name: "A"},
					HeaderColumn{Name: "B"},
					HeaderColumn{Name: "C"},
				},
				NewRowEventsWithEvts(
					RowEvent{Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
					RowEvent{Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
					RowEvent{Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
				),
			),
			e: true,
		},
		"header-diff": {
			t1: NewTableDataWithRows(
				client.NewGVR("test"),
				Header{
					HeaderColumn{Name: "A"},
					HeaderColumn{Name: "D"},
					HeaderColumn{Name: "C"},
				},
				NewRowEventsWithEvts(
					RowEvent{Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
					RowEvent{Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
					RowEvent{Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
				),
			),
			t2: NewTableDataWithRows(
				client.NewGVR("test"),
				Header{
					HeaderColumn{Name: "A"},
					HeaderColumn{Name: "B"},
					HeaderColumn{Name: "C"},
				},
				NewRowEventsWithEvts(
					RowEvent{Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
					RowEvent{Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
					RowEvent{Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
				),
			),
			e: true,
		},
		"row-diff": {
			t1: NewTableDataWithRows(
				client.NewGVR("test"),
				Header{
					HeaderColumn{Name: "A"},
					HeaderColumn{Name: "B"},
					HeaderColumn{Name: "C"},
				},
				NewRowEventsWithEvts(
					RowEvent{Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
					RowEvent{Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
					RowEvent{Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
				),
			),
			t2: NewTableDataWithRows(
				client.NewGVR("test"),
				Header{
					HeaderColumn{Name: "A"},
					HeaderColumn{Name: "B"},
					HeaderColumn{Name: "C"},
				},
				NewRowEventsWithEvts(
					RowEvent{Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
					RowEvent{Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
					RowEvent{Row: Row{ID: "C", Fields: Fields{"100", "2", "3"}}},
				),
			),
			e: true,
		},
	}

	for k := range uu {
		u := uu[k]
		t.Run(k, func(t *testing.T) {
			assert.Equal(t, u.e, u.t1.Diff(u.t2))
		})
	}
}

func TestTableDataUpdate(t *testing.T) {
	uu := map[string]struct {
		re, e *RowEvents
		rr    Rows
	}{
		"no-change": {
			re: NewRowEventsWithEvts(
				RowEvent{Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
				RowEvent{Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
				RowEvent{Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
			),
			rr: Rows{
				Row{ID: "A", Fields: Fields{"1", "2", "3"}},
				Row{ID: "B", Fields: Fields{"0", "2", "3"}},
				Row{ID: "C", Fields: Fields{"10", "2", "3"}},
			},
			e: NewRowEventsWithEvts(
				RowEvent{Kind: EventUnchanged, Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
				RowEvent{Kind: EventUnchanged, Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
				RowEvent{Kind: EventUnchanged, Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
			),
		},
		"add": {
			re: NewRowEventsWithEvts(
				RowEvent{Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
				RowEvent{Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
				RowEvent{Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
			),
			rr: Rows{
				Row{ID: "A", Fields: Fields{"1", "2", "3"}},
				Row{ID: "B", Fields: Fields{"0", "2", "3"}},
				Row{ID: "C", Fields: Fields{"10", "2", "3"}},
				Row{ID: "D", Fields: Fields{"10", "2", "3"}},
			},
			e: NewRowEventsWithEvts(
				RowEvent{Kind: EventUnchanged, Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
				RowEvent{Kind: EventUnchanged, Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
				RowEvent{Kind: EventUnchanged, Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
				RowEvent{Kind: EventAdd, Row: Row{ID: "D", Fields: Fields{"10", "2", "3"}}},
			),
		},
		"delete": {
			re: NewRowEventsWithEvts(
				RowEvent{Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
				RowEvent{Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
				RowEvent{Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
			),
			rr: Rows{
				Row{ID: "A", Fields: Fields{"1", "2", "3"}},
				Row{ID: "C", Fields: Fields{"10", "2", "3"}},
			},
			e: NewRowEventsWithEvts(
				RowEvent{Kind: EventUnchanged, Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
				RowEvent{Kind: EventUnchanged, Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
			),
		},
		"update": {
			re: NewRowEventsWithEvts(
				RowEvent{Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
				RowEvent{Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
				RowEvent{Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
			),
			rr: Rows{
				Row{ID: "A", Fields: Fields{"10", "2", "3"}},
				Row{ID: "B", Fields: Fields{"0", "2", "3"}},
				Row{ID: "C", Fields: Fields{"10", "2", "3"}},
			},
			e: NewRowEventsWithEvts(
				RowEvent{
					Kind:   EventUpdate,
					Row:    Row{ID: "A", Fields: Fields{"10", "2", "3"}},
					Deltas: DeltaRow{"1", "", ""},
				},
				RowEvent{Kind: EventUnchanged, Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
				RowEvent{Kind: EventUnchanged, Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
			),
		},
	}

	var table TableData
	for k := range uu {
		u := uu[k]
		t.Run(k, func(t *testing.T) {
			table.SetRowEvents(u.re)
			table.Update(u.rr)
			assert.Equal(t, u.e, table.GetRowEvents())
		})
	}
}

func TestTableDataDelete(t *testing.T) {
	uu := map[string]struct {
		re, e *RowEvents
		kk    sets.Set[string]
	}{
		"ordered": {
			re: NewRowEventsWithEvts(
				RowEvent{Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
				RowEvent{Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
				RowEvent{Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
			),
			kk: sets.New[string]("A", "C"),
			e: NewRowEventsWithEvts(
				RowEvent{Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
				RowEvent{Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
			),
		},
		"unordered": {
			re: NewRowEventsWithEvts(
				RowEvent{Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
				RowEvent{Row: Row{ID: "B", Fields: Fields{"0", "2", "3"}}},
				RowEvent{Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
				RowEvent{Row: Row{ID: "D", Fields: Fields{"10", "2", "3"}}},
			),
			kk: sets.New[string]("C", "A"),
			e: NewRowEventsWithEvts(
				RowEvent{Row: Row{ID: "A", Fields: Fields{"1", "2", "3"}}},
				RowEvent{Row: Row{ID: "C", Fields: Fields{"10", "2", "3"}}},
			),
		},
	}

	var table TableData
	for k := range uu {
		u := uu[k]
		t.Run(k, func(t *testing.T) {
			table.SetRowEvents(u.re)
			table.Delete(u.kk)
			assert.Equal(t, u.e, table.GetRowEvents())
		})
	}
}
