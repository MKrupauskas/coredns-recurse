# Resolver integration: test plan and recorded results

Recorded 2026-11-08, Go 1.27.2, macOS arm64. Plugin commands run from this
repository; SDNS commands below run from its source checkout.

The SDNS implementation is commit `7b531a3bbf7999d51f6e039d20a6b92a3082b118`
in the user's fork. `go.mod` pins that commit through a standard module
replacement. No patch application or local sibling module is needed for plugin
builds. CoreDNS source remains pinned to
`f79be2a2f8266c2881d01f4c884149e8067569c9` in CI.

## Design under test

Use `resolver.New(cfg)` with ordinary `config.Config` settings. `Registry.Build`
wires all dependencies, restores state, and starts maintenance. Owning pipelines
stop and persist their handlers through instance methods. There is no special
embedding constructor, constructor boolean, or background-disable setting.

`recurse CONFIGFILE` requires an existing ordinary SDNS configuration file.
Root bootstrap addresses come only from its `rootservers` / `root6servers`
settings. The plugin has no compiled-in hint list or custom `root` block.
SDNS's ordinary priming refreshes the live root set from the configured seeds;
OS recursive upstreams are not substituted for root hints.

Maintenance includes root priming, trust-anchor upkeep, pruning, bounded
nameserver enrichment, refresh, and TCP pooling. Shutdown cancels/joins owned
work and cleanup loops. Shared bounded enrichment workers skip stopped owners.
The standalone SDNS server uses the same lifecycle.

## Automated coverage

| Check | Coverage | Automation |
| --- | --- | --- |
| `TestRegistryLifecycle` | Wiring, restore, startup, and one-time shutdown order. | Plugin CI and SDNS's existing test suite. |
| `TestResolverPipelineLifecycle` | Ordinary configuration starts actual local root priming independently of global readiness; shutdown cancels/joins active enrichment and closes TCP/singleflight cleanup. | Plugin CI and SDNS's existing test suite. |
| `TestResolverStopBeforeStart` | A stopped instance cannot restart. | Plugin CI and SDNS's existing test suite. |
| Existing SDNS tests | Recursive referrals, DNSSEC validation, caching, pooling, and server behavior. The new tests do not rebuild the existing synthetic referral network. | Full local SDNS race suite; existing SDNS CI definition. |
| Plugin race tests | A/NXDOMAIN, CNAME completion, immutable caller messages, deadlines, cancellation, concurrent independent instances, shutdown admission/drain, UDP truncation, retained DNSSEC error details, and regular SDNS config loading. Missing files, missing root hints, and legacy bare/block syntax are rejected without fallback hints. | Plugin CI. |
| `TestCoreDNSEndToEnd` | Real CoreDNS binary, regular SDNS config file, local authority, UDP and TCP A replies, and graceful shutdown. | Plugin CI. |
| `TestCoreDNSInvalidReload` | A rejected replacement configuration leaves the previous resolver serving. | Plugin CI. |
| `TestCoreDNSPublicRecursion` | Normal sample config validates a public A lookup, with AD required over UDP and TCP. | Explicit opt-in automated test; outbound DNS required. |

The deterministic authority fixture answers at the configured test root.
Existing SDNS tests cover referral walking; the public test also walks real
root/TLD/authority servers. Public DNS is not a CI prerequisite. SDNS's fork
has not initialized its own workflow runs; plugin CI explicitly executes its
focused lifecycle tests from the pinned source.

## SDNS full race suite

```sh
go test -race ./... -count=1
```

All packages passed. Relevant output excerpt:

```text
ok  	github.com/semihalev/sdns	2.582s
ok  	github.com/semihalev/sdns/middleware	1.356s
ok  	github.com/semihalev/sdns/middleware/cache	8.259s
ok  	github.com/semihalev/sdns/middleware/resolver	54.781s
ok  	github.com/semihalev/sdns/middleware/resolver/dnssec	2.167s
ok  	github.com/semihalev/sdns/middleware/resolver/localroot	1.882s
ok  	github.com/semihalev/sdns/server	32.361s
```

```sh
go test -race ./middleware ./middleware/resolver -run 'Test(RegistryLifecycle|ResolverPipelineLifecycle|ResolverStopBeforeStart)$' -count=1 -v
```

```text
=== RUN   TestRegistryLifecycle
--- PASS: TestRegistryLifecycle (0.00s)
PASS
ok  	github.com/semihalev/sdns/middleware	1.231s
=== RUN   TestResolverPipelineLifecycle
--- PASS: TestResolverPipelineLifecycle (0.01s)
=== RUN   TestResolverStopBeforeStart
--- PASS: TestResolverStopBeforeStart (0.00s)
PASS
ok  	github.com/semihalev/sdns/middleware/resolver	1.302s
```

## Allocation and documentation gates

Both commands exited successfully. Allocation output excerpt:

```sh
go test -count=1 -run 'Alloc|ServeRawHitClasses' ./...
```

```text
ok  	github.com/semihalev/sdns/middleware/resolver	1.760s
ok  	github.com/semihalev/sdns/middleware/resolver/dnssec	1.757s
ok  	github.com/semihalev/sdns/middleware/resolver/localroot	1.683s [no tests to run]
?   	github.com/semihalev/sdns/middleware/resolver/localroot/roottest	[no test files]
ok  	github.com/semihalev/sdns/middleware/rpz	1.695s
ok  	github.com/semihalev/sdns/middleware/views	1.703s
ok  	github.com/semihalev/sdns/server	1.752s
ok  	github.com/semihalev/sdns/server/doh	1.707s [no tests to run]
```

```sh
bash docs/check-drift.sh
```

```text
metrics
  76 registered, 76 documented, counts agree
config keys
  133 keys, index agrees in both directions
go version
  go.mod requires 1.27, pages agree
install commands
  README and installation page agree
release platforms
  installation page covers every released platform
pinned versions
  no hard-coded image tags

no drift
```

## CoreDNS build and full adapter verification

Build output excerpt (dependency resolution messages omitted):

```sh
./scripts/build-poc.sh
```

```text
recurse
Built /Users/mykolas/dev/coredns-recurse/.build/coredns
```

```sh
COREDNS_BINARY="$PWD/.build/coredns" RECURSE_LIVE_TEST=1 \
RECURSE_CONFIG="$PWD/examples/sdns.conf" RECURSE_EXPECT_AD=1 \
go test -race ./... -count=1 -v
```

```text
=== RUN   TestCoreDNSEndToEnd
--- PASS: TestCoreDNSEndToEnd (0.05s)
=== RUN   TestCoreDNSPublicRecursion
    e2e_test.go:136: udp: rcode=NOERROR ad=true answer=[example.org.	300	IN	A	172.66.157.237 example.org.	300	IN	A	104.20.26.136 example.org.	300	IN	RRSIG	A 13 2 300 20261109223545 20261107203545 34505 example.org. jGcdjKLfsr8YK1KYwCScHGuQVKpXLZDH7plBSlS/XBctGqN/rl9GYRsmhLrXOCaL6u6gsa3UZ6Qhfd170DQSug==]
    e2e_test.go:136: tcp: rcode=NOERROR ad=true answer=[example.org.	299	IN	A	172.66.157.237 example.org.	299	IN	A	104.20.26.136 example.org.	299	IN	RRSIG	A 13 2 300 20261109223545 20261107203545 34505 example.org. jGcdjKLfsr8YK1KYwCScHGuQVKpXLZDH7plBSlS/XBctGqN/rl9GYRsmhLrXOCaL6u6gsa3UZ6Qhfd170DQSug==]
--- PASS: TestCoreDNSPublicRecursion (0.69s)
=== RUN   TestCoreDNSInvalidReload
--- PASS: TestCoreDNSInvalidReload (2.07s)
=== RUN   TestSetup
=== RUN   TestSetup/duplicate_directive
=== RUN   TestSetup/missing_config_argument
=== RUN   TestSetup/legacy_block
=== RUN   TestSetup/extra_argument
=== RUN   TestSetup/missing_file
=== RUN   TestSetup/missing_root_hints
=== RUN   TestSetup/config_and_block
--- PASS: TestSetup (0.00s)
    --- PASS: TestSetup/duplicate_directive (0.00s)
    --- PASS: TestSetup/missing_config_argument (0.00s)
    --- PASS: TestSetup/legacy_block (0.00s)
    --- PASS: TestSetup/extra_argument (0.00s)
    --- PASS: TestSetup/missing_file (0.00s)
    --- PASS: TestSetup/missing_root_hints (0.00s)
    --- PASS: TestSetup/config_and_block (0.00s)
=== RUN   TestSetupSDNSConfig
--- PASS: TestSetupSDNSConfig (0.00s)
ok  	github.com/MKrupauskas/coredns-recurse	4.193s
```

The output above is an excerpt of the complete passing adapter suite, including
both deterministic binary tests and the authenticated public lookup. The build
uses the published fork module, not the local SDNS working tree.

## Manual authenticated UDP/TCP lookups

Start the sample configuration in one terminal:

```sh
./.build/coredns -conf examples/Corefile
```

Run the commands below in a second terminal. These are the captured outputs;
addresses, signatures, IDs, TTLs, and timings may change. Success means
`NOERROR`, `rd ra ad`, A records, and a DNSSEC signature. Stop with Ctrl-C.

```sh
dig @127.0.0.1 -p 1053 example.org A +dnssec +time=12 +tries=1
```

```text
; <<>> DiG 9.10.6 <<>> @127.0.0.1 -p 1053 example.org A +dnssec +time=12 +tries=1
; (1 server found)
;; global options: +cmd
;; Got answer:
;; ->>HEADER<<- opcode: QUERY, status: NOERROR, id: 18720
;; flags: qr rd ra ad; QUERY: 1, ANSWER: 3, AUTHORITY: 0, ADDITIONAL: 1

;; OPT PSEUDOSECTION:
; EDNS: version: 0, flags: do; udp: 4096
;; QUESTION SECTION:
;example.org.			IN	A

;; ANSWER SECTION:
example.org.		300	IN	A	172.66.157.237
example.org.		300	IN	A	104.20.26.136
example.org.		300	IN	RRSIG	A 13 2 300 20261109222255 20261107202255 34505 example.org. d0ZzL8Cx1H/OLpGXA/neUU9XEnZWTPOw4NJd2qI6xSKJcCn52WvPAZUk hwqgXWq5BIwPhqraFB4D6L3aOSL/Yg==

;; Query time: 738 msec
;; SERVER: 127.0.0.1#1053(127.0.0.1)
;; WHEN: Sun Nov 08 13:22:55 PST 2026
;; MSG SIZE  rcvd: 212
```

```sh
dig @127.0.0.1 -p 1053 example.org A +dnssec +tcp +time=12 +tries=1
```

```text
; <<>> DiG 9.10.6 <<>> @127.0.0.1 -p 1053 example.org A +dnssec +tcp +time=12 +tries=1
; (1 server found)
;; global options: +cmd
;; Got answer:
;; ->>HEADER<<- opcode: QUERY, status: NOERROR, id: 41502
;; flags: qr rd ra ad; QUERY: 1, ANSWER: 3, AUTHORITY: 0, ADDITIONAL: 1

;; OPT PSEUDOSECTION:
; EDNS: version: 0, flags: do; udp: 4096
;; QUESTION SECTION:
;example.org.			IN	A

;; ANSWER SECTION:
example.org.		299	IN	A	172.66.157.237
example.org.		299	IN	A	104.20.26.136
example.org.		299	IN	RRSIG	A 13 2 300 20261109222255 20261107202255 34505 example.org. d0ZzL8Cx1H/OLpGXA/neUU9XEnZWTPOw4NJd2qI6xSKJcCn52WvPAZUk hwqgXWq5BIwPhqraFB4D6L3aOSL/Yg==

;; Query time: 0 msec
;; SERVER: 127.0.0.1#1053(127.0.0.1)
;; WHEN: Sun Nov 08 13:22:55 PST 2026
;; MSG SIZE  rcvd: 212
```

## Deployment contract and limits

Keep each independently managed persistent instance's state directory separate.
The host stops query admission and cancels/drains foreground requests before
stopping SDNS. CoreDNS hooks perform that ownership transfer. Configuration
reloads create a fresh pipeline; stopped instances are not restarted.

This integration installs SDNS's resolver, cache, EDNS, failover, and forwarding
layers. CoreDNS remains responsible for listeners and additional policy plugins.
It does not install the complete standalone SDNS server, HTTP API, or every
optional SDNS middleware. Public DNS verification is opt-in; the required CI
checks stay local and deterministic.
