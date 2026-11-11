// Package fsm provides a minimal deterministic finite state machine.
// It is designed for orchestration multi-step decision pipelines where.
// every transition must be explicit and observable.
package fsm

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// State identifies a node in a finite state machine.
type State string

// ErrNoTransition is returned when a state has no registered handler.
var ErrNoTransition = errors.New("fsm: no transition registered for state")

// Handler executes a state and returns the next state.
type Handler[T any] func(ctx context.Context, data *T) (State, error)

// Transition records a state-machine hop for observability.
type Transition struct {
	From     State
	To       State
	Duration time.Duration
	Err      error
}

// Machine is a deterministic FSM. Build it once and reuse it across goroutines.
// Run state lives in the data argument, not in the machine.
type Machine[T any] struct {
	initial   State
	terminals map[State]bool
	handlers  map[State]Handler[T]
	fallbacks map[State]State
	maxHops   int
}

// Option configures a Machine.
type Option[T any] func(*Machine[T])

// WithMaxHops sets the maximum number of state transitions.
func WithMaxHops[T any](n int) Option[T] {
	return func(machine *Machine[T]) {
		machine.maxHops = n
	}
}

// New creates a Machine with the given initial state and options.
func New[T any](initial State, opts ...Option[T]) *Machine[T] {
	machine := &Machine[T]{
		initial:   initial,
		terminals: make(map[State]bool),
		handlers:  make(map[State]Handler[T]),
		fallbacks: make(map[State]State),
		maxHops:   32,
	}

	for _, opt := range opts {
		opt(machine)
	}

	return machine
}

// Handle registers a handler for a state.
func (machine *Machine[T]) Handle(state State, handler Handler[T]) *Machine[T] {
	machine.handlers[state] = handler
	return machine
}

// Terminal marks states as terminal.
func (machine *Machine[T]) Terminal(states ...State) *Machine[T] {
	for _, state := range states {
		machine.terminals[state] = true
	}
	return machine
}

// FallbackTo registers a daterministic scape hatch. When the handler of
// state returns an error, the machine moves to target instead of aborting.
func (machine *Machine[T]) FallbackTo(states State, target State) *Machine[T] {
	machine.fallbacks[states] = target
	return machine
}

// Run executes handlers starting at the machine's initial state.
func (m *Machine[T]) Run(ctx context.Context, data *T) ([]Transition, error) {
	var trace = make([]Transition, 0, 8)
	current := m.initial

	for hops := 0; hops < m.maxHops; hops++ {
		if m.terminals[current] {
			return trace, nil
		}

		if err := ctx.Err(); err != nil {
			return trace, fmt.Errorf("fsm: context canceled at %q: %w", current, err)
		}

		handler, ok := m.handlers[current]
		if !ok {
			return trace, fmt.Errorf("%w: %q", ErrNoTransition, current)
		}

		started := time.Now()
		next, err := handler(ctx, data)
		hop := Transition{
			From:     current,
			To:       next,
			Duration: time.Since(started),
			Err:      err,
		}

		if err != nil {
			fallback, hasFallback := m.fallbacks[current]
			if !hasFallback {
				trace = append(trace, hop)
				return trace, fmt.Errorf("fsm: handler %q: %w", current, err)
			}
			hop.To = fallback
			next = fallback
		}
		trace = append(trace, hop)
		current = next
	}

	if m.terminals[current] {
		return trace, nil
	}

	return trace, fmt.Errorf("fsm: exceeded %d hops", m.maxHops)
}
