package codex

import "testing"

func TestProvenanceModelRequiresModelType(t *testing.T) {
	tests := []struct {
		name       string
		provenance *instructionsProvenance
		want       string
	}{
		{
			name: "model provenance",
			provenance: &instructionsProvenance{
				Type:  "model",
				Model: "gpt-5.6-luna",
			},
			want: "gpt-5.6-luna",
		},
		{
			name: "non-model provenance",
			provenance: &instructionsProvenance{
				Type:  "user",
				Model: "gpt-5.6-luna",
			},
		},
		{
			name: "missing provenance type",
			provenance: &instructionsProvenance{
				Model: "gpt-5.6-luna",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := &sessionMetaPayload{
				BaseInstructions: &baseInstructions{Provenance: tt.provenance},
			}
			if got := meta.ProvenanceModel(); got != tt.want {
				t.Fatalf("ProvenanceModel() = %q, want %q", got, tt.want)
			}
		})
	}
}
