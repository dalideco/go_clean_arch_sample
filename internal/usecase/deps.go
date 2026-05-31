package usecase

// Deps is the wiring-layer bundle of every adapter bundle the app uses —
// today that's the repositories and the queue producers. It exists so
// `api.New` / `router.Register` / `RegisterX` don't gain a new parameter
// every time a new adapter type lands (cache, search, object store, ...).
// Add a field per new bundle.
//
// Like Repositories and Producers, Deps is a **wiring-time aggregate**:
// the composition root assembles it, the wiring layer destructures it.
// Use cases never receive Deps — they take the specific interfaces they
// actually need as explicit constructor params (see AGENTS.md).
type Deps struct {
	Repos     Repositories
	Producers Producers
}
