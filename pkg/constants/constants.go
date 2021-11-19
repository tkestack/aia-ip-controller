package constants

const (
	TkeNodeInsIdAnnoKey = "cloud.tencent.com/node-instance-id"

	EipTypeWanIp   = "WanIP"
	EipTypeCommon  = "EIP"
	EipTypeAnyCast = "AnycastEIP"

	// event reasons
	FailedAllocateAnycastIp  = "FailedAllocateAnycastIp"
	FailedAssociateAnycastIP = "FailedAssociateAnycastIp"
	AlreadyHasAnycastIp      = "AlreadyHasAnycastIp"
	FailedUntaintNode        = "FailedUntaintNode"

	// tag annotation key
	AiaIpControllerClusterUuidAnnoKey = "aia-official-cluster-uuid"
	AiaIpControllerClusterIdAnnoKey   = "aia-official-cluster-id"
	AiaNodeNameAnnoKey                = "aia-node-name"
	AiaNodeInsIdAnnoKey               = "aia-node-ins-id"

	// anycast eip id
	AnycastIdPrefix = "eip-"

	// anycast ip possible status
	AnycastStatusBIND   = "BIND"
	AnycastStatusUnBind = "UNBIND"

	// taint node key
	NoAnycastIpTaintKey   = "tke.cloud.tencent.com/no-aia-ip"
	NoAnycastIpTaintValue = "true"

	// anycast ip annotation
	AnycastIpIdAnnotationKey = "tke.cloud.tencent.com/anycast-ip-id"
	AnycastIpIpAnnotationKey = "tke.cloud.tencent.com/anycast-ip-address"

	// Credential env key
	ClusterIdEnvKey = "AIA_CLUSTER_ID"
	AppIdEnvKey     = "AIA_APP_ID"
	SecretIdEnvKey  = "AIA_SECRET_ID"
	SecretKeyEnvKey = "AIA_SECRET_KEY"
)
