# Shipyard

Shipyard is an open-source CLI tool for deploying and managing Docker containers from images or Git repositories with minimal configuration.

It provides a simple workflow for running services locally or behind a reverse proxy (Traefik), with built-in lifecycle management and deployment tracking.

---

## Features

* Deploy Docker images directly from registries
* Deploy applications from Git repositories
* Traefik integration (automatic routing + networks)
* Local mode (direct port exposure)
* Volume mounting support
* Environment variables (`--env`) and env files (`--env-file`)
* Docker build arguments and configurable build platform
* Traefik entrypoints, TLS, cert resolvers and middlewares
* Optional container healthchecks, verified on deploy and shown in `status`
* Persistent per-service `deployment.yaml`, reused by `update`
* Safe updates: failed builds never touch the running deployment; failed starts restore the previous configuration
* Service lifecycle management (start, stop, restart, logs, update, delete)
* Cleanup orphaned deployments
* Dependency validation (`doctor`)
* Multi-architecture builds (`linux/arm64` by default, `linux/amd64`, `linux/arm/v7`, …)

---

## Installation

### Quick install

```bash
curl -sSL https://raw.githubusercontent.com/acaraballocastro/shipyard/main/install.sh | bash
```

### Manual installation

#### Build from source

```bash
go build -o shipyard
```

#### Install binary globally

```bash
sudo mv shipyard /usr/local/bin/
```

---

## Requirements

* Docker
* Docker Compose v2 (`docker compose`)
* Git, with SSH access to GitHub (repositories are cloned via `git@github.com:`)
* Docker Buildx, logged in to `ghcr.io` (`docker login ghcr.io`) for `--repo` deployments
* `shipyard setup` run once, so `github_user` is known (images are pushed to `ghcr.io/<github_user>/<name>`)
* SSH (optional for future remote deployments)

Verify your environment:

```bash
shipyard doctor
```

---

## Usage

### Interactive mode

Run commands without arguments and Shipyard asks for what it needs:

```bash
shipyard deploy      # step-by-step wizard, prints the equivalent command before deploying
shipyard logs        # pick a service from a list (also restart, update, status, shell, delete)
```

Flags still work as before and skip the questions. Prompts are disabled when there is no terminal, with `--no-input`, or when `SHIPYARD_NO_INPUT` or `CI` is set.

---

### Remote servers

Deploy to another machine (e.g. a Raspberry Pi) over SSH. Shipyard runs on the server too:
cloning, building, pushing and running all happen **on the server that hosts the service**,
and its state lives there. Your machine only asks the questions and forwards the command.

```bash
shipyard server add pi --host pi@192.168.1.50   # checks ssh/docker, installs shipyard, offers `setup`
shipyard deploy                                  # the wizard asks where to deploy
shipyard deploy --server pi --repo user/app      # or explicitly
shipyard logs                                    # pick a service on the default server
shipyard list --server local                     # force this machine
shipyard server use local                        # change the default
```

* `--server` works with `deploy`, `update`, `logs`, `restart`, `status`, `shell`, `delete`, `list`, `cleanup` and `doctor`. `watch` always runs locally: start it on the server itself.
* Local `--env-file` files are uploaded to `~/.shipyard/envfiles/` on the server (0600). `--env` values are sent over stdin, never on the command line.
* SSH uses your normal `ssh` client, so `~/.ssh/config`, keys and agents apply. Passwordless (key) login is required.
* The server needs what a local repo deploy needs: `shipyard setup`, `docker login ghcr.io`, Docker Buildx and an SSH key with GitHub access.

---

### Deploy a Docker image

```bash
shipyard deploy --image traefik/whoami
```

---

### Deploy from a Git repository (Traefik mode)

```bash
shipyard deploy \
  --repo user/my-app \
  --mode traefik \
  --domain app.example.com \
  --network edge \
  --create-network
```

---

### Deploy in local mode

```bash
shipyard deploy \
  --repo user/my-api \
  --mode local \
  --port 3000:3000
```

---

### Use volumes

```bash
shipyard deploy \
  --repo user/app \
  --volume /srv/data:/app/data
```

---

## Deployment options

| Flag | Applies to | Description |
|---|---|---|
| `--repo owner/name` | git | GitHub repository to clone, build and push to GHCR |
| `--image ref` | image | Existing image to run (public or private registry) |
| `--name` | all | Service name (default: derived from repo or image) |
| `--mode local\|traefik` | all | Publish ports directly, or route through Traefik (default `local`) |
| `--port`, `--volume` | all | Port mappings (local mode) and volume mounts |
| `--env-file path` | all | Env file for the container. Repeatable. Stored as an absolute path |
| `--env KEY=VALUE` | all | Single variable. Repeatable. Stored in the service's `.env` (mode 0600) |
| `--build-arg KEY=VALUE` | git | Build argument, persisted for updates. Repeatable |
| `--build-arg KEY` | git | Build argument read from the environment at build time; only the name is persisted |
| `--platform` | git | Buildx platform (default `linux/arm64`) |
| `--domain`, `--network`, `--internal-port` | traefik | Host rule, external network (default `edge`), container port (default `80`) |
| `--entrypoint` | traefik | Traefik entrypoint (default `web`) |
| `--tls` | traefik | Adds `tls=true` to the router |
| `--cert-resolver name` | traefik | Adds `tls.certresolver` (requires `--tls`) |
| `--router-name` | traefik | Router/service name (default: service name) |
| `--middleware name` | traefik | Attach a middleware, e.g. `redirect-https@file`. Repeatable |
| `--healthcheck-cmd` | all | Enables a healthcheck (`CMD-SHELL`) |
| `--healthcheck-interval/-timeout/-start-period` | all | Durations such as `30s`, `1m` |
| `--healthcheck-retries` | all | Consecutive failures before `unhealthy` |
| `--no-healthcheck` | all | Remove a previously configured healthcheck |
| `--create-network` | all | Create `--network` if it does not exist |
| `--compose-path` | all | Base directory (default `/srv/apps`) |

### Environment files

```bash
shipyard deploy \
  --repo acaraballocastro/inariops-backend \
  --mode traefik \
  --domain inariback.example.com \
  --network edge \
  --internal-port 9142 \
  --env-file /srv/config/inariops.env \
  --env-file /srv/config/inariops.production.env \
  --env APP_ENV=production \
  --env LOG_LEVEL=info
```

* Env files are validated (existence and `KEY=VALUE` syntax) before anything is deployed, and referenced from `docker-compose.yml` by absolute path. They are not copied, so editing the file and running `shipyard update` picks up the change. Keep them outside Git repositories.
* `--env` values are written to `/srv/apps/<service>/.env` with mode `0600`. That file is listed last in `env_file`, so it overrides values from `--env-file`.
* Running `deploy` again with `--env` merges into the existing variables; variables you do not mention are kept. To remove one, edit `/srv/apps/<service>/.env`.
* Standard Compose semantics apply (`KEY=value`, `KEY="quoted"`, `KEY='literal'`, comments with `#`).

### Build arguments and platform

```bash
shipyard deploy \
  --repo acaraballocastro/inariWeb \
  --platform linux/arm64 \
  --build-arg NEXT_PUBLIC_API_URL=https://inariback.example.com/api/v1
```

Build arguments are stored in `deployment.yaml` and reused by `update`. **Build arguments are not for secrets**: they are visible in the image history and in the process list. If a build needs a token (for example a private npm registry), pass only its name with `--build-arg NPM_TOKEN`: Docker reads the value from your shell environment, and Shipyard stores only the name. BuildKit secrets (`--secret`) are a better fit and are planned.

`--platform` defaults to `linux/arm64` (Raspberry Pi 5). Use `linux/amd64`, `linux/arm/v7`, or a comma-separated list for multi-arch images.

### Traefik and TLS

```bash
shipyard deploy \
  --repo acaraballocastro/inariWeb \
  --mode traefik \
  --domain inari.example.com \
  --network edge \
  --internal-port 3000 \
  --entrypoint websecure \
  --tls \
  --cert-resolver letsencrypt
```

Generated labels:

```yaml
labels:
  - traefik.enable=true
  - traefik.http.routers.inariweb.rule=Host(`inari.example.com`)
  - traefik.http.routers.inariweb.entrypoints=websecure
  - traefik.http.routers.inariweb.tls=true
  - traefik.http.routers.inariweb.tls.certresolver=letsencrypt
  - traefik.http.services.inariweb.loadbalancer.server.port=3000
```

Without `--entrypoint`/`--tls`, labels are identical to previous Shipyard versions (`web`, no TLS). The entrypoint and cert resolver must already exist in your Traefik static configuration.

### Healthchecks

```bash
--healthcheck-cmd "wget -qO- http://localhost:3000/api/health || exit 1" \
--healthcheck-interval 30s \
--healthcheck-timeout 5s \
--healthcheck-retries 3 \
--healthcheck-start-period 20s
```

Healthchecks are optional. The command runs inside the container, so the tool it uses (`wget`, `curl`, …) must exist in the image. When a healthcheck is configured, `deploy` and `update` wait for the container to become `healthy` and fail if it becomes `unhealthy`. `shipyard status <service>` shows the current health.

---

## Deployment configuration

Every service has a directory with its configuration:

```text
/srv/apps/
├── inariweb/
│   ├── docker-compose.yml   # generated, do not edit
│   ├── deployment.yaml      # source of truth, non-secret
│   └── .env                 # --env values, mode 0600 (only if used)
└── inariops-backend/
    └── ...
```

Example `deployment.yaml`:

```yaml
version: 1
name: inariweb
source:
  type: git
  repository: acaraballocastro/inariWeb
image: ghcr.io/acaraballocastro/inariweb
build:
  platform: linux/arm64
  args:
    NEXT_PUBLIC_API_URL: https://inariback.example.com/api/v1
deploy:
  mode: traefik
  domain: inari.example.com
  network: edge
  internal_port: "3000"
  entrypoint: websecure
  tls: true
env:
  files:
    - /srv/config/inari.env
  keys:
    - APP_ENV
healthcheck:
  enabled: false
```

* It never holds secret values: env files are stored as paths, `--env` variables only by name (`env.keys`).
* You can edit it by hand. It is validated (unknown fields are rejected) before the next `update`, and `docker-compose.yml` is regenerated from it.
* Running `shipyard deploy` again for an existing service starts from this file. Only the flags you pass change; everything else is kept.

---

## Update behavior

```bash
shipyard update inariweb
```

1. Loads `deployment.yaml`, or migrates a legacy deployment (see below).
2. Validates the configuration and checks that the env files still exist.
3. **Git sources:** clones the latest commit and builds with the saved platform and build arguments, then pushes to GHCR. **Image sources:** skips the build; the latest image is pulled.
4. If the build fails, the update stops and nothing on disk or in Docker changes.
5. Regenerates `docker-compose.yml` and runs `docker compose pull` and `up -d`. Env files, volumes and networks are kept, and volumes are never removed.
6. Waits until the container is running, and `healthy` if a healthcheck is configured.
7. If step 5 or 6 fails, Shipyard restores the previous `docker-compose.yml`, `deployment.yaml` and `.env` and starts that configuration again. Images are always tagged `:latest`, so it cannot roll back to the previous image.

`shipyard watch` uses the same workflow and skips image-based services.

### Migrating existing deployments

Services deployed with older versions have no `deployment.yaml`. The first `shipyard update <service>`, or a new `shipyard deploy`, rebuilds the configuration from `~/.shipyard-state.yaml` and the existing `docker-compose.yml`: image, ports, volumes, network, domain, internal port and entrypoint. It then writes `deployment.yaml`. Git deployments get the platform `linux/arm64`, the only platform older versions built. The state file format is unchanged, so no manual migration is needed.

---

## Complete examples

### Next.js (InariWeb)

```bash
shipyard deploy \
  --repo acaraballocastro/inariWeb \
  --mode traefik \
  --domain inari.example.com \
  --network edge \
  --internal-port 3000 \
  --platform linux/arm64 \
  --entrypoint websecure \
  --tls \
  --build-arg NEXT_PUBLIC_API_URL=https://inariback.example.com/api/v1 \
  --env-file /srv/config/inariweb.env \
  --healthcheck-cmd "wget -qO- http://localhost:3000/ || exit 1" \
  --healthcheck-start-period 20s
```

`NEXT_PUBLIC_*` variables are inlined at build time, so they must be build arguments. Server-side variables belong in the env file.

### Go backend (InariOps)

```bash
shipyard deploy \
  --repo acaraballocastro/inariops-backend \
  --mode traefik \
  --domain inariback.example.com \
  --network edge \
  --internal-port 9142 \
  --platform linux/arm64 \
  --env-file /srv/config/inariops.env \
  --entrypoint websecure \
  --tls
```

### Existing image

```bash
shipyard deploy \
  --image nginx:alpine \
  --mode traefik \
  --domain nginx.example.com \
  --network edge \
  --internal-port 80
```

For private registries, run `docker login <registry>` on the host first.

---

## Security considerations

* Secrets belong in env files (`--env-file`, ideally `chmod 600` and outside any Git repository) or in `--env`, which is stored in a `0600` file. They are never written to `deployment.yaml` or `~/.shipyard-state.yaml`.
* Shipyard never prints env values. Logs show only variable names and env file paths. Build argument values are not logged either.
* Commands run without a shell (`exec` with an argument list), and user input (service names, repositories, platforms, domains, Traefik names, durations) is validated first.
* Never pass secrets with `--build-arg KEY=VALUE`. Values are persisted and end up in the image metadata.
* `update` and `delete` keep named volumes and bind-mounted data. `delete` removes the service directory, including its `.env`.
* Add `.env` and `*.env` to the `.gitignore` of your application repositories.

---

## Service Management

### List services

```bash
shipyard list
```

### Restart a service

```bash
shipyard restart <service>
```

### Show status (including health)

```bash
shipyard status <service>
```

### View logs

```bash
shipyard logs <service>
```

### Update a service

```bash
shipyard update <service>
```

### Delete a service

```bash
shipyard delete <service>
```

### Cleanup unused resources

```bash
shipyard cleanup
```

---

## Architecture

```
shipyard/
├── cmd/
├── internal/
│   ├── compose/
│   ├── config/
│   ├── deployment/   # deployment.yaml model, validation, migration
│   ├── docker/
│   ├── envfile/      # env file parsing/validation/writing
│   ├── git/
│   ├── state/
│   ├── ui/
│   └── update/       # shared build → compose → up → verify pipeline
├── .github/
├── main.go
├── install.sh
├── Makefile
└── README.md
```

---

## Supported platforms

* Linux (amd64)
* Linux (arm64)

---

## Releases

Shipyard uses GitHub Actions to build binaries automatically.

Create a new release:

```bash
git tag v0.1.0
git push origin v0.1.0
```

Artifacts:

* shipyard-linux-amd64
* shipyard-linux-arm64

---

## Roadmap

* Watch mode (auto redeploy on changes)
* Remote deployments (SSH / API)
* BuildKit secrets for builds
* Image tags per commit (enables rollback)
* Rollback system
* Stable / beta release channels

---

## Contributing

Contributions are welcome.

1. Fork the repository
2. Create a feature branch
3. Commit changes
4. Open a pull request

Keep changes focused and consistent with the CLI design.

---

## License

MIT
