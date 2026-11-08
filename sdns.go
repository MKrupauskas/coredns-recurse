package recurse

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"time"

	"github.com/miekg/dns"
	"github.com/semihalev/sdns/config"
	"github.com/semihalev/sdns/middleware"
	"github.com/semihalev/sdns/middleware/cache"
	"github.com/semihalev/sdns/middleware/edns"
	"github.com/semihalev/sdns/middleware/failover"
	"github.com/semihalev/sdns/middleware/forwarder"
	"github.com/semihalev/sdns/middleware/resolver"
)

type sdnsEngine struct {
	mu        sync.Mutex
	closed    bool
	requests  sync.WaitGroup
	closeOnce sync.Once
	ctx       context.Context
	cancel    context.CancelFunc
	cfg       *config.Config
	pipeline  *middleware.Pipeline
	queryer   middleware.Queryer
	timeout   time.Duration
}

func engineFromConfig(cfg *config.Config) *sdnsEngine {
	if cfg.CookieSecret == "" {
		cfg.CookieSecret = rand.Text()
	}
	if cfg.QueryTimeout.Duration == 0 {
		cfg.QueryTimeout.Duration = 10 * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &sdnsEngine{cfg: cfg, ctx: ctx, cancel: cancel, timeout: cfg.QueryTimeout.Duration}
}

// CoreDNS starts configured plugins only after the whole Corefile is accepted.
// Delaying construction also avoids orphaned workers after a rejected reload.
func (e *sdnsEngine) start() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return context.Canceled
	}
	if e.pipeline != nil {
		return nil
	}
	registry := middleware.NewRegistry()
	registry.Register("edns", func(c *config.Config) middleware.Handler { return edns.New(c) })
	registry.Register("cache", func(c *config.Config) middleware.Handler { return cache.New(c) })
	registry.Register("failover", func(c *config.Config) middleware.Handler { return failover.New(c) })
	registry.Register("resolver", func(c *config.Config) middleware.Handler { return resolver.New(c) })
	registry.Register("forwarder", func(c *config.Config) middleware.Handler { return forwarder.New(c) })
	e.pipeline = registry.Build(e.cfg)
	e.queryer = middleware.NewPipelineQueryer(e.pipeline)
	return nil
}

func (e *sdnsEngine) Resolve(ctx context.Context, request *dns.Msg) (*dns.Msg, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, errors.New("nil DNS request")
	}
	if err := e.start(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil, context.Canceled
	}
	e.requests.Add(1)
	e.mu.Unlock()
	defer e.requests.Done()
	// SDNS changes flags and EDNS while walking. CoreDNS and outer plugins
	// retain the original request, so give the resolver its own message.
	query := request.Copy()
	if query.Opcode != dns.OpcodeQuery || len(query.Question) != 1 {
		return new(dns.Msg).SetRcode(request, dns.RcodeFormatError), nil
	}
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	stop := context.AfterFunc(e.ctx, cancel)
	defer stop()
	response, err := e.queryer.Query(ctx, query)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return response, err
}

func (e *sdnsEngine) close() error {
	e.closeOnce.Do(func() {
		e.mu.Lock()
		e.closed = true
		e.cancel()
		e.mu.Unlock()
		e.requests.Wait()
		e.pipeline.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		e.pipeline.Persist(ctx)
	})
	return nil
}
