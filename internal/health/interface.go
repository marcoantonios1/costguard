package health

// ProviderHealth is the read interface a router uses to query health signals.
// *Tracker satisfies this interface.
type ProviderHealth interface {
	Snapshot(provider string) Snapshot
	Snapshots() []Snapshot
}

var _ ProviderHealth = (*Tracker)(nil)
