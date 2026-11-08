package recurse

import (
	"context"
	"errors"
	"time"

	"github.com/miekg/dns"
	"github.com/semihalev/sdns/config"
	"github.com/semihalev/sdns/middleware"
	"github.com/semihalev/sdns/middleware/resolver"
)

// Root hints, not recursive upstreams: SDNS follows referrals from these servers.
var rootHints = []string{
	"198.41.0.4:53", "170.247.170.2:53", "192.33.4.12:53",
	"199.7.91.13:53", "192.203.230.10:53", "192.5.5.241:53",
	"192.112.36.4:53", "198.97.190.53:53", "192.36.148.17:53",
	"192.58.128.30:53", "193.0.14.129:53", "199.7.83.42:53", "202.12.27.33:53",
}

type sdnsEngine struct {
	queryer middleware.Queryer
	handler *resolver.DNSHandler
	timeout time.Duration
}

func newSDNSEngine(roots []string, timeout time.Duration) *sdnsEngine {
	cfg := &config.Config{
		RootServers:   append([]string(nil), roots...),
		DNSSEC:        "off",
		Maxdepth:      30,
		QueryTimeout:  config.Duration{Duration: timeout},
		Timeout:       config.Duration{Duration: 2 * time.Second},
		QnameMinLevel: 10,
	}
	h := resolver.NewEmbedded(cfg)
	registry := middleware.NewRegistry()
	registry.Register(h.Name(), func(*config.Config) middleware.Handler { return h })
	q := middleware.NewPipelineQueryer(registry.Build(cfg))
	h.SetQueryer(q)
	h.SetPrefetchQueryer(q)
	return &sdnsEngine{queryer: q, handler: h, timeout: timeout}
}

func (e *sdnsEngine) Resolve(ctx context.Context, request *dns.Msg) (*dns.Msg, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, errors.New("nil DNS request")
	}
	// SDNS changes flags and EDNS while walking. CoreDNS and outer plugins
	// retain the original request, so give the resolver its own message.
	query := request.Copy()
	if query.Opcode != dns.OpcodeQuery || len(query.Question) != 1 {
		return new(dns.Msg).SetRcode(request, dns.RcodeFormatError), nil
	}
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	response, err := e.queryer.Query(ctx, query)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return response, err
}

func (e *sdnsEngine) close() error {
	e.handler.Stop()
	return nil
}
