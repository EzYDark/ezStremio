# Deployment Guide for ezStremio

This guide covers how to deploy the `ezStremio` addon using Docker and Caddy. This setup provides automatic HTTPS (SSL) support using `sslip.io`, making it compatible with Stremio's security requirements even on a local network.

## Prerequisites

- **Docker** and **Docker Compose** installed on your server or local machine.
- A **TMDB API Key** (Get one at [themoviedb.org](https://www.themoviedb.org/documentation/api)).

## Quick Start

### 1. Configuration

1.  **Copy the environment template:**
    ```bash
    cp .env.example .env
    ```

2.  **Edit the `.env` file:**
    Open `.env` in your text editor.
    ```bash
    nano .env
    ```

3.  **Fill in your details:**

    *   `TMDB_API_KEY`: Your API key from The Movie Database.
    *   `DOMAIN_NAME`: Your local IP combined with `sslip.io` to get a valid SSL certificate.
        *   Example: If your IP is `192.168.0.178`, use `192.168.0.178.sslip.io`.
    *   `ACME_EMAIL`: Your real email address (required by Let's Encrypt for certificate generation).

    **Example `.env`:**
    ```ini
    TMDB_API_KEY=123456abcdef...
    DOMAIN_NAME=192.168.0.178.sslip.io
    ACME_EMAIL=me@example.com
    ```

### 2. Deployment

Run the following command to build and start the services:

```bash
sudo docker compose up -d --build
```

*   `-d`: Detached mode (runs in the background).
*   `--build`: Forces a rebuild of the addon image.

### 3. Verification

Check if the services are running:

```bash
sudo docker compose ps
```

View logs (useful for troubleshooting):

```bash
sudo docker compose logs -f
```

### 4. Install in Stremio

1.  Wait about 30-60 seconds after starting for Caddy to obtain the SSL certificate.
2.  Open **Stremio**.
3.  Go to the search bar (or "Addons" -> "Search Addon URL").
4.  Paste your addon URL:
    ```
    https://192.168.0.178.sslip.io/manifest.json
    ```
    *(Replace `192.168.0.178` with your actual IP)*
5.  Click **Install**.

## Troubleshooting

### "Permission denied" connecting to Docker
If you see a permission error, either run commands with `sudo` or add your user to the docker group:
```bash
sudo usermod -aG docker $USER
# Log out and log back in for changes to take effect
```

### "Failed to fetch" in Stremio
*   Ensure you are using `https://`.
*   Verify Caddy has issued a certificate by checking logs: `sudo docker compose logs caddy`.
*   Ensure your `.env` file has a valid `ACME_EMAIL`.

### Stopping the Addon
To stop the services:
```bash
sudo docker compose down
```
