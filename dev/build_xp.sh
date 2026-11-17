#!/usr/bin/env bash
# Reproduces ShuttleBuilder's Windows XP build (win_i386): Go 1.10.8 in GOPATH
# mode, GOOS=windows GOARCH=386. The repo is mounted read-only at /src, which is
# outside GOPATH.
#
# Usage: dev/build_xp.sh [--offline] [repo_dir] [out_dir]
#   repo_dir  defaults to the repo root
#   out_dir   defaults to a fresh mktemp -d
#   --offline mount the go.mod-pinned gowebdav from the local module cache
#             instead of running "go get" (no network needed)
set -euo pipefail

IMAGE="golang:1.10.8"
GOWEBDAV_PIN="v0.0.0-20220901075112-8721ee532c0c"

offline=0
args=()
for a in "$@"; do
    case "$a" in
        --offline) offline=1 ;;
        -h|--help) sed -n '2,10p' "$0"; exit 0 ;;
        *) args+=("$a") ;;
    esac
done

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_dir="${args[0]:-$(dirname "$script_dir")}"
repo_dir="$(cd "$repo_dir" && pwd)"
out_dir="${args[1]:-$(mktemp -d)}"
mkdir -p "$out_dir"
out_dir="$(cd "$out_dir" && pwd)"

docker_args=(run --rm -v "$repo_dir:/src:ro" -v "$out_dir:/out")
fetch='go get -d github.com/StarmanMartin/gowebdav'

if [ "$offline" = 1 ]; then
    modcache="$(go env GOMODCACHE)"
    pinned="$modcache/github.com/!starman!martin/gowebdav@$GOWEBDAV_PIN"
    if [ ! -d "$pinned" ]; then
        echo "build_xp.sh: pinned gowebdav not found: $pinned" >&2
        echo "  (run 'go mod download' with a modern Go first)" >&2
        exit 2
    fi
    docker_args+=(-v "$pinned:/go/src/github.com/StarmanMartin/gowebdav:ro")
    fetch='echo "offline: using mounted gowebdav"'
fi

# Runs as root inside the container; the exe is chowned back to the caller.
inner="set -e
$fetch
cd /src
go version
GOOS=windows GOARCH=386 go vet .
GOOS=windows GOARCH=386 go build -o /out/shuttle.exe .
chown $(id -u):$(id -g) /out/shuttle.exe"

echo "build_xp.sh: $IMAGE, source $repo_dir" >&2
if ! docker "${docker_args[@]}" "$IMAGE" sh -c "$inner"; then
    echo "build_xp.sh: XP build FAILED" >&2
    exit 1
fi
echo "$out_dir/shuttle.exe"
