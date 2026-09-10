package semantic

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestEmbeddingSpaceIDIsStableAndSecretFree(t *testing.T) {
	a, err := NewEmbeddingContract("openai-compatible", "model", 1024, "https://user:password@example.test/v1?api_key=secret#fragment")
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewEmbeddingContract("openai-compatible", "model", 1024, "https://example.test/v1?api_key=other")
	if err != nil {
		t.Fatal(err)
	}
	if a.SpaceID != b.SpaceID {
		t.Fatalf("credential-bearing endpoint details changed space: %q != %q", a.SpaceID, b.SpaceID)
	}
	if strings.Contains(a.SpaceID, "secret") || strings.Contains(a.Endpoint, "password") || strings.Contains(a.Endpoint, "api_key") {
		t.Fatalf("secret leaked into contract identity: %+v", a)
	}
	for _, changed := range []struct {
		provider, model, endpoint string
		dimensions                int
	}{
		{"openai", "model", "https://example.test/v1", 1024},
		{"openai-compatible", "other-model", "https://example.test/v1", 1024},
		{"openai-compatible", "model", "https://other.test/v1", 1024},
		{"openai-compatible", "model", "https://example.test/v1", 1536},
	} {
		other, contractErr := NewEmbeddingContract(changed.provider, changed.model, changed.dimensions, changed.endpoint)
		if contractErr != nil {
			t.Fatal(contractErr)
		}
		if other.SpaceID == a.SpaceID {
			t.Fatalf("contract change did not change space: %+v", changed)
		}
	}
}

func TestValidateVectorRejectsMismatchEmptyAndNonFinite(t *testing.T) {
	for name, vector := range map[string][]float32{
		"empty": {},
		"short": {1},
		"long":  {1, 2, 3},
		"nan":   {1, 0},
	} {
		t.Run(name, func(t *testing.T) {
			if name == "nan" {
				vector[1] = float32(math.NaN())
			}
			if err := ValidateVector(vector, 2, "test"); err == nil {
				t.Fatal("invalid vector was accepted")
			} else if name != "nan" && !errors.Is(err, ErrEmbeddingDimensionMismatch) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
