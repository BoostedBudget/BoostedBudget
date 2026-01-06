package river

import (
	"github.com/riverqueue/river"
)

// WorkerRegistry helps manage River worker registration with dependency injection.
// It provides a type-safe way to register workers while keeping the dependency
// injection logic in one place.
type WorkerRegistry struct {
	workers *river.Workers
}

// NewWorkerRegistry creates a new worker registry.
func NewWorkerRegistry() *WorkerRegistry {
	return &WorkerRegistry{
		workers: river.NewWorkers(),
	}
}

// Workers returns the underlying River workers for client creation.
func (r *WorkerRegistry) Workers() *river.Workers {
	return r.workers
}

// AddWorker is a generic function to register a worker with the registry.
// The worker must implement river.Worker[T] for some job args type T.
//
// Example:
//
//	registry := river.NewWorkerRegistry()
//	river.AddWorker(registry.Workers(), &PlaidSyncWorker{syncService: svc})
func AddWorker[T river.JobArgs](workers *river.Workers, worker river.Worker[T]) {
	river.AddWorker(workers, worker)
}

// AddWorkerSafely is like AddWorker but returns an error instead of panicking
// if the worker is already registered or invalid.
func AddWorkerSafely[T river.JobArgs](workers *river.Workers, worker river.Worker[T]) error {
	return river.AddWorkerSafely(workers, worker)
}
