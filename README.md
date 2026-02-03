# Caltrain Gateway

A gateway service for accessing Caltrain schedule and real-time transit information.

## Overview

Caltrain Gateway provides an interface to retrieve Caltrain schedules, arrivals, and service updates.

## Prerequisites

You will need to obtain a 511 API key to use this service. Sign up at [511.org](https://511.org/developer-services).

## Installation

1. Clone the repository:
    ```bash
    git clone https://github.com/fewald/caltrain-gateway.git
    cd caltrain-gateway
    ```

2. Configure environment variables:
    ```bash
    cp .env.example .env
    # Edit .env with your configuration
    ```

## Build

```bash
go build -o caltrain-gateway ./cmd/caltrain-gateway
```

## Usage

```bash
./caltrain-gateway
```

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | Server port | `8080` |

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Health check |

## License

[MIT License](LICENSE)
