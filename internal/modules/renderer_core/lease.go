package renderercore

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/mikey-austin/media_utopia/pkg/mu"
)

// LeaseManager tracks session leases.
type LeaseManager struct {
	mu        sync.Mutex
	session   *mu.SessionLease
	owner     string
	expiry    time.Time
	token     string
	sessionID string
	idPrefix  string
}

// Acquire grants a lease if free.
func (l *LeaseManager) Acquire(owner string, ttl time.Duration) (mu.SessionLease, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.activeLocked(time.Now()) {
		return mu.SessionLease{}, errors.New("lease already held")
	}
	return l.newLeaseLocked(owner, ttl), nil
}

// Renew extends the lease if token matches.
func (l *LeaseManager) Renew(sessionID string, token string, ttl time.Duration) (mu.SessionLease, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.activeLocked(time.Now()) {
		return mu.SessionLease{}, errors.New("lease not active")
	}
	if l.sessionID != sessionID || l.token != token {
		return mu.SessionLease{}, errors.New("lease mismatch")
	}
	l.expiry = time.Now().Add(ttl)
	return l.currentLocked(), nil
}

// Release releases the lease if token matches.
func (l *LeaseManager) Release(sessionID string, token string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.activeLocked(time.Now()) {
		return errors.New("lease not active")
	}
	if l.sessionID != sessionID || l.token != token {
		return errors.New("lease mismatch")
	}
	l.session = nil
	l.token = ""
	l.sessionID = ""
	l.owner = ""
	l.expiry = time.Time{}
	return nil
}

// Current returns the active lease if present.
func (l *LeaseManager) Current() (*mu.SessionState, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.activeLocked(time.Now()) {
		return nil, false
	}
	return &mu.SessionState{ID: l.sessionID, Owner: l.owner, LeaseExpireAt: l.expiry.Unix()}, true
}

// Require checks a lease token.
func (l *LeaseManager) Require(sessionID string, token string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.activeLocked(time.Now()) {
		return errors.New("lease required")
	}
	if l.sessionID != sessionID || l.token != token {
		return errors.New("lease mismatch")
	}
	return nil
}

func (l *LeaseManager) newLeaseLocked(owner string, ttl time.Duration) mu.SessionLease {
	prefix := l.idPrefix
	if prefix == "" {
		prefix = "renderer"
	}
	l.sessionID = "mu:session:" + prefix + ":" + randToken()
	l.token = randToken()
	l.owner = owner
	l.expiry = time.Now().Add(ttl)
	lease := mu.SessionLease{ID: l.sessionID, Token: l.token, Owner: owner, LeaseExpiresAt: l.expiry.Unix()}
	l.session = &lease
	return lease
}

func (l *LeaseManager) currentLocked() mu.SessionLease {
	return mu.SessionLease{ID: l.sessionID, Token: l.token, Owner: l.owner, LeaseExpiresAt: l.expiry.Unix()}
}

func (l *LeaseManager) activeLocked(now time.Time) bool {
	return l.session != nil && now.Before(l.expiry)
}

func randToken() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "token"
	}
	return hex.EncodeToString(buf[:])
}
