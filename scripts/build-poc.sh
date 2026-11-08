#!/usr/bin/env bash
set -euo pipefail

plugin_dir=$(cd "$(dirname "$0")/.." && pwd)
coredns_dir=$(cd "${1:-$plugin_dir/../coredns}" && pwd)
build_dir="$plugin_dir/.build"

# Go does not inherit a dependency module's replacements into the build host.
sdns_module=$(cd "$plugin_dir" && go list -m -f '{{with .Replace}}{{.Path}}{{with .Version}}@{{.}}{{end}}{{end}}' github.com/semihalev/sdns)
if [ "$#" -ge 2 ]; then
    sdns_module=$(cd "$2" && pwd)
fi

# Work in a copy: do not change the user's CoreDNS checkout or plugin list.
mkdir -p "$build_dir/coredns-src"
git -C "$coredns_dir" archive HEAD | tar -xf - -C "$build_dir/coredns-src"
cd "$build_dir/coredns-src"
go mod edit -replace "github.com/MKrupauskas/coredns-recurse=$plugin_dir"
if [ -n "$sdns_module" ]; then
    go mod edit -replace "github.com/semihalev/sdns=$sdns_module"
fi
go get github.com/MKrupauskas/coredns-recurse

# Keep CoreDNS's configured plugin order and insert our external plugin.
awk '/^recurse:/ { next } /^forward:forward$/ { print "recurse:github.com/MKrupauskas/coredns-recurse" } { print }' plugin.cfg > plugin.cfg.tmp
mv plugin.cfg.tmp plugin.cfg
go generate
go mod tidy
go build -o "$build_dir/coredns" .
"$build_dir/coredns" -plugins | grep -x recurse
echo "Built $build_dir/coredns"
