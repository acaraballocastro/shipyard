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
* Service lifecycle management (start, stop, restart, logs, update, delete)
* Cleanup orphaned deployments
* Dependency validation (`doctor`)
* Multi-architecture support (linux/amd64, linux/arm64)

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
* Docker Compose
* Git
* SSH (optional for future remote deployments)
* Docker Buildx

Verify your environment:

```bash
shipyard doctor
```

---

## Usage

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

## Service Management

### List services

```bash
shipyard list
```

### Restart a service

```bash
shipyard restart <service>
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
│   ├── docker/
│   ├── git/
│   ├── state/
│   └── ui/
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
* Environment file support (.env templates)
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
