# recurse

`recurse` is an **external CoreDNS plugin stub** for a future recursive resolver.
It follows the root-package layout and registration/setup pattern of the
[canonical CoreDNS example](https://github.com/coredns/example).

**Recursion is not implemented. Every query reaching the default handler returns
SERVFAIL, with an explicit unimplemented error and no answer records.** There is
no SDNS integration, upstream forwarding, DNSSEC validation, or production resolver.

## Syntax

```corefile
recurse
```

No arguments or block options are supported. Only one instance is allowed per
server block. This is a terminal handler: it does not call the next plugin.

## Build with CoreDNS

Verified baseline: CoreDNS v1.13.1, Go 1.25.3 (module minimum: Go 1.24).
This package is compiled into CoreDNS; it is not a standalone server or a
runtime-loaded shared library.

```sh
git clone --branch v1.13.1 --depth 1 https://github.com/coredns/coredns.git
cd coredns
```

In **CoreDNS's** `plugin.cfg`, insert this line immediately before `forward:forward`:

```text
recurse:github.com/MKrupauskas/coredns-recurse
```

Order in `plugin.cfg` determines execution order, regardless of Corefile order.
Place response-processing plugins such as `errors` and `cache` before `recurse`.
For the stub demo, omit other answering plugins.

```sh
go get github.com/MKrupauskas/coredns-recurse@main
go generate
go build -o coredns .
./coredns -plugins
```

Confirm `recurse` appears in the plugin list. For reproducible builds, replace
`@main` with a chosen commit SHA. See the [CoreDNS build documentation](https://coredns.io/2017/07/25/compile-time-enabling-or-disabling-plugins/).

Example `Corefile` (local testing):

```corefile
.:1053 {
    bind 127.0.0.1
    errors
    recurse
}
```

```sh
./coredns -conf Corefile
dig @127.0.0.1 -p 1053 example.org A
```

Expected result: `status: SERVFAIL`, zero answers. The `errors` plugin logs
`recursive resolution is not implemented`. The stub makes no outbound DNS queries.

## Development

```sh
gofmt -w *.go
go test ./...
```

`Recurse.Engine` is an injectable `Engine` interface for future resolver work and
unit tests. The default engine returns `ErrUnimplemented`; the zero-value handler
has the same behavior. Engine errors and nil responses fail closed with SERVFAIL.
The handler follows CoreDNS's return-code convention: failure codes are returned
without writing, allowing CoreDNS to send the error response once.

No lifecycle hooks are needed yet: the stub owns no goroutines, sockets, or other
resources. Add startup/shutdown handling when a real engine needs it. Tests cover
setup validation, default failure behavior, and engine injection. CI also builds
CoreDNS with the external plugin wired through `plugin.cfg`.
