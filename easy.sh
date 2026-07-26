#!/bin/bash
# Install the gittr / Nostr-git Pyramid adaptation (arbadacarbaYK/pyramid),
# not stock fiatjaf/pyramid. See FORGE.md.
set -euo pipefail

REPO_URL="${PYRAMID_REPO_URL:-https://github.com/arbadacarbaYK/pyramid.git}"
REPO_REF="${PYRAMID_REPO_REF:-master}"

# ufw allow (if necessary)
if command -v ufw >/dev/null 2>&1; then
  ufw allow http || true
  ufw allow https || true
fi

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing dependency: $1"
    exit 1
  }
}

need git
need go
# CGO + LMDB
need gcc

DIR="$(pwd)/pyramid"
if [ -d "$DIR/.git" ]; then
  echo "updating existing clone in $DIR ..."
  git -C "$DIR" fetch --depth 1 origin "$REPO_REF"
  git -C "$DIR" checkout -B "$REPO_REF" "FETCH_HEAD"
else
  echo "cloning $REPO_URL ($REPO_REF) into $DIR ..."
  rm -rf "$DIR"
  git clone --depth 1 --branch "$REPO_REF" "$REPO_URL" "$DIR"
fi

cd "$DIR"
# Prefer just if present; otherwise plain Go build
if command -v just >/dev/null 2>&1 && [ -f justfile ]; then
  just templ 2>/dev/null || true
  just build 2>/dev/null || CGO_ENABLED=1 go build -o ./pyramid-exe .
else
  if command -v templ >/dev/null 2>&1; then
    templ generate || true
  fi
  CGO_ENABLED=1 go build -o ./pyramid-exe .
fi

if [ ! -x ./pyramid-exe ] && [ -x ./pyramid ]; then
  : # already named pyramid
elif [ -x ./pyramid-exe ]; then
  mv -f ./pyramid-exe ./pyramid
fi
chmod +x ./pyramid
INSTALL_DIR="$(pwd)"

# create systemd service file (PORT=443 = pyramid autocert; behind nginx use PORT=3334)
SERVICE_USER="${SUDO_USER:-$USER}"
sudo tee /etc/systemd/system/pyramid.service >/dev/null <<EOF
[Unit]
Description=pyramid relay (gittr / Nostr-git adaptation)
After=network.target

[Service]
User=$SERVICE_USER
ExecStart=$INSTALL_DIR/pyramid
WorkingDirectory=$INSTALL_DIR
Restart=always
RestartSec=60
Environment=HOST=0.0.0.0
Environment=PORT=443
Environment=NO_AUTO_UPDATES=true
Environment=DATA_PATH=$INSTALL_DIR/data

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable pyramid
sudo systemctl restart pyramid

sudo tee /etc/motd >/dev/null <<'EOF'

### pyramid (gittr / Nostr-git adaptation)
- repo: https://github.com/arbadacarbaYK/pyramid
- docs: FORGE.md
- status: systemctl status pyramid
- logs: journalctl -xefu pyramid
- restart: systemctl restart pyramid

EOF

IP=$(curl -s https://api.ipify.org || hostname -I | awk '{print $1}')
echo "***"
echo ""
echo "pyramid (arbadacarbaYK adaptation) is running."
echo "visit http://$IP to setup domain + root, then set open_kinds_spec (see FORGE.md)."
echo "For gittr-style behind nginx: set Environment PORT=3334 HOST=127.0.0.1 and proxy wss."
