package recurse

import (
	"github.com/coredns/coredns/plugin"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	recurseDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:                   plugin.Namespace,
		Subsystem:                   pluginName,
		Name:                        "recurse_duration_seconds",
		Buckets:                     plugin.TimeBuckets,
		NativeHistogramBucketFactor: plugin.NativeHistogramBucketFactor,
		Help:                        "Histogram of the time each recurse took.",
	}, []string{"to"})
)
