package config

import ctrl "sigs.k8s.io/controller-runtime"

type Config struct {
	ControllerManager ctrl.Manager
	ControllerConfig  ControllerConfig
}
