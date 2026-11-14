package domain_test

import (
	"reflect"
	"testing"

	"github.com/huncoding/payment-processor-course/internal/fraude/domain"
)

func TestApplyFastRules(t *testing.T) {
	tests := []struct {
		name    string
		attempt domain.Attempt
		want    domain.RuleResult
	}{
		{
			name: "ordinary attempt is approved",
			attempt: domain.Attempt{
				AmountCents:     12500,
				VelocityLastMin: 3,
			},
			want: domain.RuleResult{
				Verdict: domain.VerdictApprove,
				Reason:  "fast_rules_passed",
				Final:   false,
			},
		},
		{
			name: "blocklisted card is denied",
			attempt: domain.Attempt{
				CardFingerprint: "fp_stolen_card_001",
			},
			want: domain.RuleResult{
				Verdict: domain.VerdictDeny,
				Reason:  "amount_above_hard_limit",
				Final:   true,
			},
		},
		{
			name: "amount immediately below hard limit is approved",
			attempt: domain.Attempt{
				AmountCents: int64(domain.MaxAmountCents - 1),
			},
			want: domain.RuleResult{
				Verdict: domain.VerdictApprove,
				Reason:  "fast_rules_passed",
			},
		},
		{
			name: "amount at hard limit is approved",
			attempt: domain.Attempt{
				AmountCents: int64(domain.MaxAmountCents),
			},
			want: domain.RuleResult{
				Verdict: domain.VerdictApprove,
				Reason:  "fast_rules_passed",
			},
		},
		{
			name: "amount immediately above hard limit needs review",
			attempt: domain.Attempt{
				AmountCents: int64(domain.MaxAmountCents + 1),
			},
			want: domain.RuleResult{
				Verdict: domain.VerdictManualReview,
				Reason:  "amount_above_hard_limit",
				Final:   true,
			},
		},
		{
			name: "velocity immediately below limit is approved",
			attempt: domain.Attempt{
				VelocityLastMin: domain.VelocityPerMinLimit - 1,
			},
			want: domain.RuleResult{
				Verdict: domain.VerdictApprove,
				Reason:  "fast_rules_passed",
			},
		},
		{
			name: "velocity at limit is approved",
			attempt: domain.Attempt{
				VelocityLastMin: domain.VelocityPerMinLimit,
			},
			want: domain.RuleResult{
				Verdict: domain.VerdictApprove,
				Reason:  "fast_rules_passed",
			},
		},
		{
			name: "velocity immediately above limit is denied",
			attempt: domain.Attempt{
				VelocityLastMin: domain.VelocityPerMinLimit + 1,
			},
			want: domain.RuleResult{
				Verdict: domain.VerdictDeny,
				Reason:  "velocity_limit_exceeded",
				Final:   true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := domain.ApplyFastRules(tt.attempt); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ApplyFastRules() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
