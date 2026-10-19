package mcpserver

import (
	"reflect"
	"testing"
)

// TestMoveElements moves elements in turn, each named by its index as the
// template is now, keeping track of where each went.
func TestMoveElements(t *testing.T) {
	elements := []any{"a", "b", "c", "d"}
	for _, tc := range []struct {
		moves []elementMove
		want  []any
		order []int
	}{
		{nil, []any{"a", "b", "c", "d"}, []int{0, 1, 2, 3}},
		{[]elementMove{{Index: 0, To: 99}}, []any{"b", "c", "d", "a"}, []int{1, 2, 3, 0}},
		{[]elementMove{{Index: 3, To: 0}}, []any{"d", "a", "b", "c"}, []int{3, 0, 1, 2}},
		// The second move names "a" by its index as given, wherever it is now.
		{[]elementMove{{Index: 2, To: 0}, {Index: 0, To: -5}}, []any{"a", "c", "b", "d"}, []int{0, 2, 1, 3}},
	} {
		got, order, err := moveElements(elements, tc.moves)
		if err != nil || !reflect.DeepEqual(got, tc.want) || !reflect.DeepEqual(order, tc.order) {
			t.Errorf("%+v: %v %v %v", tc.moves, got, order, err)
		}
	}
	if _, _, err := moveElements(elements, []elementMove{{Index: 4}}); err == nil {
		t.Error("moved an element that is not there")
	}
}
