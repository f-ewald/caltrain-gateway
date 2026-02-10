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
| GET | `/up` | Health check |
| GET | `/caltrain/timetable` | Get all train departures by stop ID |
| GET | `/caltrain/timetable?weekday=Monday` | Get departures filtered by weekday |

## Timetable

The timetable module parses Caltrain schedule data and provides departures grouped by stop ID. Each departure includes train ID, line, direction, arrival/departure times, and destination. Schedules are filtered by day type (weekday/weekend) based on the `weekday` query parameter.

Supported weekday values: `Monday`, `Tuesday`, `Wednesday`, `Thursday`, `Friday`, `Saturday`, `Sunday`

## Lines

Lines represent the different Caltrain services (Limited, Local, Express, etc.). Each line includes metadata such as validity dates, transport mode, public code, and monitoring status. Lines can be loaded from a local file or fetched from the 511 API.

## License

[MIT License](LICENSE)
