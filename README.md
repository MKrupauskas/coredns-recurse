# recurse

An external CoreDNS plugin that embeds the SDNS iterative recursive resolver.
This POC resolves a direct A question from root hints through authoritative DNS
servers, inside the CoreDNS process.

```text
DNS client → CoreDNS → recurse → SDNS → root → TLD → authority → A answer
```

The initial target is IPv4, direct A records, and DNSSEC disabled. There is no
forwarder or separate SDNS server. CoreDNS owns the listener; SDNS owns recursion
and its bounded delegation/glue caches.

## Run the POC

Requires **Go 1.27+**, Git, and sibling checkouts of `coredns-recurse`, `coredns`,
and `sdns`. The Go minimum comes from the current SDNS module. Verified with
Go 1.27.2, CoreDNS commit `f79be2a2f8266c2881d01f4c884149e8067569c9`
(reporting 1.15.0), and SDNS commit
`2226f31b50fd1f146f452165acdcd7520213ea8f` plus the included patch.

The local SDNS checkout needs the new `resolver.NewEmbedded` constructor. It has
already been implemented in the companion checkout for this POC. For a fresh
checkout at the pinned SDNS commit, apply it once from this repository:

```sh
git -C ../sdns apply "$PWD/patches/sdns-embedded.patch"
```

Build and run:

```sh
./scripts/build-poc.sh
./.build/coredns -conf examples/Corefile
```

In another terminal:

```sh
dig @127.0.0.1 -p 1053 example.org A
dig @127.0.0.1 -p 1053 example.org A +tcp
```

Expected: `status: NOERROR`, `rd ra`, and A records in the answer. The example
binds only to `127.0.0.1:1053`. Public recursion requires outbound UDP/TCP port 53.
The binary continues running until interrupted.

The build script copies the committed CoreDNS source to `.build/coredns-src`,
adds `recurse` before `forward` in that copy's `plugin.cfg`, generates the plugin
registration, and builds `.build/coredns`. It leaves the source CoreDNS checkout
untouched. Alternate checkouts can be supplied as arguments:

```sh
./scripts/build-poc.sh /path/to/coredns /path/to/sdns
```

The plugin module deliberately replaces SDNS with `../sdns` because the new
constructor is not published upstream. The build script also installs this
replacement in the CoreDNS build module; Go does not inherit dependency modules'
`replace` directives. Package tests expect the default sibling SDNS checkout.

## Configuration

The minimal Corefile is in [examples/Corefile](examples/Corefile):

```corefile
.:1053 {
    bind 127.0.0.1
    errors
    recurse
}
```

Optional settings:

```corefile
recurse {
    root 198.41.0.4:53 192.33.4.12:53
    timeout 10s
}
```

`root` replaces the built-in 13 IPv4 root hints with explicit IPv4 address:port
endpoints. These must serve the root namespace; they are not recursive upstreams.
`timeout` is a positive duration bounding the whole lookup, default `10s`.
Each option and `recurse` itself may appear only once per server block.

This is a terminal handler. Plugin execution order comes from `plugin.cfg`, as
explained in the [CoreDNS plugin documentation](https://coredns.io/manual/plugins/).
For the initial demo, use the example without other answering plugins.

## Research and implementation

The existing SDNS `Resolver.Resolve` API exposes internal walk state, including an
`internal/authority` type and private root-server state. Its public DNS middleware
handler already provides the right DNS semantics, and the existing `Registry`,
`Pipeline`, and `NewPipelineQueryer` APIs support in-memory calls and internal
nameserver lookups. See [SDNS's resolver API](https://pkg.go.dev/github.com/semihalev/sdns/middleware/resolver)
and [middleware API](https://pkg.go.dev/github.com/semihalev/sdns/middleware).

The plan implemented here was to add an embedding constructor, adapt the existing
in-memory API, and then verify both referral walking and a compiled external
plugin. The SDNS production-code patch is only **27 added lines and 4 removed
lines in two files**, plus a regression test:

- `New` and `NewResolver` keep their existing behavior.
- `NewEmbedded` reuses their initialization while skipping maintenance and
  enrichment startup. It never waits for global middleware readiness.
- No recursive-walk or network-exchange algorithm changes are needed.

The CoreDNS adapter builds a resolver-only pipeline per configured instance,
wires its internal queryer, copies incoming requests, bounds their lifetime, and
registers shutdown/restart hooks. It never publishes an SDNS global pipeline.
Failures follow CoreDNS's response-writing convention and produce SERVFAIL.
The exact SDNS change and test are in [patches/sdns-embedded.patch](patches/sdns-embedded.patch).

## Verification

See [the test plan and recorded command outputs](docs/test-plan.md) for the
coverage matrix, local results, and manual UDP/TCP lookup transcript.

```sh
go test ./...
go test -race ./...
(cd ../sdns && go test -race ./middleware/resolver -run 'Test(Embedded|HermeticResolve|Handler)' -count=1)
./scripts/build-poc.sh
COREDNS_BINARY="$PWD/.build/coredns" go test -run '^TestCoreDNSEndToEnd$' -v
COREDNS_BINARY="$PWD/.build/coredns" RECURSE_LIVE_TEST=1 go test -run '^TestCoreDNSPublicRecursion$' -v
```

Regular tests use local unprivileged DNS sockets and check A answers, NXDOMAIN,
request preservation, deadlines, cancellation, setup parsing, and instance
isolation. The SDNS regression test follows real referrals to a synthetic zone
with out-of-bailiwick nameservers, forcing the internal queryer to resolve their
addresses. Its existing test-only address mapper redirects advertised port-53
endpoints to unprivileged local ports.

`TestCoreDNSEndToEnd` starts the compiled CoreDNS binary and checks both UDP and
TCP through a local authoritative fixture. `TestCoreDNSPublicRecursion` starts a
fresh binary with default hints and checks a real `example.org A` lookup over
both transports. The binary tests skip unless `COREDNS_BINARY` is set; public
recursion also requires the explicit live-test flag. CI runs the deterministic
binary smoke test and applies the same SDNS patch to the pinned upstream source.

## Next iterations

This is a narrow POC, not a complete resolver integration. DNSSEC validation,
IPv6 upstream access, persistent state, priming, automated trust-anchor upkeep,
and periodic cache pruning are disabled. Cache expiry checks still apply.
CNAME-only answers are returned as received; completing their target chain needs
SDNS's cache middleware or another follow-up implementation. Client-facing EDNS
policy, large-response truncation, and answer caching also need further work.
