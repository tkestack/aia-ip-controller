package config

import (
	"fmt"
	"strings"
)

// ControllerConfig contains the controller configuration.
type ControllerConfig struct {
	AiaConfigFilePath                      string
	ConfigFileConf                         *YamlValueConfig
	MaxAiaIpControllerConcurrentReconciles int
	EnableReverseReconcile                 bool
}

type InternalControllerConfig struct {
	ResourceLockName string `yaml:"resourceLockName"`
}

type RegionConfig struct {
	ShortName string `yaml:"shortName"`
	LongName  string `yaml:"longName"`
}

// CredentialConfig para will be override if env set
type CredentialConfig struct {
	ClusterID string `yaml:"clusterID"`
	AppID     string `yaml:"appID"`
	SecretID  string `yaml:"secretID"`
	SecretKey string `yaml:"secretKey"`
}

type AiaConfig struct {
	Tags        map[string]string `yaml:"tags"`
	Bandwidth   int64             `yaml:"bandwidth"`
	AnycastZone string            `yaml:"anycastZone"`
	AddressType string            `yaml:"addressType"`
}

type NodeConfig struct {
	Labels map[string]string `yaml:"labels"`
}

type YamlValueConfig struct {
	Controller InternalControllerConfig `yaml:"controller"`
	Region     RegionConfig             `yaml:"region"`
	Credential CredentialConfig         `yaml:"credential"`
	Aia        AiaConfig                `yaml:"aia"`
	Node       NodeConfig               `yaml:"node"`
}

func (y *YamlValueConfig) Validate() error {
	if !strings.HasPrefix(y.Credential.ClusterID, "cls-") {
		return fmt.Errorf("invalid cluster id %s", y.Credential.ClusterID)
	}
	if y.Credential.SecretID == "" || y.Credential.SecretKey == "" {
		return fmt.Errorf("invalid secret id or secret key")
	}
	return nil
}
