binary := "herdlord"
bin_dir := "bin"

default:
    @just --list

dev: check

build:
    mkdir -p {{bin_dir}}
    go build -o {{bin_dir}}/{{binary}} ./cmd/{{binary}}

docs:
    go run ./cmd/gen-docs

docs-check:
    scripts/check-docs.sh

test:
    go test ./...

race:
    go test -race ./...

vet:
    go vet ./...

lint:
    golangci-lint run
    actionlint

tidy-check:
    go mod tidy -diff

scripts-check:
    @if ls scripts/*.sh >/dev/null 2>&1; then bash -n scripts/*.sh; fi

skill-check:
    go run ./cmd/skill-check

smoke: build
    ./{{bin_dir}}/{{binary}} --version
    ./{{bin_dir}}/{{binary}} --help

demo: build
    scripts/demo.sh up
    scripts/demo.sh run

demo-session session: build
    scripts/demo.sh up
    scripts/demo.sh attach '{{session}}'

demo-toggle session: build
    scripts/demo.sh toggle '{{session}}'

demo-down: build
    scripts/demo.sh down

release-snapshot-check:
    goreleaser check
    goreleaser release --snapshot --clean --skip=publish
    scripts/verify-release-archives.sh dist
    scripts/smoke-release.sh dist

nix-check:
    nix flake check --show-trace
    nix build .#herdlord
    test -x result/bin/herdlord
    nix run .#herdlord -- --version

check: docs-check skill-check vet test lint tidy-check scripts-check smoke
    goreleaser check

ci: check release-snapshot-check nix-check

release-prep version:
    scripts/check-release-notes.sh '{{version}}'
    just check
    nix flake check --show-trace
    just release-snapshot-check
    @printf '\nRelease prep passed for {{version}}.\n'
    @printf 'Publish with:\n'
    @printf '  git push origin main\n'
    @printf '  git tag -a {{version}} -m "{{version}}"\n'
    @printf '  git push origin {{version}}\n'

clean:
    rm -rf {{bin_dir}} dist result
