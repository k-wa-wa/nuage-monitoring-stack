package push

import (
	"sync"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Store keeps push subscriptions in memory, keyed by endpoint.
//
// This is a verification-only implementation: subscriptions are lost on
// pod restart. Swap for a persistent store (ConfigMap/PVC/DB) once the
// end-to-end flow is confirmed working.
type Store struct {
	mu   sync.RWMutex
	subs map[string]webpush.Subscription
}

func NewStore() *Store {
	return &Store{
		subs: make(map[string]webpush.Subscription),
	}
}

func (s *Store) Add(sub webpush.Subscription) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subs[sub.Endpoint] = sub
}

func (s *Store) Remove(endpoint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subs, endpoint)
}

func (s *Store) List() []webpush.Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]webpush.Subscription, 0, len(s.subs))
	for _, sub := range s.subs {
		out = append(out, sub)
	}
	return out
}
