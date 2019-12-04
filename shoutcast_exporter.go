// Copyright 2016 Markus Lindenberg
// Copyright 2019 David Klann
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/log"
)

const (
	namespace = "shoutcast"
)

var (
	labelNames = []string{"id", "streampath"}
)

type ShoutcastStatus struct {
	Totalstreams     int    `json:"totalstreams"`
	Activestreams    int    `json:"activestreams"`
	Streams          []struct {
		ID                int    `json:"id"`
		Currentlisteners  int    `json:"currentlisteners"`
		Streampath        string `json:"streampath"`
		Streamuptime      int    `json:"streamuptime"`
	} `json:"streams"`
}

// Exporter collects Shoutcast stats from the given URI and exports them using
// the prometheus metrics package.
type Exporter struct {
	URI   string
	mutex sync.RWMutex

	up                              prometheus.Gauge
	totalScrapes, jsonParseFailures prometheus.Counter
	listeners                       *prometheus.GaugeVec
	streamUptime                    *prometheus.GaugeVec
	client                          *http.Client
}

// NewExporter returns an initialized Exporter.
func NewExporter(uri string, timeout time.Duration) *Exporter {
	return &Exporter{
		URI: uri,
		up: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "up",
			Help:      "Was the last scrape of Shoutcast successful.",
		}),
		totalScrapes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "exporter_total_scrapes",
			Help:      "Current total Shoutcast scrapes.",
		}),
		jsonParseFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "exporter_json_parse_failures",
			Help:      "Number of errors while parsing JSON.",
		}),
		listeners: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "listeners",
			Help:      "The number of currently connected listeners.",
		}, labelNames),
		streamUptime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "streamuptime",
			Help:      "Duration (in seconds) the active source client has been connected to this stream.",
		}, labelNames),
		client: &http.Client{
			Transport: &http.Transport{
				DisableCompression: true,
				Dial: func(netw, addr string) (net.Conn, error) {
					c, err := net.DialTimeout(netw, addr, timeout)
					if err != nil {
						return nil, err
					}
					if err := c.SetDeadline(time.Now().Add(timeout)); err != nil {
						return nil, err
					}
					return c, nil
				},
			},
		},
	}
}

// Describe describes all the metrics ever exported by the Shoutcast exporter. It
// implements prometheus.Collector.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.up.Desc()
	ch <- e.totalScrapes.Desc()
	ch <- e.jsonParseFailures.Desc()
	e.listeners.Describe(ch)
	e.streamUptime.Describe(ch)
}

// Collect fetches the stats from configured Shoutcast location and delivers them
// as Prometheus metrics. It implements prometheus.Collector.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	status := make(chan *ShoutcastStatus)
	go e.scrape(status)

	e.mutex.Lock() // To protect metrics from concurrent collects.
	defer e.mutex.Unlock()

	e.listeners.Reset()
	e.streamUptime.Reset()

	if s := <-status; s != nil {
		for _, stream := range s.Streams {
			e.listeners.WithLabelValues(strconv.Itoa(stream.ID), stream.Streampath).Set(float64(stream.Currentlisteners))
			e.streamUptime.WithLabelValues(strconv.Itoa(stream.ID), stream.Streampath).Set(float64(stream.Streamuptime))
			//log.Infof("stream id %d, path %s, listeners %d", stream.ID, stream.Streampath, stream.Currentlisteners)
		}
	}

	ch <- e.up
	ch <- e.totalScrapes
	ch <- e.jsonParseFailures
	e.listeners.Collect(ch)
	e.streamUptime.Collect(ch)
}

func (e *Exporter) scrape(status chan<- *ShoutcastStatus) {
	defer close(status)

	e.totalScrapes.Inc()

	resp, err := e.client.Get(e.URI)
	if err != nil {
		e.up.Set(0)
		log.Errorf("Can't scrape Shoutcast: %v", err)
		return
	}
	defer resp.Body.Close()
	e.up.Set(1)
	
	// Copy response body into intermediate buffer,
	// so we can deserialize twice
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	//log.Infof("got json: %s", bodyBytes)

	if err != nil {
		e.up.Set(0)
		log.Errorf("Can't read response body: %v", err)
		return
	}
	
	buf := bytes.NewBuffer(bodyBytes)
	//log.Infof("NewBuffer buf: %s", buf)
	var s ShoutcastStatus
	err = json.NewDecoder(buf).Decode(&s)

	if err != nil {
		log.Errorf("Can't read JSON: %v", err)
		e.jsonParseFailures.Inc()
		return
	}

	status <- &s
	//log.Infof("s is: %v", s)
}

func main() {
	var (
		listenAddress      = flag.String("web.listen-address", ":9240", "Address to listen on for web interface and telemetry.")
		metricsPath        = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
		shoutcastScrapeURI = flag.String("shoutcast.scrape-uri", "http://localhost:8000/statistics?json=1", "URI on which to scrape Shoutcast.")
		shoutcastTimeout   = flag.Duration("shoutcast.timeout", 5*time.Second, "Timeout for trying to get stats from Shoutcast.")
	)
	flag.Parse()

	// Listen to signals
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGTERM, syscall.SIGINT)

	exporter := NewExporter(*shoutcastScrapeURI, *shoutcastTimeout)
	prometheus.MustRegister(exporter)

	// Setup HTTP server
	http.Handle(*metricsPath, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
             <head><title>Shoutcast Exporter</title></head>
             <body>
             <h1>Shoutcast Exporter</h1>
             <p><a href='` + *metricsPath + `'>Metrics</a></p>
             </body>
             </html>`))
	})

	go func() {
		log.Infof("Starting Server: %s", *listenAddress)
		log.Fatal(http.ListenAndServe(*listenAddress, nil))
	}()

	s := <-sigchan
	log.Infof("Received %v, terminating", s)
	os.Exit(0)
}
