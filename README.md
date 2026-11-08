# recurse

An external CoreDNS plugin embedding SDNS's normal recursive resolver, cache,
EDNS, failover, and forwarding middleware. CoreDNS owns the DNS listeners and
outer plugin chain; each `recurse` instance owns an independent SDNS pipeline.

## Configuration

Use an ordinary SDNS TOML configuration file:

```corefile
.:1053 {
    bind 127.0.0.1
    errors
    recurse examples/sdns.conf
}
```

The [sample SDNS configuration](examples/sdns.conf) enables DNSSEC validation,
root priming, automated trust-anchor upkeep, cache pruning, bounded nameserver
refresh/enrichment, and TCP pooling. Its trust-anchor seeds come from SDNS's
normal default configuration. SDNS maintains their state in `.build/sdns-state`.
Keep that directory across restarts. Independently managed instances must use
separate state directories. Configuration paths follow CoreDNS's working directory.

The file is loaded through SDNS's ordinary `config.Load`, including validation,
defaults, state-directory initialization, and random cookie-secret generation.
It must already exist; an incorrect path fails startup. A Corefile block cannot
be combined with the file argument.

The embedded middleware consumes SDNS's resolver, cache, EDNS, forwarding, and
failover settings. Standalone listeners, API, logging configuration, and optional
policy middlewares such as blocklists, RPZ, hosts, Kubernetes, and DNS64 are not
installed by this plugin. Configure those policies through CoreDNS's outer chain
where needed; this is not a drop-in replacement for the entire SDNS server.

For compatibility, `recurse` alone uses built-in IPv4 root hints with DNSSEC off.
It also accepts the existing small block form:

```corefile
recurse {
    root 198.41.0.4:53 192.33.4.12:53
    timeout 10s
}
```

`root` supplies authoritative root hints, not recursive upstreams. `timeout`
bounds a lookup and defaults to 10 seconds. Use the SDNS config-file form for
DNSSEC and the full set of supported resolver options.

## Build and run

Requires Go 1.27+ and a CoreDNS Git checkout (default `../coredns`). The module
pins the lifecycle implementation in the SDNS fork; it does not apply patches or
require a sibling SDNS checkout. Upstreaming that commit can later replace the
fork pin without changing the embedding interface.

```sh
go test -race ./... -count=1
./scripts/build-poc.sh
./.build/coredns -conf examples/Corefile
```

In a second terminal:

```sh
dig @127.0.0.1 -p 1053 example.org A +dnssec
dig @127.0.0.1 -p 1053 example.org A +dnssec +tcp
```

Expected: `NOERROR`, `rd ra ad`, and A records. Stop the server with Ctrl-C.
Public recursion requires outbound UDP/TCP port 53. The example listens on
`127.0.0.1:1053`.

The build script copies committed CoreDNS source into `.build/coredns-src`, adds
`recurse` before `forward` in that copy's `plugin.cfg`, generates registration,
and builds `.build/coredns`. It preserves the original CoreDNS checkout. An
optional second argument uses a local SDNS checkout for development:

```sh
./scripts/build-poc.sh /path/to/coredns /path/to/sdns
```

## Lifecycle

There is no embedding-specific SDNS constructor or background-disable setting.
The adapter uses `resolver.New(cfg)` and `Registry.Build(cfg)`. Building an SDNS
pipeline wires dependencies, restores state, then starts maintenance. The same
lifecycle is used by standalone SDNS. See [SDNS's middleware API](https://pkg.go.dev/github.com/semihalev/sdns/middleware).

CoreDNS constructs the pipeline in its startup hook after accepting the entire
Corefile. Shutdown rejects new queries, cancels and drains in-flight calls,
stops pipeline handlers, joins instance-owned background work, closes pooling
and cleanup loops, and persists configured cache state. Failed configuration
reloads leave the existing instance serving. Shared bounded SDNS enrichment
workers remain process-wide and skip work belonging to stopped instances.

SDNS's cache completes CNAME chains. Its EDNS middleware handles DNSSEC response
policy, and the outer adapter applies the client's UDP/TCP size limit. Complete
SDNS failure replies, including Extended DNS Errors, are retained.

## Automated testing

[The test plan](docs/test-plan.md) records exact commands, outputs, coverage,
configuration, and limits. CI runs plugin race tests, the SDNS lifecycle tests,
a complete CoreDNS build, and deterministic UDP/TCP binary smoke tests. Existing
SDNS referral and DNSSEC tests remain the recursion-algorithm coverage.

The optional live test verifies an authenticated public answer with the sample
configuration:

```sh
COREDNS_BINARY="$PWD/.build/coredns" RECURSE_LIVE_TEST=1 \
RECURSE_CONFIG="$PWD/examples/sdns.conf" RECURSE_EXPECT_AD=1 \
go test -run '^TestCoreDNSPublicRecursion$' -count=1 -v
```

This requires public DNS access and is kept separate from deterministic CI.
