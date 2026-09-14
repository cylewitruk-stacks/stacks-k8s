package telemetry

const (
	// SensitiveTerms is the shared credential vocabulary for structured keys, labels and diagnostics.
	SensitiveTerms = `password|private[_-]?key|authorization|auth[_-]?token|seed`
	// SensitiveNamePattern matches sensitive structured keys and metric label names.
	SensitiveNamePattern = `(?i).*(` + SensitiveTerms + `).*`
	// SensitiveAssignmentPattern removes whole diagnostic lines containing credential assignments.
	SensitiveAssignmentPattern = `(?is).*(` + SensitiveTerms + `)[" ]*[:=].*`
	// SourceActorNative identifies externally scraped actor measurements.
	SourceActorNative = "actor-native"
	// SourceCollectorInternal identifies the collection infrastructure's own measurements.
	SourceCollectorInternal = "collector-internal"
)
