package task

import "testing"

func TestEvmLogHasConfirmations(t *testing.T) {
	tests := []struct {
		name             string
		latestBlock      uint64
		eventBlock       uint64
		minConfirmations int
		want             bool
	}{
		{name: "first confirmation", latestBlock: 100, eventBlock: 100, minConfirmations: 1, want: true},
		{name: "insufficient confirmations", latestBlock: 101, eventBlock: 100, minConfirmations: 3, want: false},
		{name: "exact confirmations", latestBlock: 102, eventBlock: 100, minConfirmations: 3, want: true},
		{name: "future block", latestBlock: 99, eventBlock: 100, minConfirmations: 1, want: false},
		{name: "invalid config defaults to one", latestBlock: 100, eventBlock: 100, minConfirmations: 0, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := evmLogHasConfirmations(tt.latestBlock, tt.eventBlock, tt.minConfirmations); got != tt.want {
				t.Fatalf("evmLogHasConfirmations() = %v, want %v", got, tt.want)
			}
		})
	}
}
