# `deploy/` — example ops files

This directory contains working examples for running riftapi in
production. **They are starting points, not the only way to
deploy.** Pick the ones that fit your setup, modify freely, ignore
the rest.

| File | Purpose |
|---|---|
| `systemd/riftapi-sync.service` | One-shot service that runs the sync binary (typically invoked by the timer). |
| `systemd/riftapi-sync.timer` | Daily schedule (03:00 by default) that triggers the service. |
| `caddy/Caddyfile` | TLS-terminating reverse proxy in front of the API. |
| `backup/backup.sh` | Daily online backup of the SQLite file with 7-day rolling retention. |

## Picking what to use

- **If you run the API on a single Linux host** — copy the systemd
  units to `/etc/systemd/system/`, put `backup.sh` in cron, run
  Caddy (or nginx, or your preferred reverse proxy) on the host.
  The two systemd units together give you a nightly sync without
  the rest of the rig.
- **If you run the API in Docker** — `docker compose up -d api`
  is the long-running process; the sync is a one-shot container
  you trigger from any scheduler (cron on the host, a
  Kubernetes CronJob, a GitHub Actions schedule, etc.). The
  systemd files and the Caddyfile are still useful as a model;
  the `backup.sh` script works against any path you can reach.
- **If you run the API in Kubernetes** — the API is a stateless
  Deployment; the sync is a CronJob. The `backup.sh` script can
  run from a CronJob too, with a `hostPath` or PVC for the
  destination. The systemd files and Caddyfile are not directly
  applicable.
- **If you run the API on your laptop for development** — skip
  this directory entirely.

## Configuring the example files

The example files use placeholder paths and names. Adjust these
before deploying:

| Placeholder | Replace with |
|---|---|
| `/opt/riftapi` | The directory where you cloned this repo. |
| `/etc/riftapi/riftapi.env` | The path to your env file (see `riftapi.example.env` at the repo root). |
| `docker` | The path to your Docker CLI (usually `/usr/bin/docker`). |
| `riftapi` (user/group) | The user that owns the riftapi directory. |
| `riftapi.lan` | The hostname the API is served on (or your public domain). |
| `/data/riftapi.db` | The path to your SQLite file (must match `RIFTAPI_DATABASE_PATH`). |
| `/var/backups/riftapi/` | A directory you create, writable by the backup cron job. |
| `sqlite3` | The path to your `sqlite3` binary. |

## Why systemd for the sync, not the API?

The API is long-running and is best handled by whatever your
platform's long-running process manager is (systemd, Docker,
Kubernetes). The sync is one-shot and runs on a schedule, which
matches systemd timer semantics nicely. If you prefer, you can
run the sync from cron with the same `riftapi-sync` invocation
that the systemd service uses — the example service file is just
the most portable scheduler.

## Health checks

The `docker-compose.yml` at the repo root has a Docker healthcheck
that calls `riftapi --healthcheck`, which probes the local
`/health` endpoint and exits 0/1 accordingly. The same approach
works in any other environment: have your supervisor (Docker,
Kubernetes, systemd) periodically run `riftapi --healthcheck` (or
`curl --fail http://localhost:8080/health`) and restart on
failure.
