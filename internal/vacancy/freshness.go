package vacancy

import "time"

const (
	// LegacyFingerprintVersion is the P1.1/P1.4 pre-professional-roles
	// fingerprint contract. It remains available only to recognize a stored
	// legacy baseline during the one-time upgrade observation.
	LegacyFingerprintVersion = 1

	// FingerprintVersion identifies the current canonical normalization used for
	// vacancy source fingerprints. A version change is a schema/semantics
	// change, not a provider update.
	FingerprintVersion = 2
)

// Freshness is provider-observation state. A zero timestamp or empty
// fingerprint is unknown; it is never inferred from the legacy vacancy
// timestamps.
type Freshness struct {
	VacancyID                 int
	FirstSeenAt               time.Time
	LastSeenAt                time.Time
	SourceFingerprint         string
	PreviousSourceFingerprint string
	MaterialFingerprint       string
	MaterialChangedAt         time.Time
	FingerprintVersion        int
}

// ObservationResult describes the local effect of one successful provider
// observation. It deliberately says nothing about user review or HH write
// eligibility.
type ObservationResult struct {
	Created         bool
	Updated         bool
	SourceChanged   bool
	MaterialChanged bool
}

// ObservationFingerprintChanges compares an observation with stored
// freshness without treating different algorithm contracts as provider
// changes. For the v1→v2 upgrade, legacyPair must be computed from the same
// current provider value under the v1 projection.
func ObservationFingerprintChanges(previous Freshness, current, legacy FingerprintPair, legacySupported bool) (sourceChanged, materialChanged bool) {
	switch previous.FingerprintVersion {
	case FingerprintVersion:
		return previous.SourceFingerprint != current.SourceFingerprint, previous.MaterialFingerprint != current.MaterialFingerprint
	case LegacyFingerprintVersion:
		if legacySupported {
			return previous.SourceFingerprint != legacy.SourceFingerprint, previous.MaterialFingerprint != legacy.MaterialFingerprint
		}
	}
	return false, false
}
