package cmd

import (
	"context"
	"net/http"

	"github.com/minio/radio/cmd/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequestsDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "s3_ttfb_seconds",
			Help:    "Time taken by requests served by current Radio server instance",
			Buckets: []float64{.05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"api"},
	)
	minioVersionInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "minio",
			Name:      "version_info",
			Help:      "Version of current Radio server instance",
		},
		[]string{
			// current version
			"version",
			// commit-id of the current version
			"commit",
		},
	)
)

func init() {
	prometheus.MustRegister(httpRequestsDuration)
	prometheus.MustRegister(newMinioCollector())
	prometheus.MustRegister(minioVersionInfo)
}

// newMinioCollector describes the collector
// and returns reference of minioCollector
// It creates the Prometheus Description which is used
// to define metric and  help string
func newMinioCollector() *minioCollector {
	return &minioCollector{
		desc: prometheus.NewDesc("minio_stats", "Statistics exposed by Radio server", nil, nil),
	}
}

// minioCollector is the Custom Collector
type minioCollector struct {
	desc *prometheus.Desc
}

// Describe sends the super-set of all possible descriptors of metrics
func (c *minioCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

// Collect is called by the Prometheus registry when collecting metrics.
func (c *minioCollector) Collect(ch chan<- prometheus.Metric) {

	// Expose Radio's version information
	minioVersionInfo.WithLabelValues(Version, CommitID).Set(float64(1.0))

	// Fetch disk space info
	objLayer := newObjectLayerFn()
	// Service not initialized yet
	if objLayer == nil {
		return
	}

	connStats := globalConnStats.toServerConnStats()

	// Network Sent/Received Bytes (Outbound)
	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc(
			prometheus.BuildFQName("s3", "tx", "bytes_total"),
			"Total number of s3 bytes sent by current Radio server instance",
			nil, nil),
		prometheus.CounterValue,
		float64(connStats.S3OutputBytes),
	)

	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc(
			prometheus.BuildFQName("s3", "rx", "bytes_total"),
			"Total number of s3 bytes received by current Radio server instance",
			nil, nil),
		prometheus.CounterValue,
		float64(connStats.S3InputBytes),
	)

	httpStats := globalHTTPStats.toServerHTTPStats()

	for api, value := range httpStats.CurrentS3Requests.APIStats {
		ch <- prometheus.MustNewConstMetric(
			prometheus.NewDesc(
				prometheus.BuildFQName("s3", "requests", "current"),
				"Total number of running s3 requests in current Radio server instance",
				[]string{"api"}, nil),
			prometheus.CounterValue,
			float64(value),
			api,
		)
	}

	for api, value := range httpStats.TotalS3Requests.APIStats {
		ch <- prometheus.MustNewConstMetric(
			prometheus.NewDesc(
				prometheus.BuildFQName("s3", "requests", "total"),
				"Total number of s3 requests in current Radio server instance",
				[]string{"api"}, nil),
			prometheus.CounterValue,
			float64(value),
			api,
		)
	}

	for api, value := range httpStats.TotalS3Errors.APIStats {
		ch <- prometheus.MustNewConstMetric(
			prometheus.NewDesc(
				prometheus.BuildFQName("s3", "errors", "total"),
				"Total number of s3 errors in current Radio server instance",
				[]string{"api"}, nil),
			prometheus.CounterValue,
			float64(value),
			api,
		)
	}
	// Cache related metrics
	if globalCacheConfig.Enabled {
		ch <- prometheus.MustNewConstMetric(
			prometheus.NewDesc(
				prometheus.BuildFQName("cache", "hits", "total"),
				"Total number of disk cache hits in current Radio instance",
				nil, nil),
			prometheus.CounterValue,
			float64(newCachedObjectLayerFn().CacheStats().getHits()),
		)
		ch <- prometheus.MustNewConstMetric(
			prometheus.NewDesc(
				prometheus.BuildFQName("cache", "misses", "total"),
				"Total number of disk cache misses in current Radio instance",
				nil, nil),
			prometheus.CounterValue,
			float64(newCachedObjectLayerFn().CacheStats().getMisses()),
		)
		ch <- prometheus.MustNewConstMetric(
			prometheus.NewDesc(
				prometheus.BuildFQName("cache", "data", "served"),
				"Total number of bytes served from cache of current Radio instance",
				nil, nil),
			prometheus.CounterValue,
			float64(newCachedObjectLayerFn().CacheStats().getBytesServed()),
		)
	}
}

func metricsHandler() http.Handler {

	registry := prometheus.NewRegistry()

	err := registry.Register(minioVersionInfo)
	logger.LogIf(context.Background(), err)

	err = registry.Register(httpRequestsDuration)
	logger.LogIf(context.Background(), err)

	err = registry.Register(newMinioCollector())
	logger.LogIf(context.Background(), err)

	gatherers := prometheus.Gatherers{
		prometheus.DefaultGatherer,
		registry,
	}
	// Delegate http serving to Prometheus client library, which will call collector.Collect.
	return promhttp.InstrumentMetricHandler(
		registry,
		promhttp.HandlerFor(gatherers,
			promhttp.HandlerOpts{
				ErrorHandling: promhttp.ContinueOnError,
			}),
	)

}
