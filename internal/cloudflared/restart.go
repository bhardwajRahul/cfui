package cloudflared

import (
	"context"
	"time"

	"github.com/cloudflare/backoff"
)

const (
	restartBackoffBaseDelay  = 5 * time.Second
	restartBackoffMaxDelay   = 60 * time.Second
	restartBackoffResetAfter = 5 * time.Minute
	maxRestartAttempts       = 10
)

// NewBackoff builds an exponential backoff helper; exposed for tests and for
// future supervisors that want the same schedule.
func NewBackoff(interval, max, decay time.Duration, noJitter bool) *backoff.Backoff {
	var b *backoff.Backoff
	if noJitter {
		b = backoff.NewWithoutJitter(max, interval)
	} else {
		b = backoff.New(max, interval)
	}
	if decay > 0 {
		b.SetDecay(decay)
	}
	return b
}

// NewRestartBackoff returns the standard tunnel restart schedule
// (5s, 10s, 20s, 40s, 60s cap, reset after 5 minutes of stability).
func NewRestartBackoff() *backoff.Backoff {
	return NewBackoff(restartBackoffBaseDelay, restartBackoffMaxDelay, restartBackoffResetAfter, true)
}

func shouldAutoRestartAfterRun(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	return err == nil || IsRetryableError(err)
}

func shouldRestartAfterExit(ctx context.Context, restartAllowed, restartRequested bool) bool {
	if restartRequested {
		return true
	}
	if ctx.Err() != nil {
		return false
	}
	return restartAllowed
}

// maybeAutoRestart re-reads the options and restarts the tunnel with
// exponential backoff when auto-restart is enabled. ctx belongs to the run
// that just ended; cancelling it (Stop) aborts the pending restart.
func (i *Instance) maybeAutoRestart(ctx context.Context) {
	if err := ctx.Err(); err != nil {
		logDebugf("Tunnel %q auto-restart canceled: %v", i.name, err)
		return
	}

	opts, err := i.optsFn()
	if err != nil {
		logWarnf("Tunnel %q auto-restart skipped: %v", i.name, err)
		return
	}
	if !opts.AutoRestart {
		logInfof("Tunnel %q: auto-restart is disabled, tunnel will not restart", i.name)
		return
	}

	i.mu.Lock()
	if i.restartBackoff == nil {
		i.restartBackoff = NewRestartBackoff()
	}

	// Reset restart state if the last retry was long enough ago to consider
	// the next failure a fresh incident.
	if time.Since(i.lastRestart) > restartBackoffResetAfter {
		i.restartCount = 0
		i.restartBackoff.Reset()
	}

	if i.restartCount >= maxRestartAttempts {
		logWarnf("Tunnel %q: maximum restart attempts reached (%d), stopping auto-restart", i.name, i.restartCount)
		i.mu.Unlock()
		return
	}

	delay := i.restartBackoff.Duration()
	i.restartCount++
	i.lastRestart = time.Now()
	attemptNum := i.restartCount
	i.mu.Unlock()

	logInfof("Tunnel %q auto-restarting in %v (attempt %d)...", i.name, delay, attemptNum)
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		logInfof("Tunnel %q auto-restart canceled before attempt %d: %v", i.name, attemptNum, ctx.Err())
		return
	case <-timer.C:
	}

	if err := ctx.Err(); err != nil {
		logInfof("Tunnel %q auto-restart canceled before attempt %d: %v", i.name, attemptNum, err)
		return
	}
	if err := i.Start(); err != nil {
		logErrorf("Failed to restart tunnel %q: %v", i.name, err)
	}
}
