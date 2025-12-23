# Deployment Guide for VPS

This guide assumes you have a Linux VPS (Ubuntu/Debian/CentOS) and root/sudo access.

## 1. Build the Application
On your local machine (this project folder):
```bash
make build-linux
```
This creates `ezstremio-linux-amd64`.

## 2. Upload to VPS
Use `scp` to upload the binary and your `.env` file. Replace `user@your-vps-ip` with your actual login.
```bash
# Create directory on VPS
ssh user@your-vps-ip "mkdir -p /opt/ezstremio"

# Upload binary
scp ezstremio-linux-amd64 user@your-vps-ip:/opt/ezstremio/

# Upload config
scp .env user@your-vps-ip:/opt/ezstremio/
```

## 3. Setup Systemd Service
Upload the service file:
```bash
scp ezstremio.service user@your-vps-ip:/etc/systemd/system/
```

Then SSH into your VPS and enable the service:
```bash
ssh user@your-vps-ip
# Inside VPS:
chmod +x /opt/ezstremio/ezstremio-linux-amd64
systemctl daemon-reload
systemctl enable ezstremio
systemctl start ezstremio
systemctl status ezstremio
```
You should see "active (running)".

## 4. HTTPS with Caddy (Recommended)
Stremio requires HTTPS. The easiest way is using Caddy Server, which handles SSL automatically.

1. **Install Caddy** (Ubuntu/Debian):
   ```bash
   sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https
   curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
   curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
   sudo apt update
   sudo apt install caddy
   ```

2. **Configure Caddy**:
   Edit `/etc/caddy/Caddyfile`:
   ```bash
   nano /etc/caddy/Caddyfile
   ```
   Replace contents with:
   ```caddy
   your-domain.com {
       reverse_proxy localhost:8080
   }
   ```
   *Note: If you don't have a domain, you can use a "magic" domain like `nip.io`.
   If your VPS IP is `1.2.3.4`, use `1.2.3.4.nip.io` as your domain in Caddyfile.
   Caddy will automatically get a valid Let's Encrypt certificate for it!*

3. **Restart Caddy**:
   ```bash
   systemctl restart caddy
   ```

## 5. Verify
Open `https://your-domain.com/manifest.json` in your browser.
If it loads the JSON, you are ready!
Add this URL to Stremio.
