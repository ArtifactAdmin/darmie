#!/usr/bin/env bash
# deploy.sh — Deploy darmie to a VPS via SSH
#
# Usage:
#   ./deploy.sh [ssh-target]
#
# Default target is defined by VPS_HOST below.
# Override with:  ./deploy.sh user@other-server.com

set -euo pipefail

# ── Configuration ─────────────────────────────────────────────────────────────
VPS_HOST="${1:-root@155.138.138.87}"
REPO_URL="https://github.com/ArtifactAdmin/darmie"
REPO_DIR="/root/darmie"
APP_NAME="darmie"
DATA_DIR="/data/darmie"
HOST_PORT="8080"
CONTAINER_PORT="8080"
# ──────────────────────────────────────────────────────────────────────────────

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

log()  { echo -e "${CYAN}[deploy]${NC} $*"; }
ok()   { echo -e "${GREEN}[  ok  ]${NC} $*"; }
warn() { echo -e "${YELLOW}[ warn ]${NC} $*"; }
err()  { echo -e "${RED}[ err  ]${NC} $*" >&2; exit 1; }

log "Target: ${VPS_HOST}"
log "Repo:   ${REPO_URL}"
log "Dir:    ${REPO_DIR}"

# ── Verify SSH connectivity ───────────────────────────────────────────────────
log "Checking SSH connectivity..."
ssh -o BatchMode=yes -o ConnectTimeout=10 "${VPS_HOST}" "echo ok" > /dev/null \
  || err "Cannot reach ${VPS_HOST} via SSH. Ensure your key is in ~/.ssh/authorized_keys."
ok "SSH connection successful"

# ── Remote deployment ─────────────────────────────────────────────────────────
log "Running remote deployment steps..."

ssh -o BatchMode=yes "${VPS_HOST}" bash -s << EOF
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
NC='\033[0m'
log()  { echo -e "\${CYAN}[remote]\${NC} \$*"; }
ok()   { echo -e "\${GREEN}[  ok  ]\${NC} \$*"; }
err()  { echo -e "\${RED}[ err  ]\${NC} \$*" >&2; exit 1; }

# ── Ensure Docker is installed ────────────────────────────────────────────────
if ! command -v docker &>/dev/null; then
  log "Docker not found — installing..."
  apt-get update -qq
  apt-get install -y -qq ca-certificates curl gnupg lsb-release
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
    | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
  chmod a+r /etc/apt/keyrings/docker.gpg
  echo "deb [arch=\$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
    https://download.docker.com/linux/ubuntu \$(lsb_release -cs) stable" \
    > /etc/apt/sources.list.d/docker.list
  apt-get update -qq
  apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin
  systemctl enable --now docker
  ok "Docker installed"
else
  ok "Docker already installed: \$(docker --version)"
fi

# ── Ensure git is installed ───────────────────────────────────────────────────
command -v git &>/dev/null || apt-get install -y -qq git

# ── Clone or update the repository ───────────────────────────────────────────
if [ -d "${REPO_DIR}/.git" ]; then
  log "Repository found — pulling latest changes..."
  git -C "${REPO_DIR}" fetch --all --prune
  git -C "${REPO_DIR}" reset --hard origin/\$(git -C "${REPO_DIR}" symbolic-ref --short HEAD)
  ok "Repository updated"
else
  log "Cloning repository..."
  git clone "${REPO_URL}" "${REPO_DIR}"
  ok "Repository cloned"
fi

# ── Build Docker image ────────────────────────────────────────────────────────
log "Building Docker image..."
docker build -t "${APP_NAME}:latest" "${REPO_DIR}"
ok "Image built"

# ── Create persistent data directory ─────────────────────────────────────────
mkdir -p "${DATA_DIR}"

# ── Stop and remove existing container (if any) ───────────────────────────────
if docker ps -a --format '{{.Names}}' | grep -q "^${APP_NAME}\$"; then
  log "Stopping existing container..."
  docker stop "${APP_NAME}" || true
  docker rm   "${APP_NAME}" || true
  ok "Old container removed"
fi

# ── Start new container ───────────────────────────────────────────────────────
log "Starting container..."
docker run -d \
  --name "${APP_NAME}" \
  --restart unless-stopped \
  -p "${HOST_PORT}:${CONTAINER_PORT}" \
  -v "${DATA_DIR}:/data" \
  "${APP_NAME}:latest"
ok "Container started"

# ── Health check ──────────────────────────────────────────────────────────────
log "Waiting for app to become ready..."
for i in \$(seq 1 15); do
  if curl -sf "http://localhost:${HOST_PORT}" > /dev/null 2>&1; then
    ok "App is healthy ✓"
    break
  fi
  sleep 2
done

docker ps --filter "name=^/${APP_NAME}\$"
EOF

ok "Deployment complete!"
echo -e "\n${GREEN}App is running at:${NC} http://${VPS_HOST%%@*}${HOST_PORT:+:${HOST_PORT}}"
echo -e "${CYAN}Logs:${NC} ssh ${VPS_HOST} 'docker logs -f ${APP_NAME}'"
