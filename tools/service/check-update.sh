#!/bin/sh
# Checks bam-update (README §2.2) in a Debian container booted with systemd,
# against fake releases served from files: it updates to a newer release,
# leaves an up-to-date or newer installation alone, refuses a download that
# does not match its checksum, rolls back a release that does not come up
# along with the changes it made to the database, refuses to update a
# database it cannot back up, runs backup hooks, keeps only the newest
# backups, and replaces a build that is not a release only when forced.
# Needs podman.
set -eu

DEBIAN_IMAGE="docker.io/library/debian:bookworm-slim@sha256:a4672c0cb26fbdde88e38fa2dfb6c681942306680e41e4378b28770b6e79ee91"
GO_IMAGE="docker.io/library/golang@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
NAME=bam-update-check
export MSYS_NO_PATHCONV=1

echo "== building releases v0.0.1, v0.0.2 and a dev build"
for v in v0.0.1 v0.0.2 dev; do
  podman run --rm -v "$ROOT:/src" -v bam-gocache:/root/.cache/go-build -v bam-gomod:/go/pkg/mod -w /src \
    -e CGO_ENABLED=0 "$GO_IMAGE" \
    go build -trimpath -ldflags "-X github.com/Cambridge-Hackspace/boring-asset-manager/internal/api.Version=$v" \
    -o "bin/check/bam-$v" ./cmd/bam
done

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

echo "== installing v0.0.1, the service and the updater"
run '
set -e
cd /src
install -m 0755 bin/check/bam-v0.0.1 /usr/local/bin/bam
install -D -m 0644 packaging/linux/bam.sysusers /etc/sysusers.d/bam.conf
systemd-sysusers >/dev/null
install -d -m 0750 -o root -g bam /etc/bam
install -m 0640 -o root -g bam packaging/linux/config.example.toml /etc/bam/config.toml
install -m 0644 packaging/linux/bam.service /etc/systemd/system/bam.service
systemctl daemon-reload
systemctl enable --now bam 2>/dev/null
install -m 0755 packaging/bam-update /usr/local/sbin/bam-update
install -m 0644 packaging/linux/bam-update.cron /etc/cron.d/bam-update
printf "%s\n" BAM_UPDATE_API_URL=file:///srv/release/latest.json BAM_UPDATE_DOWNLOAD_URL=file:///srv/release/download BAM_UPDATE_KEEP_BACKUPS=2 > /etc/bam/update.conf
# A real binary for the broken release below to hand all but serving to.
install -D -m 0755 bin/check/bam-v0.0.2 /usr/local/libexec/bam-real
'

# publish TAG BINARY [BAD]: makes TAG the latest release, with BINARY as its
# Linux amd64 asset; BAD publishes a checksum that does not match.
publish() {
  run "
set -e
d=/srv/release/download/$1
mkdir -p \$d
cp $2 \$d/bam-linux-amd64
(cd \$d && sha256sum bam-linux-amd64 > SHA256SUMS)
[ -z '${3:-}' ] || sed -i 's/^./0/' \$d/SHA256SUMS
printf '{\"url\":\"x\",\"tag_name\":\"%s\",\"name\":\"%s\"}' $1 $1 > /srv/release/latest.json
"
}
pid() { run "systemctl show -p MainPID --value bam"; }
version() { run "bam version"; }
expect() {
  if [ "$1" != "$2" ]; then echo "FAIL: $3: got '$1', want '$2'" >&2; exit 1; fi
  echo "ok: $3"
}
healthy() {
  for _ in $(seq 1 30); do
    run "curl -fsS http://127.0.0.1:8080/healthz" >/dev/null 2>&1 && return 0
    sleep 1
  done
  echo "FAIL: not healthy" >&2
  exit 1
}
healthy

echo "== a newer release"
publish v0.0.2 /src/bin/check/bam-v0.0.2
run "bam-update --check"
before=$(pid)
run "bam-update"
expect "$(version)" "BAM! version v0.0.2" "updated to v0.0.2"
healthy
[ "$(pid)" != "$before" ] && echo "ok: the service was restarted"
run "test -x /usr/local/bin/bam.previous && /usr/local/bin/bam.previous version" | grep -q v0.0.1 && echo "ok: v0.0.1 kept as bam.previous"

echo "== nothing newer"
before=$(pid)
run "bam-update"
expect "$(pid)" "$before" "an up-to-date installation is left running"
publish v0.0.1 /src/bin/check/bam-v0.0.1
run "bam-update"
expect "$(version)" "BAM! version v0.0.2" "an older latest release is ignored"

echo "== a download that does not match its checksum"
publish v0.0.3 /src/bin/check/bam-v0.0.2 bad
if run "bam-update" 2>&1; then echo "FAIL: accepted a bad checksum" >&2; exit 1; fi
expect "$(version)" "BAM! version v0.0.2" "the bad download was refused"
expect "$(pid)" "$before" "and the service left running"

echo "== a release that does not come up"
# It changes the database (adding a user) before failing to serve, as a
# release whose migrations ran and which then broke would.
run "cat > /tmp/broken <<'EOF'
#!/bin/sh
case \"\$1\" in
  version) echo 'BAM! version v0.0.4' ;;
  serve)
    /usr/local/libexec/bam-real admin create --config /etc/bam/config.toml --email upgraded@check.test --password 'Marker123!' >/dev/null
    exit 1 ;;
  *) exec /usr/local/libexec/bam-real \"\$@\" ;;
esac
EOF
chmod +x /tmp/broken"
publish v0.0.4 /tmp/broken
if run "bam-update" 2>&1; then echo "FAIL: kept a release that does not start" >&2; exit 1; fi
expect "$(version)" "BAM! version v0.0.2" "rolled back to v0.0.2"
healthy
echo "ok: v0.0.2 is serving again"
# The user the broken release added went with the restore, so it cannot
# sign in.
signin=$(run "curl -s -o /dev/null -w %{http_code} -H 'Content-Type: application/json' -d '{\"email\":\"upgraded@check.test\",\"password\":\"Marker123!\"}' http://127.0.0.1:8080/api/v1/auth/login")
if [ "$signin" != 401 ]; then
  echo "FAIL: signing in as the user the broken release added: $signin" >&2
  exit 1
fi
echo "ok: the database is as it was before the update"
expect "$(run "stat -c %a /var/backups/bam")" "700" "backups are readable by root only"
expect "$(run "stat -c %U /var/lib/bam/bam.db")" "bam" "the restored database still belongs to bam"

echo "== a database that cannot be backed up, and backup hooks"
run "sed 's/^type = .*/type = \"postgres\"/' /etc/bam/config.toml > /tmp/pg.toml"
if out=$(run "BAM_UPDATE_CONFIG=/tmp/pg.toml bam-update" 2>&1); then echo "FAIL: updated without a backup" >&2; exit 1; fi
if ! echo "$out" | grep -q "BAM_UPDATE_BACKUP_COMMAND"; then echo "FAIL: no word on how to back it up: $out" >&2; exit 1; fi
echo "ok: says how to back it up"
expect "$(version)" "BAM! version v0.0.2" "and changed nothing"
healthy
hooks="BAM_UPDATE_CONFIG=/tmp/pg.toml BAM_UPDATE_BACKUP_COMMAND='echo dumped > \"\$BAM_BACKUP_FILE\"'"
if run "$hooks bam-update" 2>&1; then echo "FAIL: a backup command without a restore command" >&2; exit 1; fi
if run "$hooks BAM_UPDATE_RESTORE_COMMAND='cp \"\$BAM_BACKUP_FILE\" /tmp/restored' bam-update" 2>&1; then
  echo "FAIL: kept a release that does not start" >&2
  exit 1
fi
expect "$(run "cat /tmp/restored")" "dumped" "the hooks backed up and restored"
healthy

echo "== a build that is not a release"
run "install -m 0755 /src/bin/check/bam-dev /usr/local/bin/bam && systemctl restart bam"
publish v0.0.2 /src/bin/check/bam-v0.0.2
if run "bam-update" 2>&1; then echo "FAIL: replaced a dev build unasked" >&2; exit 1; fi
expect "$(version)" "BAM! version dev" "a dev build is left alone"
run "bam-update --force"
expect "$(version)" "BAM! version v0.0.2" "and replaced when forced"
healthy
expect "$(run "ls /var/backups/bam | wc -l")" "2" "only the newest two backups are kept"
echo "PASS"
