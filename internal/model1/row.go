// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package model1

// Row represents a collection of columns.
type Row struct {
	ID     string
	Fields Fields
	// Source identifies the row's source context name in multi-context mode.
	// Empty in single-context mode.
	Source string
}

// RowIdent identifies a row across contexts.
type RowIdent struct {
	ID     string
	Source string
}

// StoreKey is the dedup key TableData uses for the rowEvents index. In
// single-context mode it's just Row.ID (so existing single-cluster behavior
// is untouched). In multi-context mode it's "<Source>@<ID>", which prevents
// same-FQN resources from different kubeconfig contexts (e.g.
// kube-system/coredns in cluster-a vs cluster-b) from colliding into a
// single index slot and getting progressively deduped over refresh ticks.
// Uses '@' rather than '/' because '/' is part of the FQN syntax — '@'
// reads as "from" and won't collide with anything in the FQN namespace.
func (r Row) StoreKey() string {
	return storeKey(r.ID, r.Source)
}

// Ident returns the row's stable selection identity.
func (r Row) Ident() RowIdent {
	return RowIdent{ID: r.ID, Source: r.Source}
}

// StoreKey returns the dedup key matching Row.StoreKey.
func (r RowIdent) StoreKey() string {
	return storeKey(r.ID, r.Source)
}

func storeKey(id, source string) string {
	if source == "" {
		return id
	}
	return source + "@" + id
}

// NewRow returns a new row with initialized fields.
func NewRow(size int) Row {
	return Row{Fields: make([]string, size)}
}

// Labelize returns a new row based on labels.
func (r Row) Labelize(cols []int, labelCol int, labels []string) Row {
	out := NewRow(len(cols) + len(labels))
	for _, col := range cols {
		out.Fields = append(out.Fields, r.Fields[col])
	}
	m := labelize(r.Fields[labelCol])
	for _, label := range labels {
		out.Fields = append(out.Fields, m[label])
	}

	return out
}

// Customize returns a row subset based on given col indices.
func (r Row) Customize(cols []int) Row {
	out := NewRow(len(cols))
	r.Fields.Customize(cols, out.Fields)
	out.ID = r.ID
	out.Source = r.Source

	return out
}

// Diff returns true if row differ or false otherwise.
func (r Row) Diff(ro Row, ageCol int) bool {
	if r.ID != ro.ID {
		return true
	}
	if r.Source != ro.Source {
		return true
	}
	return r.Fields.Diff(ro.Fields, ageCol)
}

// Clone copies a row.
func (r Row) Clone() Row {
	return Row{
		ID:     r.ID,
		Fields: r.Fields.Clone(),
		Source: r.Source,
	}
}

// Len returns the length of the row.
func (r Row) Len() int {
	return len(r.Fields)
}

// ----------------------------------------------------------------------------

// RowSorter sorts rows.
type RowSorter struct {
	Rows       Rows
	Index      int
	IsNumber   bool
	IsDuration bool
	IsCapacity bool
	Asc        bool
}

func (s RowSorter) Len() int {
	return len(s.Rows)
}

func (s RowSorter) Swap(i, j int) {
	s.Rows[i], s.Rows[j] = s.Rows[j], s.Rows[i]
}

func (s RowSorter) Less(i, j int) bool {
	v1, v2 := s.Rows[i].Fields[s.Index], s.Rows[j].Fields[s.Index]
	id1, id2 := s.Rows[i].ID, s.Rows[j].ID
	less := Less(s.IsNumber, s.IsDuration, s.IsCapacity, id1, id2, v1, v2)
	if s.Asc {
		return less
	}
	return !less
}

// ----------------------------------------------------------------------------
// Helpers...
