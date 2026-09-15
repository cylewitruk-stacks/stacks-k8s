package bitcoinobserver

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metric names are the exporter's fixed Prometheus wire contract.
const (
	metricObserverSuccess                     = "bitcoin_observer_success"
	metricObserverLastSuccessTimestampSeconds = "bitcoin_observer_last_success_timestamp_seconds"
	metricObserverLastAttemptTimestampSeconds = "bitcoin_observer_last_attempt_timestamp_seconds"
	metricObserverPollErrorsTotal             = "bitcoin_observer_poll_errors_total"
	metricBlockHeight                         = "bitcoin_block_height"
	metricHeaderHeight                        = "bitcoin_header_height"
	metricInitialBlockDownload                = "bitcoin_initial_block_download"
	metricChainTips                           = "bitcoin_chain_tips"
	metricConnections                         = "bitcoin_connections"
	metricMempoolTransactions                 = "bitcoin_mempool_transactions"
	metricMempoolBytes                        = "bitcoin_mempool_bytes"
	metricMempoolUsageBytes                   = "bitcoin_mempool_usage_bytes"
	metricNetworkReceivedBytesTotal           = "bitcoin_network_received_bytes_total"
	metricNetworkSentBytesTotal               = "bitcoin_network_sent_bytes_total"
)

// metrics contains stable numerical names only; hashes and branch identities belong in records.
var metrics = map[string]*prometheus.Desc{
	metricObserverSuccess: prometheus.NewDesc(
		metricObserverSuccess,
		"Latest complete poll succeeded and is fresh (not node health).",
		nil,
		nil,
	),
	metricObserverLastSuccessTimestampSeconds: prometheus.NewDesc(
		metricObserverLastSuccessTimestampSeconds,
		"Completion time of the last successful poll, or zero before success.",
		nil,
		nil,
	),
	metricObserverLastAttemptTimestampSeconds: prometheus.NewDesc(
		metricObserverLastAttemptTimestampSeconds,
		"Completion time of the last attempt, or zero before polling.",
		nil,
		nil,
	),
	metricObserverPollErrorsTotal: prometheus.NewDesc(
		metricObserverPollErrorsTotal,
		"Failed complete polling attempts in this exporter process.",
		nil,
		nil,
	),
	metricBlockHeight: prometheus.NewDesc(
		metricBlockHeight,
		"Selected block height observed by this node.",
		nil,
		nil,
	),
	metricHeaderHeight: prometheus.NewDesc(
		metricHeaderHeight,
		"Best known header height observed by this node.",
		nil,
		nil,
	),
	metricInitialBlockDownload: prometheus.NewDesc(
		metricInitialBlockDownload,
		"Core initial block download flag.",
		nil,
		nil,
	),
	metricChainTips: prometheus.NewDesc(
		metricChainTips,
		"Number of branch tips returned by Core.",
		nil,
		nil,
	),
	metricConnections: prometheus.NewDesc(
		metricConnections,
		"Current peer connections.",
		nil,
		nil,
	),
	metricMempoolTransactions: prometheus.NewDesc(
		metricMempoolTransactions,
		"Current mempool transaction count.",
		nil,
		nil,
	),
	metricMempoolBytes: prometheus.NewDesc(
		metricMempoolBytes,
		"Current mempool transaction bytes.",
		nil,
		nil,
	),
	metricMempoolUsageBytes: prometheus.NewDesc(
		metricMempoolUsageBytes,
		"Current mempool dynamic memory usage.",
		nil,
		nil,
	),
	metricNetworkReceivedBytesTotal: prometheus.NewDesc(
		metricNetworkReceivedBytesTotal,
		"Bytes received since this Core process started.",
		nil,
		nil,
	),
	metricNetworkSentBytesTotal: prometheus.NewDesc(
		metricNetworkSentBytesTotal,
		"Bytes sent since this Core process started.",
		nil,
		nil,
	),
}

// Describe implements prometheus.Collector with a fixed metric vocabulary.
func (o *Observer) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range metrics {
		ch <- desc
	}
}

// Collect serves the cached sample without RPC; stale or failed protocol values are omitted.
func (o *Observer) Collect(ch chan<- prometheus.Metric) {
	o.mu.RLock()
	record, facts, lastSuccess, failures := o.record, o.facts, o.lastSuccess, o.failures
	o.mu.RUnlock()
	fresh := record.Success && o.now().Sub(record.ObservedAt) <= o.interval+PollTimeout
	emit := func(name string, value float64, kind prometheus.ValueType) {
		ch <- prometheus.MustNewConstMetric(metrics[name], kind, value)
	}
	emit(metricObserverSuccess, boolean(fresh), prometheus.GaugeValue)
	emit(metricObserverLastSuccessTimestampSeconds, timestamp(lastSuccess), prometheus.GaugeValue)
	emit(metricObserverLastAttemptTimestampSeconds, timestamp(record.ObservedAt), prometheus.GaugeValue)
	emit(metricObserverPollErrorsTotal, float64(failures), prometheus.CounterValue)
	if !fresh {
		return
	}
	for name, value := range map[string]float64{
		metricBlockHeight: float64(record.Chain.Blocks), metricHeaderHeight: float64(record.Chain.Headers),
		metricInitialBlockDownload: boolean(record.Chain.InitialBlockDownload),
		metricChainTips:            float64(len(record.Tips)),
		metricConnections:          float64(facts.Connections), metricMempoolTransactions: float64(facts.Size),
		metricMempoolBytes: float64(facts.Bytes), metricMempoolUsageBytes: float64(facts.Usage),
	} {
		emit(name, value, prometheus.GaugeValue)
	}
	emit(metricNetworkReceivedBytesTotal, float64(facts.TotalBytesRecv), prometheus.CounterValue)
	emit(metricNetworkSentBytesTotal, float64(facts.TotalBytesSent), prometheus.CounterValue)
}

// boolean projects a native boolean to a numerical gauge.
func boolean(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// timestamp preserves the explicit never-observed sentinel.
func timestamp(t time.Time) float64 {
	if t.IsZero() {
		return 0
	}
	return float64(t.UnixNano()) / 1e9
}
