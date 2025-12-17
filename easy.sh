#!/bin/bash

# ufw allow (if necessary)
ufw allow http
ufw allow https

# download binary into ./pyramid directory
VERSION=$(curl -s "https://api.github.com/repos/fiatjaf/pyramid/releases/latest" | grep '"tag_name":' | cut -d '"' -f 4)
case $(uname -m) in
  x86_64)
    ARCH="amd64"
    ;;
  aarch64)
    ARCH="arm64"
    ;;
  *)
    echo "unsupported architecture: $(uname -m)"
    exit 1
    ;;
esac
mkdir -p pyramid
cd pyramid
rm pyramid-old
mv pyramid pyramid-old 2>/dev/null || true
wget "https://github.com/fiatjaf/pyramid/releases/download/$VERSION/pyramid-$ARCH"
mv "pyramid-$ARCH" pyramid
chmod +x pyramid
DIR=$(pwd)

# create systemd service file
echo "[Unit]
Description=pyramid relay
After=network.target

[Service]
User=$USER
ExecStart=$DIR/pyramid
WorkingDirectory=$DIR
Restart=always
RestartSec=60
Environment=HOST=0.0.0.0 PORT=443

[Install]
WantedBy=multi-user.target
" > /etc/systemd/system/pyramid.service

# reload systemd, enable and start
sudo systemctl daemon-reload
sudo systemctl enable pyramid
sudo systemctl start pyramid

# setup motd
echo '

### pyramid
- see status: systemctl status pyramid
- view logs: journalctl -xefu pyramid
- restart: systemctl restart pyramid

' > /etc/motd

# print instructions
IP=$(curl -s https://api.ipify.org)
echo "***"
echo ""
echo "pyramid is running. visit http://$IP to setup."
