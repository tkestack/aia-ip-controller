module tkestack.io/aia-ip-controller

go 1.13

require (
	github.com/go-logr/logr v0.4.0
	github.com/spf13/cobra v1.2.1
	github.com/spf13/pflag v1.0.5
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cbs v1.0.240
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common v1.0.240
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cvm v1.0.240
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sts v1.0.240
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tag v1.0.240
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vpc v1.0.240
	k8s.io/api v0.22.1
	k8s.io/apimachinery v0.22.1
	k8s.io/client-go v0.22.1
	k8s.io/component-base v0.22.1
	k8s.io/klog/v2 v2.10.0
	sigs.k8s.io/controller-runtime v0.9.6
)
