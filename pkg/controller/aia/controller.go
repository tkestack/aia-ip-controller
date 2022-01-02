package aia

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	cvm "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cvm/v20170312"
	tag "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tag/v20180813"
	vpc "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vpc/v20170312"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"tkestack.io/aia-ip-controller/cmd/aia-ip-controller/app/config"
	"tkestack.io/aia-ip-controller/pkg/constants"
)

// reconcile reconciles to operate on the relationship between nodes and anycast ip
type reconciler struct {
	k8sClient               client.Client
	eventRecorder           record.EventRecorder
	logger                  logr.Logger
	maxConcurrentReconciles int
	syncPeriod              time.Duration
	clusterId               string
	clusterUuid             string
	Conf                    *config.YamlValueConfig
	vpcClient               *vpc.Client
	cvmClient               *cvm.Client
	tagClient               *tag.Client
	AiaManger               Manger
	isLeader                bool
	EnableReverseReconcile  bool
}

func NewReconcile(k8sClient client.Client, eventRecorder record.EventRecorder, controllerConfig *config.ControllerConfig, logger logr.Logger) (*reconciler, error) {

	credential := common.NewCredential(controllerConfig.ConfigFileConf.Credential.SecretID, controllerConfig.ConfigFileConf.Credential.SecretKey)
	vpcClient, cErr := vpc.NewClient(credential, controllerConfig.ConfigFileConf.Region.LongName, profile.NewClientProfile())
	if cErr != nil {
		return nil, cErr
	}

	cvmClient, cErr := cvm.NewClient(credential, controllerConfig.ConfigFileConf.Region.LongName, profile.NewClientProfile())
	if cErr != nil {
		return nil, cErr
	}

	tagClient, cErr := tag.NewClient(credential, controllerConfig.ConfigFileConf.Region.LongName, profile.NewClientProfile())
	if cErr != nil {
		return nil, cErr
	}

	aiaManager, aErr := NewAiaManager(k8sClient, cvmClient, vpcClient, tagClient, eventRecorder,
		controllerConfig.ConfigFileConf.Credential.ClusterID, controllerConfig.ConfigFileConf.Aia.Bandwidth,
		controllerConfig.ConfigFileConf.Aia.AnycastZone, controllerConfig.ConfigFileConf.Aia.AddressType)
	if aErr != nil {
		klog.Errorf("NewAiaManager failed, err: %v", aErr)
		return nil, aErr
	}

	clsUuid, gErr := aiaManager.GetOrCreateClusterUuidInCm()
	if gErr != nil {
		klog.Errorf("GetOrCreateClusterUuidInCm failed, err: %v", gErr)
		return nil, gErr
	}

	return &reconciler{
		k8sClient:               k8sClient,
		eventRecorder:           eventRecorder,
		logger:                  logger,
		maxConcurrentReconciles: controllerConfig.MaxAiaIpControllerConcurrentReconciles,
		clusterId:               controllerConfig.ConfigFileConf.Credential.ClusterID,
		clusterUuid:             clsUuid,
		Conf:                    controllerConfig.ConfigFileConf,
		isLeader:                false,
		vpcClient:               vpcClient,
		cvmClient:               cvmClient,
		tagClient:               tagClient,
		AiaManger:               aiaManager,
		EnableReverseReconcile:  controllerConfig.EnableReverseReconcile,
	}, nil
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// set up a convenient log object so we don't have to type request over and over again
	log := log.FromContext(ctx)

	r.isLeader = true // let reverse reconcile loop know this

	// Fetch the node from the cache
	node := &corev1.Node{}
	err := r.k8sClient.Get(ctx, req.NamespacedName, node)
	if errors.IsNotFound(err) {
		log.Error(nil, fmt.Sprintf("Could not find node %s", req.Name))
		// call tag api to check if any anycast ip or eip related with the deleted node
		found, legacyAnycastId, err := r.AiaManger.GetAnycastIpByTags(req.Name)
		if err != nil {
			klog.Errorf("GetAnycastIpByTags of node %s failed, err: %v", req.Name, err)
			return reconcile.Result{}, err
		}
		if !found {
			klog.Infof("found node %s has no legacy anycast ip, just skip it", req.Name)
			return reconcile.Result{}, nil
		}
		// if legacy anycast ip found, need to disassociate it
		if err := r.AiaManger.DisassociateAnycastIp(legacyAnycastId); err != nil {
			klog.Errorf("DisassociateAnycastIp %s failed, err: %v", legacyAnycastId, err)
			return reconcile.Result{}, err
		}
		// release it if necessary
		if err := r.AiaManger.ReleaseAnycastIp(legacyAnycastId); err != nil {
			klog.Errorf("ReleaseAnycastIp %s failed, err: %v", legacyAnycastId, err)
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil
	}
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("could not fetch node %s: %+v", req.Name, err)
	}

	cvmInsId := node.Labels[constants.TkeNodeInsIdAnnoKey]
	log.V(2).Info("Reconciling node...", "nodeIns", cvmInsId, "nodePhase", node.Status.Phase, "maxConcurrentReconciles", r.maxConcurrentReconciles)

	// process node according to its status phase
	switch node.Status.Phase {
	case corev1.NodeTerminated:
		// if the node terminating, do nothing, we will disassociate and release anycast ip after it has been removed from cluster
		return reconcile.Result{}, fmt.Errorf("found node %s is %s, keep return err", node.Name, corev1.NodeTerminated)
	default:
		isAllocate, err := r.AiaManger.IsCvmNeedToAllocateAnyCastIp(node)
		if err != nil {
			klog.Errorf("check IsCvmNeedToAllocateAnyCastIp for node %s failed, err: %v", node.Name, err)
			return reconcile.Result{}, err
		}
		// if no need to allocate and associate, just return
		if !isAllocate {
			klog.Infof("no need to allocate and associate anycast ip for node %s, just return nil", node.Name)
			return reconcile.Result{}, nil
		}
		anycastId, err := r.AiaManger.AllocateAnycastIp(node, r.Conf.Aia.Tags)
		if err != nil {
			klog.Errorf("AllocateAnycastIp for node %s failed, err: %v", node.Name, err)
			return reconcile.Result{}, err
		}
		if err := r.AiaManger.AssociateAnycastIp(node, anycastId); err != nil {
			klog.Errorf("AssociateAnycastIp %s for node %s failed, err: %v", anycastId, node.Name, err)
			return reconcile.Result{}, err
		}
		klog.Infof("associate anycast ip %s for node %s success", anycastId, node.Name)
	}

	log.V(2).Info("Reconcile node successfully", "nodeName", req.Name)
	return reconcile.Result{}, nil
}
