package domain_test

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/huncoding/payment-processor-course/internal/fraude/domain"
)

func TestFeatureVector(t *testing.T) {
	tests := []struct {
		name             string
		hour             int
		amountCents      int64
		velocityLastMin  int
		countryCode      string
		merchantCategory string
		want             []float64
	}{
		{
			name:             "common enriched attempt",
			hour:             14,
			amountCents:      12500,
			velocityLastMin:  3,
			countryCode:      "US",
			merchantCategory: "gambling",
			want:             []float64{12500, 3, 0, 1, 1},
		},
		{
			name: "hour before night threshold",
			hour: 5,
			want: []float64{0, 0, 1, 0, 0},
		},
		{
			name: "hour at night threshold",
			hour: 6,
			want: []float64{0, 0, 0, 0, 0},
		},
		{
			name: "hour after night threshold",
			hour: 7,
			want: []float64{0, 0, 0, 0, 0},
		},
		{
			name:             "home country and high risk merchant",
			hour:             12,
			countryCode:      "BR",
			merchantCategory: "crypto",
			want:             []float64{0, 0, 0, 0, 1},
		},
		{
			name:             "unknown merchant category",
			hour:             12,
			countryCode:      "BR",
			merchantCategory: "grocery",
			want:             []float64{0, 0, 0, 0, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempt := domain.Attempt{
				AmountCents:      tt.amountCents,
				VelocityLastMin:  tt.velocityLastMin,
				CountryCode:      tt.countryCode,
				MerchantCategory: tt.merchantCategory,
				OccurredAt:       time.Date(2026, time.January, 2, tt.hour, 0, 0, 0, time.UTC),
			}

			if got := domain.FeatureVector(attempt); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FeatureVector() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVerdictFromScore(t *testing.T) {
	tests := []struct {
		name  string
		score float64
		want  domain.Verdict
	}{
		{name: "common approval", score: 0.2, want: domain.VerdictApprove},
		{name: "immediately below approval threshold", score: math.Nextafter(domain.ApproveBelow, math.Inf(-1)), want: domain.VerdictApprove},
		{name: "at approval threshold", score: domain.ApproveBelow, want: domain.VerdictManualReview},
		{name: "immediately above approval threshold", score: math.Nextafter(domain.ApproveBelow, math.Inf(1)), want: domain.VerdictManualReview},
		{name: "immediately below denial threshold", score: math.Nextafter(domain.DenyAbove, math.Inf(-1)), want: domain.VerdictManualReview},
		{name: "at denial threshold", score: domain.DenyAbove, want: domain.VerdictManualReview},
		{name: "immediately above denial threshold", score: math.Nextafter(domain.DenyAbove, math.Inf(1)), want: domain.VerdictDeny},
		{name: "common denial", score: 0.9, want: domain.VerdictDeny},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := domain.VerdictFromScore(tt.score); got != tt.want {
				t.Errorf("VerdictFromScore(%v) = %q, want %q", tt.score, got, tt.want)
			}
		})
	}
}
