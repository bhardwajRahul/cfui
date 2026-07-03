package cloudflared

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cloudflare/backoff"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/tunnel"
	"github.com/urfave/cli/v2"
)

const (
	defaultStopTimeout = 30 * time.Second

	maxProtocolFailuresBeforeSwitch = 3
)

// ErrAlreadyRunning is returned by Start when the instance is running.
var ErrAlreadyRunning = errors.New("already running")

// OptionsProvider returns fresh launch options. It is called on every start
// and auto-restart so configuration changes apply without recreating the
// instance. Returning an error blocks the (re)start.
type OptionsProvider func() (Options, error)

// Status is a point-in-time snapshot of an instance.
type Status struct {
	Running   bool
	LastError error
	// Protocol is the transport currently selected by the fallback logic
	// (quic, http2, or auto before the first start).
	Protocol string
}

// Instance manages the lifecycle of one cloudflared tunnel: start, stop,
// protocol fallback, and auto-restart with exponential backoff. Each tunnel
// profile gets its own Instance; all instances share the process-wide
// cloudflared runtime set up by EnsureInit.
type Instance struct {
	name   string
	optsFn OptionsProvider

	mu          sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{} // closed when the current run's goroutine exits
	running     bool
	lastError   error
	configFile  string
	stopTimeout time.Duration
	// restartRequested is set when cfui cancels a run to recover a tunnel that
	// is still running locally but no longer has active edge connections.
	restartRequested bool

	restartCount   int
	lastRestart    time.Time
	restartBackoff *backoff.Backoff

	// Protocol fallback management (for auto mode).
	currentProtocol     string
	protocolFailures    map[string]int
	lastProtocolSwitch  time.Time
	protocolSwitchCount int
}

// NewInstance creates an instance named after its tunnel profile. The name
// only appears in logs and error messages.
func NewInstance(name string, optsFn OptionsProvider) *Instance {
	return &Instance{
		name:             name,
		optsFn:           optsFn,
		stopTimeout:      defaultStopTimeout,
		protocolFailures: make(map[string]int),
		restartBackoff:   NewRestartBackoff(),
		currentProtocol:  "auto",
	}
}

// Name returns the instance name.
func (i *Instance) Name() string {
	return i.name
}

// Start launches the tunnel. It returns ErrAlreadyRunning when called twice
// without an intervening stop or exit.
func (i *Instance) Start() (err error) {
	// Outermost panic guard: a failure inside the embedded library during
	// launch must not take down the whole control panel.
	defer func() {
		if rec := recover(); rec != nil {
			logErrorf("Panic during tunnel %q start (recovered): %v", i.name, rec)
			err = fmt.Errorf("start panic: %v", rec)
		}
	}()

	opts, err := i.optsFn()
	if err != nil {
		logErrorf("Cannot start tunnel %q: %v", i.name, err)
		return err
	}
	if err := opts.Validate(); err != nil {
		logErrorf("Cannot start tunnel %q: %v", i.name, err)
		return err
	}
	if err := EnsureInit(opts.SoftwareName); err != nil {
		return err
	}

	i.mu.Lock()
	if i.running {
		i.mu.Unlock()
		logWarnf("Attempted to start tunnel %q that is already running", i.name)
		return ErrAlreadyRunning
	}

	// Cancel any leftover context (e.g. a pending auto-restart timer) after
	// publishing the new state so cancellation callbacks never run under i.mu.
	oldCancel := i.cancel

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	i.ctx, i.cancel, i.done = ctx, cancel, done
	i.running = true
	i.lastError = nil
	i.restartRequested = false
	i.mu.Unlock()

	logInfof("Starting cloudflared tunnel %q", i.name)
	go i.runTunnel(ctx, opts, done)
	if oldCancel != nil {
		oldCancel()
	}

	return nil
}

// Stop terminates the tunnel via context cancellation and waits for the run
// goroutine to exit. Individual instances must not touch the shared graceful
// shutdown channel: cloudflared closes it on SIGTERM (so sending could panic)
// and a stray token could stop an unrelated instance.
func (i *Instance) Stop() error {
	i.mu.Lock()
	if !i.running {
		cancel := i.cancel
		i.cancel = nil
		i.restartRequested = false
		i.mu.Unlock()
		if cancel != nil {
			cancel()
			logDebugf("Canceled pending restart of tunnel %q", i.name)
			return nil
		}
		logDebugf("Stop called but tunnel %q is not running", i.name)
		return nil
	}

	logInfof("Initiating shutdown of tunnel %q", i.name)
	cancel := i.cancel
	i.cancel = nil
	i.restartRequested = false
	done := i.done
	timeout := i.stopTimeout
	i.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		logInfof("Tunnel %q stopped gracefully", i.name)
		return nil
	case <-timer.C:
		logWarnf("Tunnel %q stop timeout exceeded (%v)", i.name, timeout)
		// The run goroutine is stuck inside the library; reflect reality in
		// the state and reclaim what we can.
		i.mu.Lock()
		i.running = false
		i.mu.Unlock()
		i.cleanupConfigFile()
		return fmt.Errorf("timeout waiting for tunnel %q to stop", i.name)
	}
}

// Status returns a snapshot of the instance state.
func (i *Instance) Status() Status {
	i.mu.Lock()
	defer i.mu.Unlock()
	return Status{
		Running:   i.running,
		LastError: i.lastError,
		Protocol:  i.currentProtocol,
	}
}

// selectProtocol determines which protocol to use based on configuration and
// failure history. Callers must hold i.mu.
func (i *Instance) selectProtocol(configProtocol string) string {
	// If the user explicitly chose a protocol, always use it.
	if configProtocol != "" && configProtocol != "auto" {
		i.currentProtocol = configProtocol
		return configProtocol
	}

	// Auto mode: cycle quic -> http2 -> quic after repeated failures.
	if i.protocolFailures[i.currentProtocol] >= maxProtocolFailuresBeforeSwitch {
		var nextProtocol string
		if i.currentProtocol == "quic" || i.currentProtocol == "auto" {
			nextProtocol = "http2"
		} else {
			nextProtocol = "quic"
		}

		logWarnf("Tunnel %q: protocol %s has failed %d times, switching to %s",
			i.name, i.currentProtocol, i.protocolFailures[i.currentProtocol], nextProtocol)

		// Reset the failing protocol's count so it gets a fresh start if we
		// ever switch back.
		i.protocolFailures[i.currentProtocol] = 0

		i.currentProtocol = nextProtocol
		i.lastProtocolSwitch = time.Now()
		i.protocolSwitchCount++

		return nextProtocol
	}

	if i.currentProtocol == "" || i.currentProtocol == "auto" {
		i.currentProtocol = "quic"
	}
	return i.currentProtocol
}

// recordProtocolSuccess clears failure history after a clean exit so no
// protocol stays blacklisted forever.
func (i *Instance) recordProtocolSuccess() {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.currentProtocol != "" && i.currentProtocol != "auto" {
		logInfof("Tunnel %q: protocol %s connected successfully, resetting failure counts", i.name, i.currentProtocol)

		i.restartCount = 0
		if i.restartBackoff != nil {
			i.restartBackoff.Reset()
		}
		for proto := range i.protocolFailures {
			i.protocolFailures[proto] = 0
		}
	}
}

// recordProtocolFailure increments the failure count for the current protocol
// when the error looks transport-related.
func (i *Instance) recordProtocolFailure(err error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.currentProtocol == "" || i.currentProtocol == "auto" {
		i.currentProtocol = "quic"
	}

	if IsProtocolRelatedError(err) {
		i.protocolFailures[i.currentProtocol]++
		logWarnf("Tunnel %q: protocol %s failure count: %d (error: %v)",
			i.name, i.currentProtocol, i.protocolFailures[i.currentProtocol], err)
	}
}

func (i *Instance) runTunnel(ctx context.Context, opts Options, done chan struct{}) {
	restartAllowed := true
	defer close(done)
	defer func() {
		if rec := recover(); rec != nil {
			logErrorf("Recovered from panic in tunnel %q: %v", i.name, rec)
			i.mu.Lock()
			i.lastError = fmt.Errorf("tunnel panic: %v", rec)
			i.mu.Unlock()
		}

		i.cleanupConfigFile()

		i.mu.Lock()
		restartRequested := i.restartRequested
		i.restartRequested = false
		i.running = false
		restartCtx := ctx
		if restartRequested {
			var restartCancel context.CancelFunc
			restartCtx, restartCancel = context.WithCancel(context.Background())
			i.ctx = restartCtx
			i.cancel = restartCancel
		}
		i.mu.Unlock()

		if shouldRestartAfterExit(ctx, restartAllowed, restartRequested) {
			logWarnf("Tunnel %q exited unexpectedly, checking auto-restart policy", i.name)
			i.maybeAutoRestart(restartCtx)
		}
	}()

	app := &cli.App{
		Name:     "cloudflared-web",
		Commands: tunnel.Commands(),
		// Prevent cli from calling os.Exit on errors.
		ExitErrHandler: func(c *cli.Context, err error) {
			if err != nil {
				logErrorf("Tunnel %q CLI error handler caught: %v", i.name, err)
			}
		},
	}

	var configFile string
	if opts.CustomTag != "" {
		file, err := createTempConfig(opts.CustomTag)
		if err != nil {
			logWarnf("Tunnel %q: failed to create config file for custom tag: %v", i.name, err)
		} else {
			configFile = file
			i.mu.Lock()
			i.configFile = file
			i.mu.Unlock()
			logInfof("Tunnel %q using custom identifier tag: %s", i.name, opts.CustomTag)
		}
	}

	i.mu.Lock()
	selectedProtocol := i.selectProtocol(opts.Protocol)
	if opts.Protocol == "auto" {
		logDebugf("Tunnel %q protocol failure counts: quic=%d, http2=%d",
			i.name, i.protocolFailures["quic"], i.protocolFailures["http2"])
	}
	i.mu.Unlock()

	readinessURL := i.configureReadinessProbe(&opts)
	args := BuildArgs(opts, selectedProtocol, configFile)

	logInfof("Starting cloudflared tunnel %q with protocol=%s (selected), config_protocol=%s, region=%s, retries=%d",
		i.name, selectedProtocol, opts.Protocol, opts.Region, opts.Retries)

	// The run we are about to launch registers an upstream signal watcher;
	// schedule pulses that strip it (and any stale ones) again.
	scheduleSignalReclaim()
	if readinessURL != "" {
		go i.monitorReadiness(ctx, readinessURL)
	}

	err := app.RunContext(ctx, args)
	restartAllowed = shouldAutoRestartAfterRun(ctx, err)

	// Context cancellation means a user-requested stop.
	if ctx.Err() != nil {
		if i.hasRestartRequest() {
			logWarnf("Tunnel %q stopped by readiness watchdog for restart", i.name)
		} else {
			logInfof("Tunnel %q stopped by user request", i.name)
		}
		return
	}

	if err != nil {
		logErrorf("Tunnel %q error: %v", i.name, err)
		i.mu.Lock()
		i.lastError = err
		i.mu.Unlock()

		i.recordProtocolFailure(err)

		if !restartAllowed {
			logWarnf("Tunnel %q: non-retryable error detected: %v", i.name, err)
			return
		}
	} else {
		i.recordProtocolSuccess()
		logInfof("Tunnel %q exited cleanly", i.name)
	}
}

// createTempConfig writes a temporary YAML config carrying the custom tag
// (cloudflared expects tags as a string slice).
func createTempConfig(customTag string) (string, error) {
	tempFile, err := os.CreateTemp("", "cloudflared-*.yaml")
	if err != nil {
		return "", err
	}
	defer tempFile.Close()

	configContent := fmt.Sprintf("tag:\n  - version=%s\n", customTag)
	if _, err := tempFile.WriteString(configContent); err != nil {
		os.Remove(tempFile.Name())
		return "", err
	}

	return tempFile.Name(), nil
}

// cleanupConfigFile removes the temporary config file if one exists.
func (i *Instance) cleanupConfigFile() {
	i.mu.Lock()
	configFile := i.configFile
	i.configFile = ""
	i.mu.Unlock()

	if configFile == "" {
		return
	}
	if err := os.Remove(configFile); err != nil && !os.IsNotExist(err) {
		logWarnf("Failed to remove temporary config file %s: %v", configFile, err)
	} else {
		logDebugf("Cleaned up temporary config file: %s", configFile)
	}
}
