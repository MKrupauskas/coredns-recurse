# POC test plan and recorded results

Recorded on 2026-11-08 on macOS arm64 with Go 1.27.2. Commands below run
from the `coredns-recurse` checkout unless they explicitly change directory.
The examples require the sibling SDNS checkout with `patches/sdns-embedded.patch`
applied. CoreDNS source is pinned in CI to
`f79be2a2f8266c2881d01f4c884149e8067569c9`; SDNS's base is
`2226f31b50fd1f146f452165acdcd7520213ea8f`.

## Automated coverage

| Check | Automation | What it proves |
| --- | --- | --- |
| Plugin tests with `-race` | Plugin PR CI | A/NXDOMAIN replies, fail-closed errors, input validation, preservation of the original request, request deadlines, cancellation, concurrent requests, and independent resolver instances. |
| `TestEmbeddedReferralWalk` with `-race` | Plugin PR CI and SDNS's existing `make test` CI | SDNS follows a synthetic root referral, resolves an out-of-bailiwick nameserver through the isolated queryer, and returns `192.0.2.42` without enabling enrichment lanes or changing global pipeline readiness. |
| Compile CoreDNS with external plugin | Plugin PR CI | Real `plugin.cfg` registration, module replacements, code generation, full CoreDNS build, and `recurse` in `-plugins`. |
| `TestCoreDNSEndToEnd` | Plugin PR CI | A real CoreDNS child process serves A requests over UDP and TCP through the plugin and embedded SDNS against an unprivileged local authoritative fixture; graceful shutdown also succeeds. |
| `TestCoreDNSPublicRecursion` | Explicit opt-in automated test | A fresh CoreDNS process starts from public root hints and returns A records for `example.org` over UDP and TCP. Requires outbound DNS access. |

The deterministic binary fixture answers authoritatively at the configured test
root. It tests the complete client/server integration; referral walking is
covered separately by the SDNS fixture and the public-recursion test. Default
`go test` skips binary tests unless `COREDNS_BINARY` is set, so CI builds the
binary and explicitly runs the deterministic binary test afterward. Public DNS
is not a required CI dependency.

## Plugin race suite

```sh
go test -race ./... -count=1
```

```text
ok  	github.com/MKrupauskas/coredns-recurse	1.523s
```

## SDNS resolver regressions

The full resolver package passed with the race detector:

```sh
(cd ../sdns && go test -race ./middleware/resolver -count=1)
```

```text
ok  	github.com/semihalev/sdns/middleware/resolver	54.834s
```

Focused handler, hermetic-resolution, and embedding regressions:

```sh
(cd ../sdns && go test -race ./middleware/resolver -run 'Test(Embedded|HermeticResolve|Handler)' -count=1)
```

```text
ok  	github.com/semihalev/sdns/middleware/resolver	1.957s
```

The embedding-specific run below also uses the race detector. Output excerpt
(the fixture debug logs are omitted):

```sh
(cd ../sdns && go test -race ./middleware/resolver -run '^TestEmbeddedReferralWalk$' -count=1 -v)
```

```text
=== RUN   TestEmbeddedReferralWalk
--- PASS: TestEmbeddedReferralWalk (0.05s)
PASS
ok  	github.com/semihalev/sdns/middleware/resolver	1.280s
```

## Build and deterministic binary smoke test

Build output excerpt (dependency upgrade messages omitted):

```sh
./scripts/build-poc.sh
```

```text
recurse
Built /Users/mykolas/dev/coredns-recurse/.build/coredns
```

```sh
COREDNS_BINARY="$PWD/.build/coredns" go test -run '^TestCoreDNSEndToEnd$' -count=1 -v
```

```text
=== RUN   TestCoreDNSEndToEnd
--- PASS: TestCoreDNSEndToEnd (0.05s)
PASS
ok  	github.com/MKrupauskas/coredns-recurse	0.257s
```

## Public recursive lookup

```sh
COREDNS_BINARY="$PWD/.build/coredns" RECURSE_LIVE_TEST=1 go test -run '^TestCoreDNSPublicRecursion$' -count=1 -v
```

```text
=== RUN   TestCoreDNSPublicRecursion
    e2e_test.go:127: udp: [example.org.	300	IN	A	104.20.26.136 example.org.	300	IN	A	172.66.157.237]
    e2e_test.go:127: tcp: [example.org.	300	IN	A	172.66.157.237 example.org.	300	IN	A	104.20.26.136]
--- PASS: TestCoreDNSPublicRecursion (0.20s)
PASS
ok  	github.com/MKrupauskas/coredns-recurse	0.453s
```

## Manual UDP and TCP confirmation

Start the server in one terminal:

```sh
./.build/coredns -conf examples/Corefile
```

The commands below ran in a second terminal. Recorded addresses, ordering,
request IDs, TTLs, and timing can change; success means `NOERROR`, `rd ra`, and
an A answer. Stop the server with Ctrl-C afterward.

```sh
dig @127.0.0.1 -p 1053 example.org A +time=12 +tries=1
```

```text
; <<>> DiG 9.10.6 <<>> @127.0.0.1 -p 1053 example.org A +time=12 +tries=1
; (1 server found)
;; global options: +cmd
;; Got answer:
;; ->>HEADER<<- opcode: QUERY, status: NOERROR, id: 15717
;; flags: qr rd ra; QUERY: 1, ANSWER: 2, AUTHORITY: 0, ADDITIONAL: 1

;; OPT PSEUDOSECTION:
; EDNS: version: 0, flags:; udp: 4096
;; QUESTION SECTION:
;example.org.			IN	A

;; ANSWER SECTION:
example.org.		300	IN	A	172.66.157.237
example.org.		300	IN	A	104.20.26.136

;; Query time: 418 msec
;; SERVER: 127.0.0.1#1053(127.0.0.1)
;; WHEN: Sun Nov 08 12:51:08 PST 2026
;; MSG SIZE  rcvd: 94
```

```sh
dig @127.0.0.1 -p 1053 example.org A +tcp +time=12 +tries=1
```

```text
; <<>> DiG 9.10.6 <<>> @127.0.0.1 -p 1053 example.org A +tcp +time=12 +tries=1
; (1 server found)
;; global options: +cmd
;; Got answer:
;; ->>HEADER<<- opcode: QUERY, status: NOERROR, id: 38471
;; flags: qr rd ra; QUERY: 1, ANSWER: 2, AUTHORITY: 0, ADDITIONAL: 1

;; OPT PSEUDOSECTION:
; EDNS: version: 0, flags:; udp: 4096
;; QUESTION SECTION:
;example.org.			IN	A

;; ANSWER SECTION:
example.org.		300	IN	A	172.66.157.237
example.org.		300	IN	A	104.20.26.136

;; Query time: 10 msec
;; SERVER: 127.0.0.1#1053(127.0.0.1)
;; WHEN: Sun Nov 08 12:51:08 PST 2026
;; MSG SIZE  rcvd: 94
```

## Reproducibility and scope

The packaged SDNS patch was checked against a clean archive of its pinned base
and reverse-checked against the modified checkout. Formatting and
`git diff --check` passed in both repositories.

This POC covers direct A records using IPv4 authorities with DNSSEC disabled.
CNAME target completion, answer caching, full EDNS policy/truncation, IPv6
upstreams, and long-lived maintenance are follow-up work. No full SDNS module
suite, allocation gate, cross-platform build matrix, or lint pass is implied by
the local commands recorded here; those are covered by SDNS's existing CI.
