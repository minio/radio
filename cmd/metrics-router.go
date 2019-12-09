package cmd

import (
	"github.com/gorilla/mux"
)

const (
	prometheusMetricsPath = "/prometheus/metrics"
)

// registerMetricsRouter - add handler functions for metrics.
func registerMetricsRouter(router *mux.Router) {
	// metrics router
	metricsRouter := router.NewRoute().PathPrefix(minioReservedBucketPath).Subrouter()
	metricsRouter.Handle(prometheusMetricsPath, metricsHandler())
}
