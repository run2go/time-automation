# Time-Automation

A lightweight Go application, packaged in an Alpine-based Docker image, for automating time-tracking workflows through API calls.  
Designed to interact with a custom time-tracking system, it supports smart work/pause logic, calendar-based holiday detection, and webhook notifications.

## Features

- Performs start, stop, and pause actions via the time-tracking API
- Configurable randomized work and break time ranges
- Enforces minimum durations for both work and break periods
- Parses ICS calendar files to skip tracking on holidays or vacation days
- Sends notifications for relevant time-tracking events
- Retries up to 5 times on network outage
- Persists state in a JSON file to track progress across restarts
- Environment-based configuration for easy deployment

## Usage

### Environment Variables

Configure the app via a `.env` file to set API endpoints, credentials, time windows, and notification settings.
Use the provided `.env.example` as reference for the variable names.

### Build & Run

```bash
docker build -t time-automation .
docker run --env-file .env -v $PWD/state.json:/app/state.json time-automation
```
