package main

import (
	"github.com/jacksontj/promxy/promxy"
	"github.com/jessevdk/go-flags"
	"github.com/prometheus/prometheus/promql"
	"github.com/sirupsen/logrus"
)

//http://localhost:8080/api/v1/query?query=scrape_duration_seconds%5B1m%5D&time=1507256489.103&_=1507256486365

var engine promql.Engine

var opts struct {
	ConfigFile string `long:"config" description:"path to the config file" required:"true"`
}

func main() {
	parser := flags.NewParser(&opts, flags.Default)
	if _, err := parser.Parse(); err != nil {
		logrus.Fatalf("Error parsing flags: %v", err)
	}

	config, err := promxy.ConfigFromFile(opts.ConfigFile)
	if err != nil {
		logrus.Fatalf("Error loading config: %v", err)
	}

	p, err := promxy.NewProxy(config)
	if err != nil {
		logrus.Fatalf("Error creating proxy: %v", err)
	}

	if err := p.ListenAndServe(); err != nil {
		logrus.Fatalf("Err: %v", err)
	}
}
