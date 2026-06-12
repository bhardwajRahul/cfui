// Package cloudflared owns every interaction with the embedded cloudflared
// library. It separates process-wide state (tunnel.Init, CLI exit interception,
// Prometheus registration), which can only be set up once, from per-tunnel
// state (Instance), so several tunnel instances can run side by side in one
// process.
//
// Known process-wide limitations inherited from the embedded library:
//   - tunnel.Init can run only once, so the software name shown in the
//     Cloudflare dashboard is fixed by the first EnsureInit call.
//   - All instances share one Prometheus registry; duplicate metric
//     registrations from later instances are silently absorbed.
//   - The graceful-shutdown channel is shared. Closing it (ShutdownProcess or
//     cloudflared's own SIGTERM handler) stops every instance, so individual
//     instances are stopped via context cancellation instead.
package cloudflared

import (
	"cfui/internal/logger"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"cfui/version"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/tunnel"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/updater"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/urfave/cli/v2"
)

var (
	initOnce     sync.Once
	initErr      error
	shutdownOnce sync.Once

	// gracefulShutdownC is handed to tunnel.Init and shared by all tunnel
	// runs. cloudflared closes it from its own signal handler on
	// SIGTERM/SIGINT, and ShutdownProcess closes it on app shutdown.
	gracefulShutdownC = make(chan struct{})

	// metricsRegistry collects metrics from all tunnel instances. It is
	// installed as the global default registerer exactly once, wrapped so
	// that re-registrations from restarted or parallel instances are
	// ignored instead of panicking.
	metricsRegistry = prometheus.NewRegistry()
)

// EnsureInit initializes the embedded cloudflared library. It is safe to call
// from every instance start; only the first call takes effect because
// cloudflared registers global state that cannot be re-initialized. The
// software name shown in the Cloudflare dashboard is taken from the first
// call.
func EnsureInit(softwareName string) error {
	initOnce.Do(func() {
		defer func() {
			if rec := recover(); rec != nil {
				initErr = fmt.Errorf("cloudflared init panic: %v", rec)
				logErrorf("Panic during cloudflared initialization: %v", rec)
			}
		}()

		if strings.TrimSpace(softwareName) == "" {
			softwareName = "cfui"
		}
		version.ChangeSoftName(softwareName)
		buildInfo := cliutil.GetBuildInfo("dockers-x", version.GetFullVersion())

		updater.Init(buildInfo)
		tunnel.Init(buildInfo, gracefulShutdownC)

		// Route every registration through one duplicate-tolerant registry.
		// cloudflared re-registers collectors on each tunnel start; with a
		// plain registry the second start would panic.
		prometheus.DefaultRegisterer = newSafeRegisterer(metricsRegistry)

		// cloudflared's CLI calls os.Exit on fatal errors, which would kill
		// the whole control panel. Intercept it once for the process.
		cli.OsExiter = func(exitCode int) {
			logWarnf("cloudflared CLI attempted to exit with code %d (intercepted)", exitCode)
			if exitCode != 0 {
				panic(fmt.Sprintf("CLI exit with code %d", exitCode))
			}
		}

		logInfof("Cloudflared library initialized (software: %s, version: %s)", softwareName, version.GetFullVersion())
	})
	return initErr
}

// ShutdownProcess broadcasts a graceful shutdown to every tunnel instance by
// closing the shared shutdown channel. Call this only on application exit:
// once closed, no tunnel can be started again in this process.
func ShutdownProcess() {
	shutdownOnce.Do(func() {
		// cloudflared's own signal handler closes the same channel on
		// SIGTERM/SIGINT; tolerate losing that race.
		defer func() {
			if rec := recover(); rec != nil {
				logDebugf("Graceful shutdown channel already closed: %v", rec)
			}
		}()
		close(gracefulShutdownC)
	})
}

// MetricsRegistry returns the shared Prometheus registry that collects
// metrics from all tunnel instances.
func MetricsRegistry() *prometheus.Registry {
	return metricsRegistry
}

// Process signal ownership.
//
// Every tunnel run spawns an upstream waitForSignal goroutine that closes the
// shared graceful-shutdown channel when SIGTERM/SIGINT arrives, and those
// goroutines outlive their run (they only exit on a signal or on channel
// close). After more than one run per process lifetime - an auto-restart or a
// second parallel instance - a single OS signal wakes several of them and the
// second close panics the whole process with "close of closed channel".
//
// cfui therefore claims exclusive signal ownership: main registers its
// shutdown channel through OwnProcessSignals, and after every tunnel run
// launches, reclaim pulses call signal.Reset (which unsubscribes EVERY
// channel registered for those signals, including the upstream watchers) and
// then re-register only cfui's channel. Shutdown is orchestrated entirely by
// cfui via context cancellation and ShutdownProcess, which the upstream run
// loop already honors.

var (
	sigMu      sync.Mutex
	sigChan    chan<- os.Signal
	sigSignals []os.Signal
)

// OwnProcessSignals registers ch as the process's only receiver for the given
// signals and remembers the registration so the package can re-assert it
// whenever the embedded library installs competing handlers. Call once from
// main before any tunnel instance starts; any future signal subscription in
// cfui must go through this function or it will be dropped by the reclaim.
func OwnProcessSignals(ch chan<- os.Signal, sigs ...os.Signal) {
	sigMu.Lock()
	defer sigMu.Unlock()
	sigChan = ch
	sigSignals = append([]os.Signal(nil), sigs...)
	if len(sigSignals) == 0 {
		return
	}
	signal.Reset(sigSignals...)
	signal.Notify(ch, sigs...)
}

// reclaimProcessSignals re-asserts exclusive signal ownership. No-op unless
// OwnProcessSignals was called.
func reclaimProcessSignals() {
	sigMu.Lock()
	defer sigMu.Unlock()
	if sigChan == nil {
		return
	}
	if len(sigSignals) == 0 {
		return
	}
	// Reset drops all subscribers of these signals (ours included); the
	// re-Notify restores ours. The microsecond gap in between falls back to
	// default signal handling - vanishingly unlikely to be hit, and benign
	// next to the guaranteed close panic it prevents.
	signal.Reset(sigSignals...)
	signal.Notify(sigChan, sigSignals...)
}

// scheduleSignalReclaim disarms upstream signal watchers shortly after a
// tunnel run launches. The upstream signal.Notify happens inside RunContext;
// early pulses shrink the armed window and later pulses cover slow starts.
func scheduleSignalReclaim() {
	sigMu.Lock()
	registered := sigChan != nil
	sigMu.Unlock()
	if !registered {
		return
	}
	go func() {
		// Cumulative pulses at ~10ms, 50ms, 250ms, 1s, and 3s after launch.
		for _, delay := range []time.Duration{10 * time.Millisecond, 40 * time.Millisecond, 200 * time.Millisecond, 750 * time.Millisecond, 2 * time.Second} {
			time.Sleep(delay)
			reclaimProcessSignals()
		}
	}()
}

// safeRegisterer wraps a Prometheus registerer and ignores duplicate
// registrations, which cloudflared produces whenever a tunnel is restarted.
type safeRegisterer struct {
	prometheus.Registerer
}

func newSafeRegisterer(reg prometheus.Registerer) prometheus.Registerer {
	return &safeRegisterer{Registerer: reg}
}

func isDuplicateRegistration(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate") || strings.Contains(msg, "already registered")
}

func (s *safeRegisterer) Register(c prometheus.Collector) error {
	err := s.Registerer.Register(c)
	if isDuplicateRegistration(err) {
		logDebugf("Collector already registered (ignored): %v", err)
		return nil
	}
	return err
}

func (s *safeRegisterer) MustRegister(cs ...prometheus.Collector) {
	for _, c := range cs {
		if err := s.Register(c); err != nil {
			panic(err)
		}
	}
}

// Logging helpers tolerate an uninitialized global logger so the package can
// be exercised from unit tests without logger setup.

func logDebugf(format string, args ...any) {
	if logger.Sugar != nil {
		logger.Sugar.Debugf(format, args...)
	}
}

func logInfof(format string, args ...any) {
	if logger.Sugar != nil {
		logger.Sugar.Infof(format, args...)
	}
}

func logWarnf(format string, args ...any) {
	if logger.Sugar != nil {
		logger.Sugar.Warnf(format, args...)
	}
}

func logErrorf(format string, args ...any) {
	if logger.Sugar != nil {
		logger.Sugar.Errorf(format, args...)
	}
}
