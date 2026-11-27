package realtime

import (
	"context"
	"sync"
)

// LocalLocker は Pod の中だけで効くキー単位の排他制御である。
// Pod を増やす時は Postgres の advisory lock などの Pod をまたぐ排他に差し替える。
type LocalLocker struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewLocalLocker は新しい LocalLocker を生成する。
func NewLocalLocker() *LocalLocker {
	return &LocalLocker{locks: map[string]*sync.Mutex{}}
}

// Lock は key の排他を取る。
func (l *LocalLocker) Lock(_ context.Context, key string) (func(), error) {
	l.mu.Lock()
	m, ok := l.locks[key]
	if !ok {
		m = &sync.Mutex{}
		l.locks[key] = m
	}
	l.mu.Unlock()
	m.Lock()
	return m.Unlock, nil
}
