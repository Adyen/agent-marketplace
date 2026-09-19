package artifact

import "fmt"

// ReconciliationMode is the closed set of ownership strategies understood by
// the artifact materializer.
type ReconciliationMode uint8

const (
	// ReconciliationReplace publishes the complete artifact without reading a
	// destination. It is the default for ordinary files.
	ReconciliationReplace ReconciliationMode = iota + 1
	// ReconciliationMarker permits a consumer-owned aggregate merge whose
	// entries carry the declared marker metadata.
	ReconciliationMarker
	// ReconciliationExternal reserves an aggregate for an owning consumer
	// adapter. Generic materialization must refuse it.
	ReconciliationExternal
)

// ReconciliationPolicy describes how one artifact may reconcile its
// destination. The zero value means no typed policy was supplied and invokes
// legacy compatibility resolution; that resolution defaults to replacement.
type ReconciliationPolicy struct {
	Mode        ReconciliationMode
	MarkerField string
	MarkerValue string
}

// Validate checks the closed policy combinations. Fragment and marker
// metadata are supplied separately because fragments are a property of the
// compilation result, not ownership.
func (p ReconciliationPolicy) Validate(fragment bool) error {
	switch p.Mode {
	case ReconciliationReplace:
		if p.MarkerField != "" || p.MarkerValue != "" {
			return fmt.Errorf("replacement reconciliation cannot declare marker metadata")
		}
	case ReconciliationMarker:
		if !fragment {
			return fmt.Errorf("marker reconciliation requires a fragment")
		}
		if p.MarkerField == "" || p.MarkerValue == "" {
			return fmt.Errorf("marker reconciliation requires marker field and value")
		}
	case ReconciliationExternal:
		if !fragment {
			return fmt.Errorf("external reconciliation requires a fragment")
		}
		if p.MarkerField != "" || p.MarkerValue != "" {
			return fmt.Errorf("external reconciliation cannot declare marker metadata")
		}
	default:
		return fmt.Errorf("unknown reconciliation mode %d", p.Mode)
	}
	if (p.MarkerField == "") != (p.MarkerValue == "") {
		return fmt.Errorf("marker field and value must be provided together")
	}
	return nil
}

// IsExternal reports whether the owning consumer adapter must reconcile the
// artifact instead of the generic materializer.
func (p ReconciliationPolicy) IsExternal() bool { return p.Mode == ReconciliationExternal }

// Policy returns the validated policy, translating legacy fields for callers
// that still construct Artifact literals. An explicit non-zero policy is
// authoritative: compatibility fields are ignored so an old producer cannot
// override a typed ownership decision (or turn external reconciliation into a
// marker conflict).
func (a Artifact) Policy() (ReconciliationPolicy, error) {
	if a.Reconciliation != (ReconciliationPolicy{}) {
		if err := a.Reconciliation.Validate(a.Fragment || a.MergeWithExisting); err != nil {
			return ReconciliationPolicy{}, err
		}
		return a.Reconciliation, nil
	}

	legacy := ReconciliationPolicy{Mode: ReconciliationReplace}
	if a.ExternalReconciliationRequired {
		legacy.Mode = ReconciliationExternal
	} else if a.MergeWithExisting {
		legacy.Mode = ReconciliationMarker
		legacy.MarkerField, legacy.MarkerValue = a.ManagedByField, a.ManagedByValue
	} else if a.ManagedByField != "" || a.ManagedByValue != "" {
		legacy.MarkerField, legacy.MarkerValue = a.ManagedByField, a.ManagedByValue
	}
	if err := legacy.Validate(a.Fragment || a.MergeWithExisting); err != nil {
		return ReconciliationPolicy{}, err
	}
	return legacy, nil
}

// Validate checks the path and closed reconciliation policy before any
// materializer filesystem operation.
func (a *Artifact) ValidatePolicy() error {
	if err := a.Validate(); err != nil {
		return err
	}
	if err := a.PolicyValidation(); err != nil {
		return err
	}
	return nil
}

// PolicyValidation validates the policy with the artifact's fragment or
// already-merged aggregate shape.
func (a Artifact) PolicyValidation() error {
	p, err := a.Policy()
	if err != nil {
		return err
	}
	return p.Validate(a.Fragment || a.MergeWithExisting)
}
