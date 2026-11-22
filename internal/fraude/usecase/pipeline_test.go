package usecase_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/huncoding/payment-processor-course/internal/fraude/domain"
	"github.com/huncoding/payment-processor-course/internal/fraude/usecase"
	"github.com/huncoding/payment-processor-course/pkg/fsm"
	"github.com/stretchr/testify/mock"
)

type mockGeoResolver struct {
	mock.Mock
}

func (m *mockGeoResolver) CountryCode(ctx context.Context, ip string) (string, error) {
	args := m.Called(ctx, ip)
	return args.String(0), args.Error(1)
}

type mockEmbedder struct {
	mock.Mock
}

func (m *mockEmbedder) Embed(text string) []float64 {
	return m.Called(text).Get(0).([]float64)
}

type mockSemanticCache struct {
	mock.Mock
}

func (m *mockSemanticCache) Lookup(vector []float64) (domain.Verdict, float64, bool) {
	args := m.Called(vector)
	return args.Get(0).(domain.Verdict), args.Get(1).(float64), args.Bool(2)
}

func (m *mockSemanticCache) Store(vector []float64, verdict domain.Verdict) {
	m.Called(vector, verdict)
}

func TestPipelineScore(t *testing.T) {
	type transition struct {
		from    fsm.State
		to      fsm.State
		wantErr error
	}

	tests := []struct {
		name            string
		attempt         domain.Attempt
		modelValue      float64
		modelFeatures   int
		cacheVerdict    domain.Verdict
		cacheSimilarity float64
		cacheHit        bool
		geoErr          bool
		priorRuns       int
		want            usecase.Decision
		wantTrace       []transition
	}{
		{
			name: "fast rule final blocklist denial",
			attempt: domain.Attempt{
				CardFingerprint: "fp_stolen_card_001",
			},
			want: usecase.Decision{
				Verdict:   domain.VerdictDeny,
				RiskScore: 0,
				Reasons:   nil,
				Degraded:  true,
			},
			wantTrace: []transition{
				{from: usecase.StateEnrich, to: usecase.StateFastRules},
				{from: usecase.StateFastRules, to: usecase.StateDone},
			},
		},
		{
			name: "fast rule final amount review",
			attempt: domain.Attempt{
				AmountCents: domain.MaxAmountCents + 1,
			},
			want: usecase.Decision{
				Verdict:   domain.VerdictManualReview,
				RiskScore: 0,
				Reasons:   nil,
				Degraded:  true,
			},
			wantTrace: []transition{
				{from: usecase.StateEnrich, to: usecase.StateFastRules},
				{from: usecase.StateFastRules, to: usecase.StateDone},
			},
		},
		{
			name: "fast rule final velocity denial",
			attempt: domain.Attempt{
				CardFingerprint: "fp-velocity",
			},
			priorRuns: domain.VelocityPerMinLimit,
			want: usecase.Decision{
				Verdict:   domain.VerdictDeny,
				RiskScore: 0,
				Reasons:   nil,
				Degraded:  true,
			},
			wantTrace: []transition{
				{from: usecase.StateEnrich, to: usecase.StateFastRules},
				{from: usecase.StateFastRules, to: usecase.StateDone},
			},
		},
		{
			name:       "fast rules pass and confident ML approves",
			modelValue: -40,
			want: usecase.Decision{
				Verdict:   domain.VerdictApprove,
				RiskScore: 0,
				Reasons:   []string{"ml_score_confident"},
				Degraded:  false,
			},
			wantTrace: []transition{
				{from: usecase.StateEnrich, to: usecase.StateFastRules},
				{from: usecase.StateFastRules, to: usecase.StateMLScore},
				{from: usecase.StateMLScore, to: usecase.StateDone},
			},
		},
		{
			name:       "fast rules pass and confident ML denies",
			modelValue: 40,
			want: usecase.Decision{
				Verdict:   domain.VerdictDeny,
				RiskScore: 1,
				Reasons:   []string{"ml_score_confident"},
				Degraded:  false,
			},
			wantTrace: []transition{
				{from: usecase.StateEnrich, to: usecase.StateFastRules},
				{from: usecase.StateFastRules, to: usecase.StateMLScore},
				{from: usecase.StateMLScore, to: usecase.StateDone},
			},
		},
		{
			name:       "geo lookup failure still allows confident ML approval",
			modelValue: -40,
			geoErr:     true,
			want: usecase.Decision{
				Verdict:   domain.VerdictApprove,
				RiskScore: 0,
				Reasons:   []string{"ml_score_confident"},
				Degraded:  false,
			},
			wantTrace: []transition{
				{from: usecase.StateEnrich, to: usecase.StateFastRules},
				{from: usecase.StateFastRules, to: usecase.StateMLScore},
				{from: usecase.StateMLScore, to: usecase.StateDone},
			},
		},
		{
			name:            "ambiguous ML score resolved by semantic cache",
			cacheVerdict:    domain.VerdictApprove,
			cacheSimilarity: 0.91,
			cacheHit:        true,
			want: usecase.Decision{
				Verdict:   domain.VerdictApprove,
				RiskScore: 0.5,
				Reasons:   []string{"semantic_cache_hit:0.91"},
				Degraded:  false,
			},
			wantTrace: []transition{
				{from: usecase.StateEnrich, to: usecase.StateFastRules},
				{from: usecase.StateFastRules, to: usecase.StateMLScore},
				{from: usecase.StateMLScore, to: usecase.StateTiebreak},
				{from: usecase.StateTiebreak, to: usecase.StateFinalize},
				{from: usecase.StateFinalize, to: usecase.StateDone},
			},
		},
		{
			name:            "ambiguous ML score resolved by cache denial",
			cacheVerdict:    domain.VerdictDeny,
			cacheSimilarity: 0.82,
			cacheHit:        true,
			want: usecase.Decision{
				Verdict:   domain.VerdictDeny,
				RiskScore: 0.5,
				Reasons:   []string{"semantic_cache_hit:0.82"},
				Degraded:  false,
			},
			wantTrace: []transition{
				{from: usecase.StateEnrich, to: usecase.StateFastRules},
				{from: usecase.StateFastRules, to: usecase.StateMLScore},
				{from: usecase.StateMLScore, to: usecase.StateTiebreak},
				{from: usecase.StateTiebreak, to: usecase.StateFinalize},
				{from: usecase.StateFinalize, to: usecase.StateDone},
			},
		},
		{
			name: "ambiguous ML score misses semantic cache",
			want: usecase.Decision{
				Verdict:   domain.VerdictManualReview,
				RiskScore: 0.5,
				Reasons:   []string{"ambiguous_score_manual_review"},
				Degraded:  false,
			},
			wantTrace: []transition{
				{from: usecase.StateEnrich, to: usecase.StateFastRules},
				{from: usecase.StateFastRules, to: usecase.StateMLScore},
				{from: usecase.StateMLScore, to: usecase.StateTiebreak},
				{from: usecase.StateTiebreak, to: usecase.StateFinalize},
				{from: usecase.StateFinalize, to: usecase.StateDone},
			},
		},
		{
			name:          "model failure falls back to manual review",
			modelFeatures: domain.FeatureCount - 1,
			want: usecase.Decision{
				Verdict:   domain.VerdictManualReview,
				RiskScore: 0,
				Reasons:   []string{"ml_stage_skipped"},
				Degraded:  false,
			},
			wantTrace: []transition{
				{from: usecase.StateEnrich, to: usecase.StateFastRules},
				{from: usecase.StateFastRules, to: usecase.StateMLScore},
				{from: usecase.StateMLScore, to: usecase.StateFinalize, wantErr: domain.ErrModelInvalid},
				{from: usecase.StateFinalize, to: usecase.StateDone},
			},
		},
		{
			name:            "cache hit with empty verdict falls back to manual review",
			cacheSimilarity: 0.91,
			cacheHit:        true,
			want: usecase.Decision{
				Verdict:   domain.VerdictManualReview,
				RiskScore: 0.5,
				Reasons:   []string{"semantic_cache_hit:0.91"},
				Degraded:  false,
			},
			wantTrace: []transition{
				{from: usecase.StateEnrich, to: usecase.StateFastRules},
				{from: usecase.StateFastRules, to: usecase.StateMLScore},
				{from: usecase.StateMLScore, to: usecase.StateTiebreak},
				{from: usecase.StateTiebreak, to: usecase.StateFinalize},
				{from: usecase.StateFinalize, to: usecase.StateDone},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempt := tt.attempt
			if attempt.CardFingerprint == "" {
				attempt.CardFingerprint = "fp-test"
			}
			if attempt.AmountCents == 0 {
				attempt.AmountCents = 12_500
			}
			attempt.MerchantCategory = "retail"

			modelFeatures := tt.modelFeatures
			if modelFeatures == 0 {
				modelFeatures = domain.FeatureCount
			}
			model := &domain.TreeModel{
				Features: modelFeatures,
				Trees: []domain.Tree{{
					Nodes: []domain.Node{{Left: -1, Value: tt.modelValue}},
				}},
			}
			geo := &mockGeoResolver{}
			embedder := &mockEmbedder{}
			cache := &mockSemanticCache{}
			geo.On("CountryCode", mock.Anything, mock.AnythingOfType("string")).
				Return("BR", geoLookupError(tt.geoErr)).
				Times(tt.priorRuns + 1)

			tiebreakCalls := tt.priorRuns
			for _, hop := range tt.wantTrace {
				if hop.from == usecase.StateMLScore && hop.to == usecase.StateTiebreak {
					tiebreakCalls++
				}
			}
			if tiebreakCalls > 0 {
				embedder.On("Embed", mock.AnythingOfType("string")).
					Return([]float64{1}).
					Times(tiebreakCalls)
				cache.On("Lookup", mock.AnythingOfType("[]float64")).
					Return(tt.cacheVerdict, tt.cacheSimilarity, tt.cacheHit).
					Times(tiebreakCalls)
				cache.On("Store", mock.AnythingOfType("[]float64"), mock.Anything).
					Return().
					Times(tiebreakCalls)
			}
			t.Cleanup(func() {
				geo.AssertExpectations(t)
				embedder.AssertExpectations(t)
				cache.AssertExpectations(t)
			})

			pipeline := usecase.NewPipeline(
				model,
				geo,
				embedder,
				cache,
				slog.New(slog.NewTextHandler(io.Discard, nil)),
				1,
				time.Millisecond,
			)
			defer pipeline.Close()

			for i := 0; i < tt.priorRuns; i++ {
				if _, _, err := pipeline.Score(context.Background(), attempt); err != nil {
					t.Fatalf("Score() prior run %d error = %v", i+1, err)
				}
			}

			got, trace, err := pipeline.Score(context.Background(), attempt)
			if err != nil {
				t.Fatalf("Score() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Score() decision = %#v, want %#v", got, tt.want)
			}

			if len(trace) != len(tt.wantTrace) {
				t.Fatalf("Score() trace length = %d, want %d", len(trace), len(tt.wantTrace))
			}
			for i, hop := range trace {
				if hop.From != tt.wantTrace[i].from || hop.To != tt.wantTrace[i].to {
					t.Errorf("Score() trace[%d] = %q -> %q, want %q -> %q",
						i, hop.From, hop.To, tt.wantTrace[i].from, tt.wantTrace[i].to)
				}
				if !errors.Is(hop.Err, tt.wantTrace[i].wantErr) {
					t.Errorf("Score() trace[%d] error = %v, want error wrapping %v",
						i, hop.Err, tt.wantTrace[i].wantErr)
				}
			}
		})
	}
}

func TestPipelineScoreCanceledContext(t *testing.T) {
	geo := &mockGeoResolver{}
	embedder := &mockEmbedder{}
	cache := &mockSemanticCache{}
	t.Cleanup(func() {
		geo.AssertExpectations(t)
		embedder.AssertExpectations(t)
		cache.AssertExpectations(t)
	})

	pipeline := usecase.NewPipeline(
		&domain.TreeModel{
			Features: domain.FeatureCount,
			Trees: []domain.Tree{{
				Nodes: []domain.Node{{Left: -1}},
			}},
		},
		geo,
		embedder,
		cache,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		1,
		time.Millisecond,
	)
	defer pipeline.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, trace, err := pipeline.Score(ctx, domain.Attempt{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Score() error = %v, want context.Canceled", err)
	}
	if !reflect.DeepEqual(got, usecase.Decision{}) {
		t.Errorf("Score() decision = %#v, want zero value", got)
	}
	if len(trace) != 0 {
		t.Errorf("Score() trace = %#v, want empty trace", trace)
	}
	geo.AssertNotCalled(t, "CountryCode", mock.Anything, mock.Anything)
	embedder.AssertNotCalled(t, "Embed", mock.Anything)
	cache.AssertNotCalled(t, "Lookup", mock.Anything)
	cache.AssertNotCalled(t, "Store", mock.Anything, mock.Anything)
}

func TestPipelineScoreCanceledDuringEnrichment(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	geo := &mockGeoResolver{}
	embedder := &mockEmbedder{}
	cache := &mockSemanticCache{}
	geo.On("CountryCode", mock.Anything, mock.AnythingOfType("string")).
		Run(func(mock.Arguments) { cancel() }).
		Return("BR", nil).
		Once()
	t.Cleanup(func() {
		geo.AssertExpectations(t)
		embedder.AssertExpectations(t)
		cache.AssertExpectations(t)
	})

	pipeline := usecase.NewPipeline(
		&domain.TreeModel{
			Features: domain.FeatureCount,
			Trees: []domain.Tree{{
				Nodes: []domain.Node{{Left: -1}},
			}},
		},
		geo,
		embedder,
		cache,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		1,
		time.Millisecond,
	)
	defer pipeline.Close()

	got, trace, err := pipeline.Score(ctx, domain.Attempt{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Score() error = %v, want context.Canceled", err)
	}
	if !reflect.DeepEqual(got, usecase.Decision{}) {
		t.Errorf("Score() decision = %#v, want zero value", got)
	}
	if len(trace) != 1 {
		t.Fatalf("Score() trace length = %d, want 1", len(trace))
	}
	if trace[0].From != usecase.StateEnrich || trace[0].To != usecase.StateFastRules {
		t.Errorf("Score() trace[0] = %q -> %q, want %q -> %q",
			trace[0].From, trace[0].To, usecase.StateEnrich, usecase.StateFastRules)
	}
	if trace[0].Err != nil {
		t.Errorf("Score() trace[0] error = %v, want nil", trace[0].Err)
	}
	embedder.AssertNotCalled(t, "Embed", mock.Anything)
	cache.AssertNotCalled(t, "Lookup", mock.Anything)
	cache.AssertNotCalled(t, "Store", mock.Anything, mock.Anything)
}

func geoLookupError(fail bool) error {
	if fail {
		return errors.New("geo lookup failed")
	}
	return nil
}
