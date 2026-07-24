package coverage

import "context"

// RateGate limita peticiones concurrentes/por segundo hacia TowerCoverage
// entre varias instancias (p. ej. vía cola de tokens en RabbitMQ).
type RateGate interface {
	Acquire(ctx context.Context) error
}

// acquireTowerSlot espera un cupo global antes de llamar a TowerCoverage.
// Si gate es nil, no limita (modo desarrollo / sin RabbitMQ).
func acquireTowerSlot(ctx context.Context, gate RateGate) error {
	if gate == nil {
		return nil
	}
	return gate.Acquire(ctx)
}
