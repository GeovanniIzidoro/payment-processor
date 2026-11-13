package batch_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huncoding/payment-processor-course/pkg/batch"
)

func TestSubmitAfterClose(t *testing.T) {
	echo := func(_ context.Context, items []int) ([]int, error) { return items, nil }
	b := batch.New(2, time.Millisecond, echo)
	b.Close()
	b.Close()

	_, err := b.Submit(context.Background(), 1)
	if !errors.Is(err, batch.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

func TestNewEnforcesMinimumBatchSize(t *testing.T) {
	echo := func(_ context.Context, items []int) ([]int, error) { return items, nil }
	b := batch.New(0, time.Hour, echo)
	defer b.Close()

	got, err := b.Submit(context.Background(), 7)
	if err != nil {
		t.Fatalf("Submit returned error: %v", err)
	}
	if got != 7 {
		t.Errorf("Submit returned %d, want 7", got)
	}
}

func TestSubmitWithCanceledContext(t *testing.T) {
	echo := func(_ context.Context, items []int) ([]int, error) { return items, nil }
	b := batch.New(100, time.Hour, echo)
	defer b.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := b.Submit(ctx, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestBatchBySize(t *testing.T) {
	var calls atomic.Int32
	double := func(_ context.Context, items []int) ([]int, error) {
		calls.Add(1)
		results := make([]int, len(items))
		for i, item := range items {
			results[i] = item * 2
		}
		return results, nil
	}

	b := batch.New(4, time.Second, double)
	defer b.Close()

	results := make([]int, 8)
	var wg sync.WaitGroup
	wg.Add(len(results))
	for i := range results {
		go func(i int) {
			defer wg.Done()
			result, err := b.Submit(context.Background(), i)
			if err != nil {
				t.Errorf("Submit(%d) returned error: %v", i, err)
				return
			}
			results[i] = result
		}(i)
	}
	wg.Wait()

	for i, result := range results {
		if want := i * 2; result != want {
			t.Errorf("result[%d] = %d, want %d", i, result, want)
		}
	}
	if count := calls.Load(); count > 4 {
		t.Errorf("flusher called %d times, want at most 4", count)
	}
}

func TestSubmitReturnsFlusherErrors(t *testing.T) {
	flusherErr := errors.New("flusher failed")
	tests := []struct {
		name        string
		flusher     batch.Flusher[int, int]
		wantErr     error
		wantErrText string
	}{
		{
			name: "flusher error",
			flusher: func(_ context.Context, items []int) ([]int, error) {
				return []int{items[0]}, flusherErr
			},
			wantErr: flusherErr,
		},
		{
			name: "flusher error with wrong result count",
			flusher: func(_ context.Context, _ []int) ([]int, error) {
				return nil, flusherErr
			},
			wantErr: flusherErr,
		},
		{
			name: "wrong result count",
			flusher: func(_ context.Context, _ []int) ([]int, error) {
				return nil, nil
			},
			wantErrText: "batch: flusher returned wrong number of results",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := batch.New(1, time.Hour, tt.flusher)
			defer b.Close()

			_, err := b.Submit(context.Background(), 1)
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("Submit error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErrText != "" && (err == nil || err.Error() != tt.wantErrText) {
				t.Fatalf("Submit error = %v, want %q", err, tt.wantErrText)
			}
		})
	}
}

func TestFlushesByTimeWindow(t *testing.T) {
	echo := func(_ context.Context, items []string) ([]string, error) {
		return items, nil
	}

	b := batch.New(100, 20*time.Millisecond, echo)
	defer b.Close()

	start := time.Now()
	result, err := b.Submit(context.Background(), "lonely")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Submit returned error: %v", err)
	}
	if result != "lonely" {
		t.Errorf("result = %q, want %q", result, "lonely")
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("Submit took %v, want at most 500ms", elapsed)
	}
}
