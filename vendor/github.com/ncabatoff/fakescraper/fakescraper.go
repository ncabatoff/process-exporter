package fakescraper

import (
	"bytes"
	"github.com/prometheus/client_golang/prometheus"
	"log"
	"net/http"
)

type (
	dummyResponseWriter struct {
		bytes.Buffer
		header http.Header
	}
	FakeScraper struct {
		dummyResponseWriter
	}
)

func (d *dummyResponseWriter) Header() http.Header {
	return d.header
}

func (d *dummyResponseWriter) WriteHeader(code int) {
}

func NewFakeScraper() *FakeScraper {
	return &FakeScraper{dummyResponseWriter{header: make(http.Header)}}
}

// Ask prometheus to handle a scrape request so we can capture and return the output.
func (fs *FakeScraper) Scrape() string {
	httpreq, err := http.NewRequest("GET", "/metrics", nil)
	if err != nil {
		log.Fatalf("Error building request: %v", err)
	}

	prometheus.Handler().ServeHTTP(&fs.dummyResponseWriter, httpreq)
	s := fs.String()
	fs.Truncate(0)
	return s
}
