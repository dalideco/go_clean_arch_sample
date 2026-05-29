package usecase

// Repositories bundles every repository the app wires up. Grows one
// interface-typed field per entity. Implemented by an adapter package
// (postgres today) — the bundle itself names no infrastructure type, so
// the wiring layer can depend on it without importing the ORM.
type Repositories struct {
	Users UserRepository
}
