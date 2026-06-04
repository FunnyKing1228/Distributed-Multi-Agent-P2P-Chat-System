package clock

import "testing"

func TestCompareVectorClocks(t *testing.T) {
	tests := []struct {
		name string
		a    map[string]uint64
		b    map[string]uint64
		want Ordering
	}{
		{
			name: "before",
			a:    map[string]uint64{"a": 1},
			b:    map[string]uint64{"a": 2},
			want: Before,
		},
		{
			name: "after",
			a:    map[string]uint64{"a": 2, "b": 1},
			b:    map[string]uint64{"a": 1, "b": 1},
			want: After,
		},
		{
			name: "concurrent",
			a:    map[string]uint64{"a": 2, "b": 1},
			b:    map[string]uint64{"a": 1, "b": 2},
			want: Concurrent,
		},
		{
			name: "equal",
			a:    map[string]uint64{"a": 1, "b": 2},
			b:    map[string]uint64{"b": 2, "a": 1},
			want: Equal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compare(tt.a, tt.b); got != tt.want {
				t.Fatalf("Compare() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVectorClockMergeAndSnapshot(t *testing.T) {
	vc := New()
	vc.Increment("peer-a")
	vc.Merge(map[string]uint64{"peer-a": 1, "peer-b": 3})

	snap := vc.Snapshot()
	if snap["peer-a"] != 1 || snap["peer-b"] != 3 {
		t.Fatalf("unexpected snapshot: %#v", snap)
	}

	snap["peer-b"] = 99
	if got := vc.Snapshot()["peer-b"]; got != 3 {
		t.Fatalf("snapshot mutated original clock, got %d", got)
	}
}
