package options

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
)

const (
	DefaultAiaIpControllerConfigYaml = "/app/conf/values.yaml"
	DefaultMaxConcurrentReconciles   = 1
	DefaultEnableReverseReconcile    = false
)

type ServingOptions struct {
	AiaConfigFilePath       string
	ClusterId               string
	HealthPort              int
	MaxConcurrentReconciles int
	EnableReverseReconcile  bool
}

// NewServingOptions returns serving configuration default values for aia-controller.
func NewServingOptions() *ServingOptions {
	return &ServingOptions{
		AiaConfigFilePath:       DefaultAiaIpControllerConfigYaml,
		MaxConcurrentReconciles: DefaultMaxConcurrentReconciles,
		EnableReverseReconcile:  DefaultEnableReverseReconcile,
	}
}

// AddFlags adds flags related to serving for controller to the specified FlagSet.
func (o *ServingOptions) AddFlags(fs *pflag.FlagSet) {
	if o == nil {
		return
	}

	fs.StringVar(&o.AiaConfigFilePath, "aia-conf-path", o.AiaConfigFilePath,
		"The config file path for aia ip controller to use")
	fs.StringVar(&o.ClusterId, "cluster-id", o.ClusterId,
		"The cluster id of tke cluster")
	fs.IntVar(&o.MaxConcurrentReconciles, "max-concurrent-reconcile", o.MaxConcurrentReconciles, "Max concurrent reconciles for aia controller")
	fs.BoolVar(&o.EnableReverseReconcile, "enable-reverse-reconcile", o.EnableReverseReconcile, "Enable reverse reconcile or not, default is false, means disable reverse reconcile")
}

// Validate checks validation of ServingOptions.
func (o *ServingOptions) Validate() []error {
	if o == nil {
		return nil
	}

	var errs []error
	if !strings.HasPrefix(o.ClusterId, "cls-") {
		errs = append(errs, fmt.Errorf("invalid clusterId %s, no (cls-) prefix", o.ClusterId))
	}
	return errs
}
