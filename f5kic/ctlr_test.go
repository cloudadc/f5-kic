package main

import (
	"context"
	"testing"

	"github.com/f5devcentral/f5-bigip-rest-go/utils"
	"github.com/prometheus/client_golang/prometheus"
)

func init() {
	collectors = PrometheusCollectors{
		DeployRequestTimeCostCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "deploy_request_timecost_count",
				Help: "numbers of deploy requests grouped by kind and operation",
			},
			[]string{"kind", "operation"},
		),
		DeployRequestTimeCostTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "deploy_request_timecost_total",
				Help: "time cost total(in milliseconds) of deploy requests grouped by kind and operation",
			},
			[]string{"kind", "operation"},
		),
	}
	pendingDeploys = utils.NewDeployQueue()
}

func Test_waitAndHandle(t *testing.T) {

	pendingDeploys.Add(DeployRequest{
		Context: context.TODO(),
	})
	tests := []struct {
		name string
	}{
		// TODO: Add test cases.
		{name: "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			waitAndHandle()
		})
	}
}
