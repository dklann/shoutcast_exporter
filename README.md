# Shoutcast exporter for Prometheus

This is a simple [Prometheus](https://prometheus.io/) exporter that scrapes stats from the [Shoutcast](https://www.shoutcast.com/) streaming media server. It uses the Shoutcast JSON API (`/statistics?json=1`).

This exporter is based on the Icecast exporter (`icecast_exporter`) from [Markus Lindenberg](https://github.com/markuslindenberg/).

By default shoutcast_exporter listens on port 9240 for HTTP requests.

## Installation

### Using `go get`

```bash
go get github.com/dklann/shoutcast_exporter
```

# Running

Help on flags:
```
go run shoutcast_exporter --help

Usage of ./shoutcast_exporter:
  -shoutcast.scrape-uri string
    	URI on which to scrape Shoutcast. (default "http://localhost:8000/status-json.xsl")
  -shoutcast.timeout duration
    	Timeout for trying to get stats from Shoutcast. (default 5s)
  -log.format value
    	Set the log target and format. Example: "logger:syslog?appname=bob&local=7" or "logger:stdout?json=true" (default "logger:stderr")
  -log.level value
    	Only log messages with the given severity or above. Valid levels: [debug, info, warn, error, fatal]
  -web.listen-address string
    	Address to listen on for web interface and telemetry. (default ":9240")
  -web.telemetry-path string
    	Path under which to expose metrics. (default "/metrics")
```
