#!/bin/bash

# ufw allow (if necessary)
ufw allow http
ufw allow https

# download binary into ./pyramid directory
VERSION=$(curl -s "https://api.github.com/repos/fiatjaf/pyramid/releases/latest" | grep '"tag_name":' | cut -d '"' -f 4)
mkdir -p pyramid
cd pyramid
rm pyramid-exe-old
mv pyramid-exe pyramid-exe-old
wget "https://github.com/fiatjaf/pyramid/releases/download/$VERSION/pyramid-exe"
chmod +x pyramid-exe
DIR=$(pwd)

# create systemd service file
echo "[Unit]
Description=pyramid relay
After=network.target

[Service]
User=$USER
ExecStart=$DIR/pyramid-exe
WorkingDirectory=$DIR
Restart=always
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
