package options

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"os"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	"tkestack.io/aia-ip-controller/cmd/aia-ip-controller/app/config"
	"tkestack.io/aia-ip-controller/pkg/constants"
)

// ControllerOptions is the main context object for the aia-ip-controller.
type ControllerOptions struct {
	Generic        *GenericOptions
	Serving        *ServingOptions
	LeaderElection *LeaderElectionOptions
}

// NewControllerOptions creates a new ControllerOptions with a default config.
func NewControllerOptions() *ControllerOptions {
	return &ControllerOptions{
		Generic:        NewGenericOptions(),
		Serving:        NewServingOptions(),
		LeaderElection: NewLeaderElectionOptions(),
	}
}

// Validate is used to validate the options and config before launching the controller.
func (o *ControllerOptions) Validate() error {
	var errs []error

	errs = append(errs, o.Generic.Validate()...)

	return utilerrors.NewAggregate(errs)
}

// Flags returns flags for a specific APIServer of Kubernetes by section name.
func (o *ControllerOptions) Flags() cliflag.NamedFlagSets {
	fss := cliflag.NamedFlagSets{}

	o.Generic.AddFlags(fss.FlagSet("generic"))
	o.Serving.AddFlags(fss.FlagSet("serving"))
	o.LeaderElection.AddFlags(fss.FlagSet("leader-election"))

	return fss
}

// Config return a controller config objective.
func (o ControllerOptions) Config() (*config.Config, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}

	// setup logger for controller runtime
	ctrl.SetLogger(klogr.New())

	// setup scheme
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := rbacv1.AddToScheme(scheme); err != nil {
		return nil, err
	}

	// read configFile
	yamlFile, err := ioutil.ReadFile(o.Serving.AiaConfigFilePath)
	if err != nil {
		return nil, err
	}
	klog.V(2).Infof("read config file from path %s, content: %s", o.Serving.AiaConfigFilePath, string(yamlFile))

	decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(yamlFile), 100)
	var confVal config.YamlValueConfig
	if err = decoder.Decode(&confVal); err != nil {
		return nil, err
	}
	confValB, err := json.Marshal(&confVal)
	if err != nil {
		return nil, err
	}
	// override credential para if env set
	if os.Getenv(constants.ClusterIdEnvKey) != "" {
		confVal.Credential.ClusterID = os.Getenv(constants.ClusterIdEnvKey)
	}
	if os.Getenv(constants.AppIdEnvKey) != "" {
		confVal.Credential.AppID = os.Getenv(constants.AppIdEnvKey)
	}
	if os.Getenv(constants.SecretIdEnvKey) != "" {
		confVal.Credential.SecretID = os.Getenv(constants.SecretIdEnvKey)
	}
	if os.Getenv(constants.SecretKeyEnvKey) != "" {
		confVal.Credential.SecretKey = os.Getenv(constants.SecretKeyEnvKey)
	}

	klog.V(4).Infof("conf parsed(with env override): %s", string(confValB))
	if err := confVal.Validate(); err != nil {
		klog.Errorf("generate conf for aia-ip-controller failed, err: %v")
		return nil, err
	}

	restConfig := ctrl.GetConfigOrDie()
	// customize qps and burst
	restConfig.QPS = o.Generic.QPS
	restConfig.Burst = o.Generic.Burst
	restConfig.ContentType = o.Generic.ContentType
	klog.V(4).Infof("controller kube client use qps %v, burst %v", o.Generic.QPS, o.Generic.Burst)
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:             scheme,
		LeaderElection:     true,
		LeaderElectionID:   confVal.Controller.ResourceLockName,
		LeaseDuration:      &o.LeaderElection.LeaseDuration,
		RenewDeadline:      &o.LeaderElection.RenewDeadline,
		RetryPeriod:        &o.LeaderElection.RetryPeriod,
		MetricsBindAddress: "0",
	})
	if err != nil {
		return nil, err
	}

	c := &config.Config{
		ControllerManager: mgr,
	}
	c.ControllerConfig.AiaConfigFilePath = o.Serving.AiaConfigFilePath
	c.ControllerConfig.MaxAiaIpControllerConcurrentReconciles = o.Serving.MaxConcurrentReconciles
	c.ControllerConfig.EnableReverseReconcile = o.Serving.EnableReverseReconcile
	c.ControllerConfig.ConfigFileConf = &confVal
	return c, nil
}
