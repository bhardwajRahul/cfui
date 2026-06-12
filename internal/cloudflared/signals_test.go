//go:build !windows

package cloudflared

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// TestOwnProcessSignalsReclaim verifies that reclaimProcessSignals strips a
// competing subscriber (simulating cloudflared's per-run waitForSignal) while
// keeping cfui's own channel armed.
func TestOwnProcessSignalsReclaim(t *testing.T) {
	defer func() {
		sigMu.Lock()
		sigChan = nil
		sigSignals = nil
		sigMu.Unlock()
	}()

	competitor := make(chan os.Signal, 1)
	signal.Notify(competitor, syscall.SIGUSR1)
	defer signal.Stop(competitor)

	ours := make(chan os.Signal, 1)
	OwnProcessSignals(ours, syscall.SIGUSR1)
	defer signal.Stop(ours)

	reclaimProcessSignals()

	if err := syscall.Kill(os.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatalf("failed to send SIGUSR1: %v", err)
	}

	select {
	case <-ours:
	case <-time.After(2 * time.Second):
		t.Fatal("our channel did not receive the signal after reclaim")
	}

	select {
	case s := <-competitor:
		t.Fatalf("competitor received %v after reclaim; expected it to be unsubscribed", s)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestScheduleSignalReclaimNoOpWithoutOwner ensures tunnel starts in
// processes that never registered a signal owner (tests, embedders) don't
// touch global signal state.
func TestScheduleSignalReclaimNoOpWithoutOwner(t *testing.T) {
	probe := make(chan os.Signal, 1)
	signal.Notify(probe, syscall.SIGUSR2)
	defer signal.Stop(probe)

	scheduleSignalReclaim() // sigChan is nil -> must not spawn reclaims

	time.Sleep(400 * time.Millisecond) // past the first would-be pulse

	if err := syscall.Kill(os.Getpid(), syscall.SIGUSR2); err != nil {
		t.Fatalf("failed to send SIGUSR2: %v", err)
	}
	select {
	case <-probe:
	case <-time.After(2 * time.Second):
		t.Fatal("probe subscription was disturbed despite no registered owner")
	}
}
