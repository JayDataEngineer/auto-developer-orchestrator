package observability

import (
	"context"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
	"go.uber.org/zap"
)

// MetricsPusher periodically pushes metrics to a Prometheus Pushgateway.
type MetricsPusher struct {
	pusher  *push.Pusher
	logger  *zap.Logger
	done    chan struct{}
	jobName string
}

// NewMetricsPusher creates a background pusher for the given metrics registry.
// Set PROMETHEUS_PUSHGATEWAY_URL to enable (defaults to cluster Pushgateway).
func NewMetricsPusher(gatherer prometheus.Gatherer, logger *zap.Logger) *MetricsPusher {
	url := os.Getenv("PROMETHEUS_PUSHGATEWAY_URL")

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	jobName := "orchestrator"
	p := push.New(url, jobName).
		Grouping("instance", hostname).
		Gatherer(gatherer)

	return &MetricsPusher{
		pusher:  p,
		logger:  logger,
		done:    make(chan struct{}),
		jobName: jobName,
	}
}

// Start begins pushing metrics every 30 seconds.
func (mp *MetricsPusher) Start() {
	// Test connection first — silent if unreachable
	if err := mp.pusher.PushContext(context.Background()); err != nil {
		if mp.logger != nil {
			mp.logger.Debug("Prometheus Pushgateway not reachable — metrics at /metrics only",
				zap.String("job", mp.jobName), zap.Error(err))
		}
	} else if mp.logger != nil {
		mp.logger.Info("Prometheus pusher connected", zap.String("job", mp.jobName))
	}

	go mp.loop()
}

// Stop stops the push loop and does a final push.
func (mp *MetricsPusher) Stop() {
	close(mp.done)
	_ = mp.pusher.PushContext(context.Background())
}

func (mp *MetricsPusher) loop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = mp.pusher.PushContext(context.Background())
		case <-mp.done:
			return
		}
	}
}
