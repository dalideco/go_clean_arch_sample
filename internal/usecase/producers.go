package usecase

// Producers bundles every queue producer (event enqueuer) the app wires up.
// One interface-typed field per producer; the bundle itself names no
// transport (asynq) — wiring depends on it without importing the queue lib.
// Same shape as Repositories: a wiring-time aggregate, not a god-object to
// inject into use cases. Use cases take the specific producer interface(s)
// they need as named constructor params.
type Producers struct {
	WelcomeEmail WelcomeEmailEnqueuer
}
