package trends

import "testing"

func TestComputeStreak(t *testing.T) {
	tests := []struct {
		name   string
		values []bool
		want   int
	}{
		{name: "all true", values: []bool{true, true, true}, want: 3},
		{name: "break on first false", values: []bool{false, true, true}, want: 0},
		{name: "break mid series", values: []bool{true, true, false, true}, want: 2},
		{name: "empty", values: nil, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ComputeStreak(tt.values); got != tt.want {
				t.Fatalf("ComputeStreak() = %d, want %d", got, tt.want)
			}
		})
	}
}
