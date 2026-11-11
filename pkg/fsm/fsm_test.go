package fsm_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/huncoding/payment-processor-course/pkg/fsm"
)

type payload struct {
	visited []string
	score   float64
}

func record(p *payload, name string) {
	p.visited = append(p.visited, name)
}

func TestRunHappyPath(t *testing.T) {
	p := payload{}
	m := fsm.New[payload]("start").
		Handle("start", func(_ context.Context, p *payload) (fsm.State, error) {
			record(p, "start")
			return "score", nil
		}).
		Handle("score", func(_ context.Context, p *payload) (fsm.State, error) {
			record(p, "score")
			p.score = 0.9
			return "done", nil
		}).
		Terminal("done")

	trace, err := m.Run(context.Background(), &p)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(trace) != 2 {
		t.Fatalf("len(trace) = %d, want 2", len(trace))
	}
	if p.score != 0.9 {
		t.Fatalf("p.score = %v, want 0.9", p.score)
	}
}

func TestRunExceedsMaxHops(t *testing.T) {
	p := payload{}
	m := fsm.New[payload]("a", fsm.WithMaxHops[payload](3)).
		Handle("a", func(_ context.Context, p *payload) (fsm.State, error) {
			record(p, "a")
			return "b", nil
		}).
		Handle("b", func(_ context.Context, p *payload) (fsm.State, error) {
			record(p, "b")
			return "a", nil
		}).
		Terminal("done")

	_, err := m.Run(context.Background(), &p)
	if err == nil {
		t.Fatal("Run() error = nil, want max hops error")
	}
	if !strings.Contains(err.Error(), "exceeded 3 hops") {
		t.Fatalf("Run() error = %q, want it to contain %q", err, "exceeded 3 hops")
	}
	if len(p.visited) != 3 {
		t.Fatalf("len(p.visited) = %d, want 3", len(p.visited))
	}
}

func TestRunContextCanceled(t *testing.T) {
	p := payload{}
	m := fsm.New[payload]("start").
		Handle("start", func(_ context.Context, p *payload) (fsm.State, error) {
			record(p, "start")
			return "done", nil
		}).
		Terminal("done")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.Run(ctx, &p)
	if err == nil {
		t.Fatal("Run() error = nil, want context cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(Run() error, context.Canceled) = false; error = %v", err)
	}
	if len(p.visited) != 0 {
		t.Fatalf("len(p.visited) = %d, want 0", len(p.visited))
	}
}

func TestRunNoHandler(t *testing.T) {
	p := payload{}
	m := fsm.New[payload]("start").Terminal("done")

	_, err := m.Run(context.Background(), &p)
	if err == nil {
		t.Fatal("Run() error = nil, want no transition error")
	}
	if !errors.Is(err, fsm.ErrNoTransition) {
		t.Fatalf("errors.Is(Run() error, fsm.ErrNoTransition) = false; error = %v", err)
	}
}

func TestRunReachesTerminalOnLastHop(t *testing.T) {
	p := payload{}
	m := fsm.New[payload]("start", fsm.WithMaxHops[payload](2)).
		Handle("start", func(_ context.Context, p *payload) (fsm.State, error) {
			record(p, "start")
			return "score", nil
		}).
		Handle("score", func(_ context.Context, p *payload) (fsm.State, error) {
			record(p, "score")
			return "done", nil
		}).
		Terminal("done")

	trace, err := m.Run(context.Background(), &p)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(trace) != 2 {
		t.Fatalf("len(trace) = %d, want 2", len(trace))
	}
	if len(p.visited) != 2 {
		t.Fatalf("len(p.visited) = %d, want 2", len(p.visited))
	}
}

func TestFallbackKeepsPipelineAvailable(t *testing.T) {
	dependencyDown := errors.New("upstream timeout")
	p := payload{}
	m := fsm.New[payload]("start").
		Handle("start", func(_ context.Context, p *payload) (fsm.State, error) {
			record(p, "start")
			return "ml", nil
		}).
		Handle("ml", func(_ context.Context, _ *payload) (fsm.State, error) {
			return "", dependencyDown
		}).
		FallbackTo("ml", "rules_only").
		Handle("rules_only", func(_ context.Context, p *payload) (fsm.State, error) {
			record(p, "rules_only")
			return "done", nil
		}).
		Terminal("done")

	trace, err := m.Run(context.Background(), &p)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil after fallback", err)
	}
	if len(p.visited) == 0 || p.visited[len(p.visited)-1] != "rules_only" {
		t.Fatalf("p.visited = %v, want last item %q", p.visited, "rules_only")
	}
	if len(trace) < 2 {
		t.Fatalf("len(trace) = %d, want at least 2", len(trace))
	}
	if trace[1].Err == nil {
		t.Fatal("trace[1].Err = nil, want original handler error")
	}
}

func TestErrorWithoutFallbackAborts(t *testing.T) {
	boom := errors.New("boom")
	p := payload{}
	m := fsm.New[payload]("start").
		Handle("start", func(_ context.Context, _ *payload) (fsm.State, error) {
			return "", boom
		}).
		Terminal("done")

	_, err := m.Run(context.Background(), &p)
	if !errors.Is(err, boom) {
		t.Fatalf("errors.Is(Run() error, boom) = false; error = %v", err)
	}
}
