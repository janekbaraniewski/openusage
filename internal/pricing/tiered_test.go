package pricing

import "testing"

func TestApplyTier_Boundaries(t *testing.T) {
	base := Price{
		InputCostPerMillion:  3.0,
		OutputCostPerMillion: 15.0,
		Tiers: TierOverrides{
			Above128k: &TierRates{
				InputCostPerMillion:  ptrFloat64(4.0),
				OutputCostPerMillion: ptrFloat64(20.0),
			},
			Above200k: &TierRates{
				InputCostPerMillion:  ptrFloat64(6.0),
				OutputCostPerMillion: ptrFloat64(30.0),
			},
			Above256k: &TierRates{
				InputCostPerMillion: ptrFloat64(8.0),
			},
			Above272k: &TierRates{
				InputCostPerMillion:  ptrFloat64(10.0),
				OutputCostPerMillion: ptrFloat64(50.0),
			},
		},
	}

	cases := []struct {
		name       string
		ctx        int
		wantInput  float64
		wantOutput float64
	}{
		{"below-128k", 0, 3.0, 15.0},
		{"at-127k", 127_000, 3.0, 15.0},
		{"at-128k-boundary", 128_000, 3.0, 15.0},
		{"just-above-128k", 128_001, 4.0, 20.0},
		{"middle-128k-tier", 150_000, 4.0, 20.0},
		{"at-199k", 199_000, 4.0, 20.0},
		{"at-200k-boundary", 200_000, 4.0, 20.0},
		{"just-above-200k", 200_001, 6.0, 30.0},
		{"at-255k", 255_000, 6.0, 30.0},
		{"at-256k-boundary", 256_000, 6.0, 30.0},
		{"just-above-256k", 256_001, 8.0, 30.0},
		{"at-272k-boundary", 272_000, 8.0, 30.0},
		{"just-above-272k", 272_001, 10.0, 50.0},
		{"way-above", 1_000_000, 10.0, 50.0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ApplyTier(base, c.ctx)
			if got.InputCostPerMillion != c.wantInput {
				t.Errorf("input @ %d = %v, want %v", c.ctx, got.InputCostPerMillion, c.wantInput)
			}
			if got.OutputCostPerMillion != c.wantOutput {
				t.Errorf("output @ %d = %v, want %v", c.ctx, got.OutputCostPerMillion, c.wantOutput)
			}
		})
	}
}

func TestApplyTier_NoOverrides(t *testing.T) {
	base := Price{InputCostPerMillion: 1, OutputCostPerMillion: 2}
	got := ApplyTier(base, 999_999)
	if got.InputCostPerMillion != 1 || got.OutputCostPerMillion != 2 {
		t.Errorf("expected base rates; got %+v", got)
	}
}

func TestResolveTier(t *testing.T) {
	ladder := []TieredPrice{
		{AppliesAbove: 0, Price: Price{InputCostPerMillion: 1}},
		{AppliesAbove: 200_000, Price: Price{InputCostPerMillion: 2}},
		{AppliesAbove: 1_000_000, Price: Price{InputCostPerMillion: 3}},
	}
	if got := ResolveTier(ladder, 50_000).InputCostPerMillion; got != 1 {
		t.Errorf("low tier = %v, want 1", got)
	}
	if got := ResolveTier(ladder, 200_000).InputCostPerMillion; got != 1 {
		t.Errorf("boundary tier (exclusive) = %v, want 1", got)
	}
	if got := ResolveTier(ladder, 200_001).InputCostPerMillion; got != 2 {
		t.Errorf("mid tier = %v, want 2", got)
	}
	if got := ResolveTier(ladder, 2_000_000).InputCostPerMillion; got != 3 {
		t.Errorf("top tier = %v, want 3", got)
	}
	if got := ResolveTier(nil, 0); got.InputCostPerMillion != 0 {
		t.Errorf("empty ladder should be zero price")
	}
}
