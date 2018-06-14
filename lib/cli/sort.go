package cli

import (
	"sort"

	"github.com/nedscode/transit/proto"
)

type uint64Slice []uint64
func (p uint64Slice) Len() int           { return len(p) }
func (p uint64Slice) Less(i, j int) bool { return p[i] < p[j] }
func (p uint64Slice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
func (p uint64Slice) Sort()              { sort.Sort(p) }

type boxSlice []*transit.Box
func (b boxSlice) Len() int           { return len(b) }
func (b boxSlice) Less(i, j int) bool { return len(b[i].States) > len(b[j].States) }
func (b boxSlice) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b boxSlice) Sort()              { sort.Sort(b) }
