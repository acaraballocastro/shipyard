# 📦 Shipyard

> 🧰 A lightweight CLI tool to deploy Docker applications from GitHub repositories or public images directly to your homelab (Raspberry Pi / Linux servers).

Shipyard automates the full lifecycle of container deployment:

- Build from GitHub repositories
- Push to GitHub Container Registry (GHCR)
- Deploy via Docker Compose
- Support for Traefik or local port mode
- Manage services via CLI (deploy, restart, logs, delete, cleanup)

---

# ⚓ Features

- 🚀 Deploy from GitHub repos (build + GHCR)
- 📦 Deploy from public Docker images (no build required)
- 🌐 Traefik support (automatic routing)
- 🔌 Local port publishing mode
- 🧱 Docker network management
- 📊 State tracking of deployed apps
- 🧹 Cleanup orphan deployments
- 🗑️ Safe delete with confirmation
- 📜 Logs and restart commands

---

# ⚙️ Requirements

Shipyard assumes your system has:

- Docker
- Docker Compose
- Git

You can verify with:

```bash
shipyard doctor
```

---

# 📥 Installation

## 🟢 Build from source

```bash
git clone https://github.com/your-user/shipyard
cd shipyard
go build -o shipyard
sudo mv shipyard /usr/local/bin/
```

---

## 🟢 Verify installation

```bash
shipyard --help
```

---

# 🚀 Usage

## 📦 Deploy from GitHub repository

```bash
shipyard deploy --repo user/my-app
```

---

## 🌐 Deploy with Traefik

```bash
shipyard deploy   --repo user/my-app   --mode traefik   --domain app.example.com   --network edge   --create-network
```

---

## 🔌 Deploy with local ports

```bash
shipyard deploy   --repo user/my-api   --mode local   --port 3000:3000
```

---

## 📦 Deploy public Docker image

```bash
shipyard deploy --image traefik/whoami
```

---

## 🌐 Public image with Traefik

```bash
shipyard deploy   --image traefik/whoami   --mode traefik   --domain whoami.local
```

---

# 🗑️ Manage services

## 📜 List deployed apps

```bash
shipyard list
```

---

## 🔄 Restart service

```bash
shipyard restart my-app
```

---

## 📄 View logs

```bash
shipyard logs my-app
```

---

## 🗑️ Delete service (safe)

```bash
shipyard delete my-app
```

---

## 🧹 Cleanup orphan services

```bash
shipyard cleanup
```

---

# 🧠 Architecture

```
GitHub Repo → Buildx → GHCR → Docker Compose → Deploy
```

or

```
Docker Image → Pull → Docker Compose → Deploy
```

State stored in:

```
~/.shipyard/state.yaml
```

---

# ⚠️ Disclaimer

Designed for homelab / personal infrastructure use.
