// Package supervisor owns the running host agent. It can swap the agent for a
// new one when the configuration changes, which is what the web interface needs
// in order to apply settings without restarting the process.
package supervisor

import (
	"context"
	"log/slog"
	"sync"

	"github.com/Sunse666/NoneFrp/internal/client"
	"github.com/Sunse666/NoneFrp/internal/config"
)

// Runner keeps exactly one agent alive.
type Runner struct {
	mu      sync.Mutex
	path    string
	dataDir string
	cfg     config.Client
	log     *slog.Logger

	parent context.Context
	agent  *client.Client
	cancel context.CancelFunc
	done   chan struct{}
}

// New returns a runner that persists settings to path and stores agent state in
// dataDir.
func New(path, dataDir string, cfg config.Client, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{path: path, dataDir: dataDir, cfg: cfg, log: logger}
}

// Start runs the agent until ctx is cancelled.
func (r *Runner) Start(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.parent = ctx
	if err := r.startLocked(r.cfg); err != nil {
		r.log.Error("could not start the agent", "error", err)
	}
}

// Stop shuts the current agent down.
func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked()
}

// Config returns a copy of the settings currently in use.
func (r *Runner) Config() config.Client {
	r.mu.Lock()
	defer r.mu.Unlock()
	return copyClient(r.cfg)
}

// Status reports what the running agent is doing.
func (r *Runner) Status() client.Status {
	r.mu.Lock()
	agent := r.agent
	cfg := r.cfg
	r.mu.Unlock()

	if agent == nil {
		return client.Status{
			LocalPort:     cfg.Local.Port,
			ProxyProtocol: cfg.Local.ProxyProtocol,
		}
	}
	return agent.Status()
}

// Apply validates cfg, writes it to disk and restarts the agent with it. The
// previous agent keeps running when validation or the write fails.
func (r *Runner) Apply(cfg config.Client) error {
	if err := config.SaveClient(r.path, cfg); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopLocked()
	if err := r.startLocked(cfg); err != nil {
		return err
	}
	r.log.Info("settings applied",
		"relay", cfg.Relay[0].URL,
		"local_port", cfg.Local.Port,
	)
	return nil
}

func (r *Runner) startLocked(cfg config.Client) error {
	agent, err := client.New(cfg, r.dataDir, r.log)
	if err != nil {
		return err
	}

	parent := r.parent
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})

	r.agent = agent
	r.cancel = cancel
	r.done = done
	r.cfg = cfg

	go func() {
		defer close(done)
		_ = agent.Run(ctx)
	}()
	return nil
}

func (r *Runner) stopLocked() {
	if r.cancel == nil {
		return
	}
	r.cancel()
	<-r.done
	r.agent = nil
	r.cancel = nil
	r.done = nil
}

func copyClient(cfg config.Client) config.Client {
	out := cfg
	out.Relay = append([]config.Relay(nil), cfg.Relay...)
	return out
}
