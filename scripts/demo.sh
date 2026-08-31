#!/usr/bin/env bash
set -euo pipefail

repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
state_dir=${HERDLORD_DEMO_DIR:-${repo_dir}/.herdlord-test/demo}
config_path=${state_dir}/targets.json
owner_path=${state_dir}/owned
binary=${repo_dir}/bin/herdlord
sessions=(herdlord-demo-a herdlord-demo-b)
offline_session=herdlord-demo-offline

usage() {
    printf 'usage: %s {up|run|status|down|attach|stop|start|toggle} [a|b]\n' "$0" >&2
    exit 2
}

session_name() {
    case ${1:-} in
        a|herdlord-demo-a) printf '%s\n' herdlord-demo-a ;;
        b|herdlord-demo-b) printf '%s\n' herdlord-demo-b ;;
        *) printf 'session must be a or b\n' >&2; exit 2 ;;
    esac
}

herdr_for() {
    local name=$1
    shift
    env -u HERDR_SOCKET_PATH -u HERDR_CLIENT_SOCKET_PATH HERDR_SESSION="$name" herdr "$@"
}

session_exists() {
    herdr session list --json | grep -Fq "\"name\":\"$1\""
}

session_running() {
    local output
    output=$(herdr_for "$1" status server 2>/dev/null) || return 1
    [[ $output == *"status: running"* ]]
}

require_tools() {
    command -v herdr >/dev/null || { printf 'herdr is not installed\n' >&2; exit 1; }
    [[ -x $binary ]] || { printf 'build Herdlord first with just build\n' >&2; exit 1; }
}

claim_sessions() {
    if [[ -f $owner_path ]]; then
        return
    fi
    local name
    for name in "${sessions[@]}" "$offline_session"; do
        if session_exists "$name"; then
            printf 'refusing to use existing Herdr session %q\n' "$name" >&2
            printf 'delete or rename it, or set HERDLORD_DEMO_DIR to an existing owned demo\n' >&2
            exit 1
        fi
    done
    mkdir -p "$state_dir"
    printf '%s\n' "${sessions[@]}" "$offline_session" >"$owner_path"
}

start_session() {
    local name=$1
    if session_running "$name"; then
        return
    fi
    nohup env -u HERDR_SOCKET_PATH -u HERDR_CLIENT_SOCKET_PATH \
        HERDR_SESSION="$name" herdr server >>"${state_dir}/${name}.log" 2>&1 &
    local _
    for _ in {1..50}; do
        if session_running "$name"; then
            return
        fi
        sleep 0.1
    done
    printf 'Herdr session %q did not start; see %s\n' "$name" "${state_dir}/${name}.log" >&2
    exit 1
}

configure_targets() {
    if [[ -f $config_path ]]; then
        "$binary" --config "$config_path" targets list >/dev/null
        return
    fi
    "$binary" --config "$config_path" targets add local-a \
        --prefix 'env -u HERDR_SOCKET_PATH -u HERDR_CLIENT_SOCKET_PATH HERDR_SESSION=herdlord-demo-a'
    "$binary" --config "$config_path" targets add local-b \
        --prefix 'env -u HERDR_SOCKET_PATH -u HERDR_CLIENT_SOCKET_PATH HERDR_SESSION=herdlord-demo-b'
    "$binary" --config "$config_path" targets add offline \
        --prefix 'env -u HERDR_SOCKET_PATH -u HERDR_CLIENT_SOCKET_PATH HERDR_SESSION=herdlord-demo-offline'
}

up() {
    require_tools
    claim_sessions
    local name
    for name in "${sessions[@]}"; do
        start_session "$name"
    done
    configure_targets
    printf 'Demo ready.\nConfig: %s\n\n' "$config_path"
    printf 'Open a session with: just demo-session a\n'
    printf 'Open Herdlord with:  just demo\n'
}

run() {
    require_tools
    [[ -f $owner_path && -f $config_path ]] || { printf 'run just demo or just demo-session a first\n' >&2; exit 1; }
    exec "$binary" --config "$config_path" --interval 500ms --timeout 3s
}

status() {
    require_tools
    printf 'Demo directory: %s\n' "$state_dir"
    if [[ ! -f $owner_path ]]; then
        printf 'State: not set up\n'
        return
    fi
    printf '\nHerdr sessions:\n'
    herdr session list
    printf '\nHerdlord targets:\n'
    "$binary" --config "$config_path" targets list
}

down() {
    require_tools
    if [[ ! -f $owner_path ]]; then
        printf 'No owned demo exists at %s\n' "$state_dir"
        return
    fi
    local name
    local failed=0
    while IFS= read -r name; do
        [[ -n $name ]] || continue
        herdr session stop "$name" >/dev/null 2>&1 || true
        herdr session delete "$name" >/dev/null 2>&1 || true
        if session_exists "$name"; then
            printf 'could not delete Herdr session %q\n' "$name" >&2
            failed=1
        fi
    done <"$owner_path"
    if ((failed)); then
        printf 'Ownership state retained at %s\n' "$owner_path" >&2
        exit 1
    fi
    rm -f "$config_path" "${config_path}.lock" "$owner_path" "${state_dir}"/*.log
    rmdir "$state_dir" 2>/dev/null || true
    printf 'Removed demo sessions and state from %s\n' "$state_dir"
}

attach() {
    local name
    name=$(session_name "${1:-}")
    [[ -f $owner_path ]] || { printf 'run just demo or just demo-session a first\n' >&2; exit 1; }
    exec herdr session attach "$name"
}

stop() {
    local name
    name=$(session_name "${1:-}")
    [[ -f $owner_path ]] || { printf 'run just demo or just demo-session a first\n' >&2; exit 1; }
    herdr session stop "$name"
    printf 'Stopped %s. Herdlord will show its backoff state.\n' "$name"
}

start() {
    require_tools
    [[ -f $owner_path ]] || { printf 'run just demo or just demo-session a first\n' >&2; exit 1; }
    start_session "$(session_name "${1:-}")"
}

toggle() {
    require_tools
    [[ -f $owner_path ]] || { printf 'run just demo or just demo-session a first\n' >&2; exit 1; }
    local name
    name=$(session_name "${1:-}")
    if session_running "$name"; then
        stop "$name"
    else
        start_session "$name"
        printf 'Started %s. Herdlord will recover on its next poll.\n' "$name"
    fi
}

case ${1:-} in
    up) up ;;
    run) run ;;
    status) status ;;
    down) down ;;
    attach) attach "${2:-}" ;;
    stop) stop "${2:-}" ;;
    start) start "${2:-}" ;;
    toggle) toggle "${2:-}" ;;
    *) usage ;;
esac
