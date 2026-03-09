# Docker

This document describes how to use Docker and Docker Compose to run MisterMorph.

## Dockerfile

The `Dockerfile.goreleaser` is used to build the Docker image for MisterMorph. It uses a multi-stage build to create a small image based on `alpine:3.21`.

The image is available on GitHub Container Registry: `ghcr.io/quailyquaily/mistermorph`

## Docker Compose

The `docker-compose.yaml` file is provided to run MisterMorph with Telegram integration.

### Usage

1.  Create a `config.yaml` file with your Telegram bot token and other configurations.
2.  Run `docker-compose up -d` to start the MisterMorph service.

### Configuration

The `docker-compose.yaml` file is configured as follows:

```yaml
services:
  mistermorph_telegram:
    image: ghcr.io/quailyquaily/mistermorph:latest
    restart: unless-stopped
    ports:
      - "8787:8787"
    volumes:
      - ./config.yaml:/app/config.yaml:ro
      - morph-state:/app/.morph
    # run telegram bot
    command: telegram --config /app/config.yaml

volumes:
  morph-state:
```

-   `image`: The Docker image to use.
-   `restart`: The restart policy for the container.
-   `ports`: The ports to expose.
-   `volumes`:
    -   `./config.yaml:/app/config.yaml:ro`: Mounts the `config.yaml` file into the container.
    -   `morph-state:/app/.morph`: A named volume to persist the application state.
-   `command`: The command to run when the container starts.
