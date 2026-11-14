package domain_test

import (
	"errors"
	"math"
	"testing"

	"github.com/huncoding/payment-processor-course/internal/fraude/domain"
)

func TestParseTreeModel(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr bool
	}{
		{
			name: "valid model with split and leaf nodes",
			data: `{"version":"1","features":2,"baseline":0.1,"trees":[{"nodes":[{"feature":1,"threshold":0.5,"left":1,"right":2},{"left":-1,"value":-0.2},{"left":-1,"value":0.3}]}]}`,
		},
		{
			name: "child indexes at lower valid boundary",
			data: `{"features":2,"trees":[{"nodes":[{"left":-1},{"left":-1},{"feature":0,"threshold":0.5,"left":0,"right":1}]}]}`,
		},
		{
			name: "right child at lower valid boundary",
			data: `{"features":2,"trees":[{"nodes":[{"left":-1},{"left":-1},{"feature":0,"threshold":0.5,"left":1,"right":0}]}]}`,
		},
		{
			name:    "malformed JSON",
			data:    `{"features":`,
			wantErr: true,
		},
		{
			name:    "zero features",
			data:    `{"features":0,"trees":[{"nodes":[{"left":-1}]}]}`,
			wantErr: true,
		},
		{
			name:    "negative features",
			data:    `{"features":-1,"trees":[{"nodes":[{"left":-1}]}]}`,
			wantErr: true,
		},
		{
			name:    "empty trees",
			data:    `{"features":2,"trees":[]}`,
			wantErr: true,
		},
		{
			name:    "tree with no nodes",
			data:    `{"features":2,"trees":[{"nodes":[]}]}`,
			wantErr: true,
		},
		{
			name:    "feature below valid range",
			data:    `{"features":2,"trees":[{"nodes":[{"feature":-1,"left":1,"right":2},{"left":-1},{"left":-1}]}]}`,
			wantErr: true,
		},
		{
			name:    "feature at upper invalid boundary",
			data:    `{"features":2,"trees":[{"nodes":[{"feature":2,"left":1,"right":2},{"left":-1},{"left":-1}]}]}`,
			wantErr: true,
		},
		{
			name:    "left child below valid range",
			data:    `{"features":2,"trees":[{"nodes":[{"feature":0,"left":-2,"right":2},{"left":-1},{"left":-1}]}]}`,
			wantErr: true,
		},
		{
			name:    "left child at upper invalid boundary",
			data:    `{"features":2,"trees":[{"nodes":[{"feature":0,"left":3,"right":2},{"left":-1},{"left":-1}]}]}`,
			wantErr: true,
		},
		{
			name:    "right child below valid range",
			data:    `{"features":2,"trees":[{"nodes":[{"feature":0,"left":1,"right":-1},{"left":-1},{"left":-1}]}]}`,
			wantErr: true,
		},
		{
			name:    "right child at upper invalid boundary",
			data:    `{"features":2,"trees":[{"nodes":[{"feature":0,"left":1,"right":3},{"left":-1},{"left":-1}]}]}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.ParseTreeModel([]byte(tt.data))
			if tt.wantErr {
				if !errors.Is(err, domain.ErrModelInvalid) {
					t.Fatalf("ParseTreeModel() error = %v, want ErrModelInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTreeModel() error = %v", err)
			}
			if got == nil || got.Features != 2 || len(got.Trees) != 1 {
				t.Errorf("ParseTreeModel() = %#v, want a model with 2 features and one tree", got)
			}
		})
	}
}

func TestTreeModelScore(t *testing.T) {
	model := &domain.TreeModel{
		Features: 1,
		Trees: []domain.Tree{{
			Nodes: []domain.Node{
				{Feature: 0, Threshold: 0, Left: 1, Right: 4},
				{Feature: 0, Threshold: -1, Left: 2, Right: 3},
				{Left: -1, Value: -40},
				{Left: -1, Value: -1},
				{Feature: 0, Threshold: 1, Left: 5, Right: 6},
				{Left: -1, Value: 0},
				{Feature: 0, Threshold: 2, Left: 7, Right: 8},
				{Left: -1, Value: 1},
				{Left: -1, Value: 40},
			},
		}},
	}

	tests := []struct {
		name    string
		feature float64
		want    float64
	}{
		{name: "immediately below negative split", feature: math.Nextafter(-1, math.Inf(-1)), want: 0},
		{name: "at negative split", feature: -1, want: 0},
		{name: "immediately above negative split", feature: math.Nextafter(-1, math.Inf(1)), want: 0.2689414213699951},
		{name: "immediately below root split", feature: math.Nextafter(0, math.Inf(-1)), want: 0.2689414213699951},
		{name: "at root split", feature: 0, want: 0.2689414213699951},
		{name: "immediately above root split", feature: math.Nextafter(0, math.Inf(1)), want: 0.5},
		{name: "immediately below positive split", feature: math.Nextafter(1, math.Inf(-1)), want: 0.5},
		{name: "at positive split", feature: 1, want: 0.5},
		{name: "immediately above positive split", feature: math.Nextafter(1, math.Inf(1)), want: 0.7310585786300049},
		{name: "immediately below upper split", feature: math.Nextafter(2, math.Inf(-1)), want: 0.7310585786300049},
		{name: "at upper split", feature: 2, want: 0.7310585786300049},
		{name: "immediately above upper split", feature: math.Nextafter(2, math.Inf(1)), want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := model.Score([]float64{tt.feature})
			if err != nil {
				t.Fatalf("Score() error = %v", err)
			}
			if got < tt.want-0.000001 || got > tt.want+0.000001 {
				t.Errorf("Score(%v) = %.12f, want %.12f", tt.feature, got, tt.want)
			}
		})
	}
}

func TestTreeModelScoreRejectsFeatureCountMismatch(t *testing.T) {
	model := &domain.TreeModel{Features: 2}

	_, err := model.Score([]float64{1})
	if !errors.Is(err, domain.ErrModelInvalid) {
		t.Fatalf("Score() error = %v, want ErrModelInvalid", err)
	}
	if err.Error() != "invalid tree model: expected 2 features, received 1" {
		t.Errorf("Score() error = %q, want expected and received feature counts", err)
	}
}

func TestTreeModelScoreExpNegCutoff(t *testing.T) {
	modelForTotal := func(total float64) *domain.TreeModel {
		return &domain.TreeModel{
			Features: 1,
			Baseline: total,
			Trees: []domain.Tree{{
				Nodes: []domain.Node{{Left: -1}},
			}},
		}
	}

	tests := []struct {
		name       string
		total      float64
		wantZero   bool
		wantOne    bool
		wantInside bool
	}{
		{name: "positive immediately below cutoff", total: math.Nextafter(30, math.Inf(-1)), wantInside: true},
		{name: "positive at cutoff", total: 30, wantInside: true},
		{name: "positive immediately above cutoff", total: math.Nextafter(30, math.Inf(1)), wantOne: true},
		{name: "negative immediately below cutoff", total: math.Nextafter(-30, math.Inf(-1)), wantZero: true},
		{name: "negative at cutoff", total: -30, wantInside: true},
		{name: "negative immediately above cutoff", total: math.Nextafter(-30, math.Inf(1)), wantInside: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := modelForTotal(tt.total).Score([]float64{0})
			if err != nil {
				t.Fatalf("Score() error = %v", err)
			}
			switch {
			case tt.wantZero && got != 0:
				t.Errorf("Score() = %v, want exact zero after cutoff", got)
			case tt.wantOne && got != 1:
				t.Errorf("Score() = %v, want exact one after cutoff", got)
			case tt.wantInside && (got <= 0 || got >= 1):
				t.Errorf("Score() = %v, want a value strictly between zero and one", got)
			}
		})
	}
}
