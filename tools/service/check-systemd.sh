#!/bin/sh
# Installs BAM in a Debian container booted with systemd, following the
# steps in README §2.1, and checks the service: it starts at boot, serves,
# comes back after it is killed and after a reboot, and stops cleanly.
# Needs bin/bam-linux-amd64 (tools/release/build.sh linux/amd64) and
# podman.
set -eu

DEBIAN_IMAGE="docker.io/library/debian:bookworm-slim@sha256:a4672c0cb26fbdde88e38fa2dfb6c681942306680e41e4378b28770b6e79ee91"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
NAME=bam-systemd-check
export MSYS_NO_PATHCONV=1

[ -x "$ROOT/bin/bam-linux-amd64" ] || [ -f "$ROOT/bin/bam-linux-amd64" ] || {
  echo "bin/bam-linux-amd64 is missing: run tools/release/build.sh linux/amd64" >&2
  exit 1
}

podman build -q -t bam-systemd-check - >/dev/null <<CONTAINERFILE
FROM $DEBIAN_IMAGE
RUN apt-get update && apt-get install -y --no-install-recommends systemd systemd-sysv curl ca-certificates && rm -rf /var/lib/apt/lists/*
CMD ["/lib/systemd/systemd"]
CONTAINERFILE

podman rm -f "$NAME" >/dev/null 2>&1 || true
trap 'podman rm -f "$NAME" >/dev/null 2>&1 || true' EXIT
podman run -d --name "$NAME" --systemd=always -v "$ROOT:/src:ro" bam-systemd-check >/dev/null

run() { podman exec "$NAME" sh -c "$1"; }
until run "systemctl is-system-running 2>/dev/null | grep -qE 'running|degraded'"; do sleep 1; done

echo "== installing, as README §2.1 says"
run '
set -e
cd /src
install -m 0755 bin/bam-linux-amd64 /usr/local/bin/bam
install -D -m 0644 packaging/linux/bam.sysusers /etc/sysusers.d/bam.conf
systemd-sysusers
install -d -m 0750 -o root -g bam /etc/bam
install -m 0640 -o root -g bam packaging/linux/config.example.toml /etc/bam/config.toml
install -m 0644 packaging/linux/bam.service /etc/systemd/system/bam.service
systemctl daemon-reload
systemctl enable --now bam
runuser -u bam -- bam admin create --config /etc/bam/config.toml --email admin@example.org --password "Initial-Pass-1"
'

healthy() {
  for i in $(seq 1 30); do
    run "curl -fsS http://127.0.0.1:8080/healthz" >/dev/null 2>&1 && return 0
    sleep 1
  done
  run "systemctl status bam --no-pager; journalctl -u bam --no-pager | tail -20"
  echo "FAIL: $1" >&2
  exit 1
}
mainpid() { run "systemctl show -p MainPID --value bam"; }

healthy "the service did not start"
echo "ok: running as $(run 'stat -c %U /proc/$(systemctl show -p MainPID --value bam)')"
run "curl -fsS -o /dev/null -X POST -H 'Content-Type: application/json' -d '{\"email\":\"admin@example.org\",\"password\":\"Initial-Pass-1\"}' http://127.0.0.1:8080/api/v1/auth/login" \
  && echo "ok: the administrator can sign in"
run "stat -c '%U:%G %a %n' /var/lib/bam /var/lib/bam/bam.db /etc/bam/config.toml"

echo "== verifying the unit"
run "systemd-analyze verify /etc/systemd/system/bam.service"
run "systemd-analyze security bam --no-pager | tail -1"

echo "== killing it"
before=$(mainpid)
run "kill -9 $before"
sleep 7
healthy "the service did not come back after being killed"
[ "$(mainpid)" != "$before" ] && echo "ok: restarted after being killed"

echo "== rebooting"
podman restart "$NAME" >/dev/null
until run "systemctl is-system-running 2>/dev/null | grep -qE 'running|degraded'"; do sleep 1; done
healthy "the service did not start at boot"
echo "ok: started at boot"

echo "== stopping"
run "systemctl stop bam"
run "journalctl -u bam --no-pager | grep -q 'stopped cleanly'" && echo "ok: stopped cleanly"
echo "PASS"
