package aia

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	cvm "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cvm/v20170312"
	tag "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tag/v20180813"
	vpc "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vpc/v20170312"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"tkestack.io/aia-ip-controller/pkg/constants"
)

// Manger is not only take care of aia ip staff, but also HighQualityEIP
type Manger interface {
	ProcessingEipType() string
	IsAiaNode(labels map[string]string, node *corev1.Node) bool
	IsCvmNeedToAllocateAnyCastIp(node *corev1.Node) (bool, error)
	GetAnycastIpByTags(nodeName string) (bool, string, error)
	GetOrCreateClusterUuidInCm() (string, error)
	AllocateAnycastIp(node *corev1.Node, additionalTags map[string]string) (string, error)
	AssociateAnycastIp(node *corev1.Node, anycastIpId string) error
	DisassociateAnycastIp(anycastIpId string) error
	ReleaseAnycastIp(anycastIpId string) error
}

const (
	tagDuplicateErrCode     = "TagDuplicate"
	tagNotExistedErrCode    = "InvalidTag.NotExisted"
	newTagNotExistedErrCode = "InvalidParameterValue.TagNotExisted"
)

type MangerImp struct {
	cvmClient        *cvm.Client
	vpcClient        *vpc.Client
	tagClient        *tag.Client
	eventRecorder    record.EventRecorder
	clusterId        string
	clusterUuid      string
	bandwidth        int64
	anycastZone      string
	addressType      string
	k8sClient        client.Client
	k8sNoCacheClient clientset.Interface
}

func NewAiaManager(
	k8sClient client.Client,
	cvmClient *cvm.Client,
	vpcClient *vpc.Client,
	tagClient *tag.Client,
	record record.EventRecorder,
	clusterId string,
	bandwidth int64,
	anycastZone string,
	addressType string,
) (Manger, error) {

	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	kubeClient, err := clientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &MangerImp{
		cvmClient:        cvmClient,
		vpcClient:        vpcClient,
		tagClient:        tagClient,
		eventRecorder:    record,
		clusterId:        clusterId,
		bandwidth:        bandwidth,
		k8sClient:        k8sClient,
		anycastZone:      anycastZone,
		addressType:      addressType,
		k8sNoCacheClient: kubeClient,
	}, nil
}

// ProcessingEipType return eip type that this controller processing, default is AnycastEIP
func (m *MangerImp) ProcessingEipType() string {
	if m.addressType != "" {
		return m.addressType
	}
	return constants.EipTypeAnyCast
}

// IsAiaNode will check if a node has labels that user specify in config file
func (m *MangerImp) IsAiaNode(labels map[string]string, node *corev1.Node) bool {
	cvmInsId := node.Labels[constants.TkeNodeInsIdAnnoKey]
	if cvmInsId == "" {
		klog.V(4).Infof("node %s has no ins id in label yet, not going to process")
		return false
	}

	for k, v := range labels {
		if node.Labels[k] != v {
			klog.V(2).Infof("node %s has no aip or eip label(%s:%s), just skip it", node.Name, k, v)
			return false
		}
	}
	return true
}

func (m *MangerImp) GetAnycastIpByTags(nodeName string) (bool, string, error) {
	descTagReq := tag.NewDescribeResourcesByTagsRequest()
	descTagReq.TagFilters = []*tag.TagFilter{
		{
			TagKey:   common.StringPtr(constants.AiaIpControllerClusterUuidAnnoKey),
			TagValue: common.StringPtrs([]string{m.clusterUuid}),
		},
		{
			TagKey:   common.StringPtr(constants.AiaNodeNameAnnoKey),
			TagValue: common.StringPtrs([]string{nodeName}),
		},
	}
	descTagResp, err := m.tagClient.DescribeResourcesByTags(descTagReq)
	if err != nil {
		return false, "", err
	}
	if descTagResp == nil || descTagResp.Response == nil {
		return false, "", fmt.Errorf("describe resouces but tags has no response")
	}
	if len(descTagResp.Response.Rows) < 1 {
		klog.V(2).Infof("get anycast ip using tags(%s:%s, %s:%s) found no resource, requestId %s",
			constants.AiaIpControllerClusterUuidAnnoKey, m.clusterUuid, constants.AiaNodeNameAnnoKey, nodeName, *descTagResp.Response.RequestId)
		return false, "", nil
	}
	return true, *descTagResp.Response.Rows[0].ResourceId, nil
}

func (m *MangerImp) AllocateAnycastIp(node *corev1.Node, additionalTags map[string]string) (string, error) {
	cvmInsId := node.Labels[constants.TkeNodeInsIdAnnoKey]

	// 1. call tag api to find existed anycast ip
	anycastFound, foundAnycastId, err := m.GetAnycastIpByTags(node.Name)
	if err != nil {
		return "", err
	}
	if anycastFound {
		return foundAnycastId, nil
	}

	klog.V(2).Infof("describe resources by tags has no resource, going to create a new anycast ip")

	// 2. call vpc to create a new one
	allocateReq := vpc.NewAllocateAddressesRequest()
	allocateReq.AddressName = common.StringPtr(fmt.Sprintf("%s-aia", m.clusterId))
	allocateReq.AddressType = common.StringPtr(m.ProcessingEipType())
	if m.anycastZone != "" {
		allocateReq.AnycastZone = common.StringPtr(m.anycastZone)
	}
	if m.bandwidth > 0 {
		allocateReq.InternetMaxBandwidthOut = common.Int64Ptr(m.bandwidth)
	}

	tagKeyValMap := map[string]string{
		constants.AiaIpControllerClusterUuidAnnoKey: m.clusterUuid,
		constants.AiaIpControllerClusterIdAnnoKey:   m.clusterId,
		constants.AiaNodeNameAnnoKey:                node.Name,
		constants.AiaNodeInsIdAnnoKey:               cvmInsId,
	}
	for k, v := range additionalTags {
		tagKeyValMap[k] = v
	}

	for k, v := range tagKeyValMap {
		allocateReq.Tags = append(allocateReq.Tags, &vpc.Tag{
			Key:   common.StringPtr(k),
			Value: common.StringPtr(v),
		})
	}

	allocateResp, err := m.vpcClient.AllocateAddresses(allocateReq)
	if err != nil {
		klog.Warningf("allocate addresses for node %s failed, err: %v.", node.Name, err)
		// try to create tag key and value, because eip api do not support auto create tag.
		// and the stupid tag create api is cannot reentry, so we have to create tag here...
		if !strings.Contains(err.Error(), tagNotExistedErrCode) && !strings.Contains(err.Error(), newTagNotExistedErrCode) {
			eventStr := strings.Split(err.Error(), ", RequestId")[0]
			m.eventRecorder.Eventf(node, corev1.EventTypeWarning, constants.FailedAllocateAnycastIp, fmt.Sprintf("Failed to allocate Anycast ip (will retry): %s", eventStr))
			// event if error not container tag not exist code, we will still try to create tag, in case vpc api change error code
		}
		for k, v := range tagKeyValMap {
			reqCreateTag := tag.NewCreateTagRequest()
			reqCreateTag.TagKey = common.StringPtr(k)
			reqCreateTag.TagValue = common.StringPtr(v)
			_, tagCreateErr := m.tagClient.CreateTag(reqCreateTag)
			if tagCreateErr != nil && !strings.Contains(tagCreateErr.Error(), tagDuplicateErrCode) {
				rB, _ := json.Marshal(reqCreateTag)
				klog.Errorf("create tag failed, createTag req: %s, err: %v", string(rB), tagCreateErr)
				m.eventRecorder.Eventf(node, corev1.EventTypeWarning, constants.FailedAllocateAnycastIp, "Failed to allocate anycast ip (will retry): %s", err.Error())
				return "", fmt.Errorf("DescribeResourcesByTags failed: %s.  CreateTag failed: %s", err.Error(), tagCreateErr.Error())
			}
		}
		// create tag
		// make sure outside loop will describe tag again, if query tag resource not exist at first
		return "", err
	}
	if allocateResp == nil || allocateResp.Response == nil || allocateResp.Response.AddressSet == nil {
		return "", fmt.Errorf("allocate anycast ip for node %s has no response", node.Name)
	}
	if len(allocateResp.Response.AddressSet) != 1 {
		return "", fmt.Errorf("allocate anycast ip for node %s has %d address set, not 1, requestId: %s", node.Name, len(allocateResp.Response.AddressSet), *allocateResp.Response.RequestId)
	}

	// 3. return
	anycastIdAllocated := *allocateResp.Response.AddressSet[0]
	if !strings.HasPrefix(anycastIdAllocated, constants.AnycastIdPrefix) {
		return "", fmt.Errorf("allocate address got an invalid(has no prefix %s) anycast ip id: %s", constants.AnycastIdPrefix, anycastIdAllocated)
	}
	return anycastIdAllocated, nil
}

func (m *MangerImp) AssociateAnycastIp(node *corev1.Node, anycastIpId string) error {
	klog.V(2).Infof("trying to associate node %s with anycastIp %s", node.Name, anycastIpId)
	cvmInsId := node.Labels[constants.TkeNodeInsIdAnnoKey]
	// 1. describe anycast ip status
	descAddrReq := vpc.NewDescribeAddressesRequest()
	descAddrReq.AddressIds = common.StringPtrs([]string{anycastIpId})
	descAddrResp, err := m.vpcClient.DescribeAddresses(descAddrReq)
	if err != nil {
		return err
	}
	if descAddrResp == nil || descAddrResp.Response == nil || descAddrResp.Response.AddressSet == nil {
		return fmt.Errorf("DescribeAddresses of %s has no response", anycastIpId)
	}
	if len(descAddrResp.Response.AddressSet) != 1 {
		return fmt.Errorf("DescribeAddresses of %s has wrong addressSet len %d, expect to be 1", anycastIpId, len(descAddrResp.Response.AddressSet))
	}
	if descAddrResp.Response.AddressSet[0].AddressIp == nil {
		msg := fmt.Sprintf("DescribeAddresses of %s has no addressIp", anycastIpId)
		if descAddrResp.Response.RequestId != nil {
			msg += fmt.Sprintf(", requestId %s", *descAddrResp.Response.RequestId)
		}
		return fmt.Errorf(msg)
	}

	// 2. check if anycast has associated with this node
	anycastIpStatus := *descAddrResp.Response.AddressSet[0].AddressStatus
	anycastIpAddrIp := *descAddrResp.Response.AddressSet[0].AddressIp
	anycastAssociatedInsId := "NONE"
	if descAddrResp.Response.AddressSet[0].InstanceId != nil {
		anycastAssociatedInsId = *descAddrResp.Response.AddressSet[0].InstanceId
	}

	switch anycastIpStatus {
	case constants.AnycastStatusBIND:
		if anycastAssociatedInsId == cvmInsId {
			klog.V(2).Infof("anycast ip %s has already associated with cvm instance %s", anycastIpId, cvmInsId)
			// remove taint if necessary
			return m.removeNoAnycastTaintAndAddAnnotation(node, anycastIpId, anycastIpAddrIp) // eventually all nil will be return here
		} else {
			m.eventRecorder.Eventf(node, corev1.EventTypeWarning, constants.FailedAssociateAnycastIP,
				"anycast ip %s has associate to another resource(%s)", anycastIpId, anycastAssociatedInsId)
			return fmt.Errorf("anycast ip %s has assoicated with another instance(%s)", anycastIpId, anycastAssociatedInsId)
		}
	case constants.AnycastStatusUnBind:
		// 3. associate anycast ip with this node
		assAddrReq := vpc.NewAssociateAddressRequest()
		assAddrReq.AddressId = common.StringPtr(anycastIpId)
		assAddrReq.InstanceId = common.StringPtr(cvmInsId)
		assAddrResp, err := m.vpcClient.AssociateAddress(assAddrReq)
		if err != nil {
			return err
		}
		if assAddrResp == nil || assAddrResp.Response == nil {
			return fmt.Errorf("AssociateAddress for anycast ip %s for node %s has no response", anycastIpId, cvmInsId)
		}
		return fmt.Errorf("call vpc api to assocaite anycast ip %s with node %s success, requestId %s and taskId %s but still need to wait for it status to be BIND",
			anycastIpId, cvmInsId, *assAddrResp.Response.RequestId, *assAddrResp.Response.TaskId)
	default:
		klog.V(2).Infof("anycast ip %s status is %s, not going to process it.", anycastIpId, anycastIpStatus)
		return fmt.Errorf("waiting anycast ip %s to change it status, currently is %s", anycastIpId, anycastIpStatus)
	}
}

func (m *MangerImp) DisassociateAnycastIp(anycastIpId string) error {
	descAddrReq := vpc.NewDescribeAddressesRequest()
	descAddrReq.AddressIds = common.StringPtrs([]string{anycastIpId})
	descAddrResp, err := m.vpcClient.DescribeAddresses(descAddrReq)
	if err != nil {
		return err
	}
	if descAddrResp == nil || descAddrResp.Response == nil || descAddrResp.Response.AddressSet == nil {
		return fmt.Errorf("DescribeAddresses of %s has no response", anycastIpId)
	}
	if len(descAddrResp.Response.AddressSet) == 0 {
		klog.Warningf("DescribeAddresses of anycast ip %s return no resource, maybe has been release, no need to disassociate, requestId %s",
			anycastIpId, *descAddrResp.Response.RequestId)
		return nil
	}
	if len(descAddrResp.Response.AddressSet) != 1 {
		return fmt.Errorf("DescribeAddresses of %s has wrong addressSet len %d, expect to be 1", anycastIpId, len(descAddrResp.Response.AddressSet))
	}

	anycastIpStatus := *descAddrResp.Response.AddressSet[0].AddressStatus
	anycastAssociatedInsId := "NONE"
	if descAddrResp.Response.AddressSet[0].InstanceId != nil {
		anycastAssociatedInsId = *descAddrResp.Response.AddressSet[0].InstanceId
	}
	switch anycastIpStatus {
	case constants.AnycastStatusUnBind:
		klog.V(2).Infof("anycast ip %s status is %s, no need to call disassociate api", anycastIpId, anycastIpStatus)
		return nil
	case constants.AnycastStatusBIND:
		// todo: disassociate anycast ip
		disAssReq := vpc.NewDisassociateAddressRequest()
		disAssReq.AddressId = common.StringPtr(anycastIpId)
		disAssResp, err := m.vpcClient.DisassociateAddress(disAssReq)
		if err != nil {
			klog.Errorf("DisassociateAddress anycast ip %s failed, err: %v", anycastIpId, err)
			return err
		}
		if disAssResp == nil || disAssResp.Response == nil {
			return fmt.Errorf("DisassociateAddress anycast ip %s has no response", anycastIpId)
		}
		klog.V(2).Infof("call vpc api to disassociate anycast ip %s success, its origin associated resource %s, taskId %s, requestId %s",
			anycastIpId, anycastAssociatedInsId, *disAssResp.Response.TaskId, *disAssResp.Response.RequestId)
		return nil // outside loop will make sure the anycast status is UNBIND
	default:
		klog.V(2).Infof("anycast ip %s status is %s, not going to process it.", anycastIpId, anycastIpStatus)
		return fmt.Errorf("waiting anycast ip %s to change it status, currently is %s", anycastIpId, anycastIpStatus)
	}
}

func (m *MangerImp) ReleaseAnycastIp(anycastIpId string) error {
	klog.Infof("trying to release anycast ip %s", anycastIpId)
	reqRelease := vpc.NewReleaseAddressesRequest()
	reqRelease.AddressIds = common.StringPtrs([]string{anycastIpId})
	_, err := m.vpcClient.ReleaseAddresses(reqRelease)
	if err != nil {
		klog.Warningf("release anycast ip (%s) of failed, err: %v. And we will make sure if the anycast ip is not exist any more", anycastIpId, err)
		return err
	}
	klog.Infof("release anycast ip (%s) success", anycastIpId)
	return nil
}

func (m *MangerImp) IsCvmNeedToAllocateAnyCastIp(node *corev1.Node) (bool, error) {
	cvmInsId := node.Labels[constants.TkeNodeInsIdAnnoKey]
	descCvmAddrReq := vpc.NewDescribeAddressesRequest()
	descCvmAddrReq.Filters = []*vpc.Filter{
		{
			Name:   common.StringPtr("instance-id"),
			Values: common.StringPtrs([]string{cvmInsId}),
		},
		{
			Name:   common.StringPtr("address-type"),
			Values: common.StringPtrs([]string{constants.EipTypeCommon, constants.EipTypeCommon, constants.EipTypeAnyCast, constants.EipTypeHighQualityEIP}),
		},
	}
	descCvmAddrResp, err := m.vpcClient.DescribeAddresses(descCvmAddrReq)
	if err != nil {
		klog.Errorf("DescribeAddresses of cvm %s failed, err: %v", cvmInsId, err)
		return false, err
	}
	if descCvmAddrResp == nil || descCvmAddrResp.Response == nil {
		klog.Errorf("vpc/DescribeAddresses of cvm %s has no response", cvmInsId)
		return false, fmt.Errorf("vpc DescribeAddresses has no response")
	}

	for _, eipInfo := range descCvmAddrResp.Response.AddressSet {
		if eipInfo.AddressType == nil {
			continue
		}
		vpcReqId := "nil"
		if descCvmAddrResp.Response.RequestId != nil {
			vpcReqId = *descCvmAddrResp.Response.RequestId
		}
		klog.V(3).Infof("found node %s has eip type: %s, vpc requestId: %s, current processing type: %s", node.Name, *eipInfo.AddressType, vpcReqId, m.ProcessingEipType())
		switch *eipInfo.AddressType {
		case constants.EipTypeWanIp:
			klog.Infof("node %s already has wan ip %s,%s, cannot associate anycast ip", node.Name, *eipInfo.AddressId, *eipInfo.AddressIp)
			m.eventRecorder.Eventf(node, corev1.EventTypeWarning, constants.FailedAllocateAnycastIp, "node %s has WanIp, cannot associate AnycastIp", node.Name)
			return false, m.taintAiaToNodeIfNecessary(node)
		case constants.EipTypeCommon:
			klog.Infof("node %s already has EIP %s,%s, cannot associate anycast ip", node.Name, *eipInfo.AddressId, *eipInfo.AddressIp)
			m.eventRecorder.Eventf(node, corev1.EventTypeWarning, constants.FailedAllocateAnycastIp, "node %s has EIP %s/%s, cannot associate AnycastIp", node.Name, *eipInfo.AddressId, *eipInfo.AddressIp)
			return false, m.taintAiaToNodeIfNecessary(node)
		case constants.EipTypeAnyCast, constants.EipTypeHighQualityEIP:
			if *eipInfo.AddressType == m.ProcessingEipType() {
				if eipInfo.AddressId == nil || eipInfo.AddressIp == nil {
					return false, fmt.Errorf("anycast ip of cvm %s has no id or ip info", cvmInsId)
				}
				if err := m.removeNoAnycastTaintAndAddAnnotation(node, *eipInfo.AddressId, *eipInfo.AddressIp); err != nil {
					m.eventRecorder.Eventf(node, corev1.EventTypeWarning, constants.FailedUntaintNode, "failed to untaint node %s, will retry", node.Name)
					return false, err
				}
				klog.Infof("node %s already has anycast ip %s-%s, and remove taint success, just skip it", node.Name, *eipInfo.AddressId, *eipInfo.AddressIp)
				return false, nil
			} else {
				// upload warning events
				klog.Warningf("node %s already has EIP %s,%s, type is %s, cannot allocate %s", node.Name, *eipInfo.AddressId, *eipInfo.AddressIp, *eipInfo.AddressType, m.ProcessingEipType())
				m.eventRecorder.Eventf(node, corev1.EventTypeWarning, constants.FailedAllocateAnycastIp, "node %s has EIP %s/%s, type %s, cannot allocate %s",
					node.Name, *eipInfo.AddressId, *eipInfo.AddressIp, *eipInfo.AddressType, m.ProcessingEipType())
				return false, m.taintAiaToNodeIfNecessary(node)
			}
		default:
			klog.Warningf("found an unknown eip type for cvm %s: %s", cvmInsId, *eipInfo.AddressType)
		}
	}

	if err := m.taintAiaToNodeIfNecessary(node); err != nil {
		return true, err
	}
	return true, nil
}

func (m *MangerImp) GetOrCreateClusterUuidInCm() (string, error) {
	uuidRes := ""
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		uuidCm, err := m.k8sNoCacheClient.CoreV1().ConfigMaps("kube-system").Get(context.TODO(), constants.AiaIpControllerClusterUuidAnnoKey, metav1.GetOptions{})
		if err != nil && errors.IsNotFound(err) {
			data := map[string]string{}
			uuidStr := string(uuid.NewUUID())
			data[constants.AiaIpControllerClusterUuidAnnoKey] = uuidStr
			_, createErr := m.k8sNoCacheClient.CoreV1().ConfigMaps("kube-system").Create(context.TODO(), &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      constants.AiaIpControllerClusterUuidAnnoKey,
					Namespace: "kube-system",
				},
				Data: data,
			}, metav1.CreateOptions{})
			if createErr != nil {
				klog.Errorf("create cm in kube-system failed, err: %v", createErr)
				return createErr
			} else {
				uuidRes = uuidStr
				return nil
			}
		}
		if err != nil {
			klog.Errorf("get cm %s in kube-system failed, err: %v", constants.AiaIpControllerClusterUuidAnnoKey, err)
			return err
		}
		if uuidCm.Data == nil || uuidCm.Data[constants.AiaIpControllerClusterUuidAnnoKey] == "" {
			// create uuid, update successfully, and return
			klog.Errorf("get uuid from cm in kue-system got empty result")
			return fmt.Errorf("empty uuid cm %s in kube-system", constants.AiaIpControllerClusterUuidAnnoKey)
		}
		uuidRes = uuidCm.Data[constants.AiaIpControllerClusterUuidAnnoKey]
		return nil
	})

	klog.Infof("GetOrCreateClusterUuidInCm got uuid: %s", uuidRes)
	m.clusterUuid = uuidRes
	return uuidRes, retryErr
}

func (m *MangerImp) taintAiaToNodeIfNecessary(node *corev1.Node) error {
	for _, taint := range node.Spec.Taints {
		if taint.Key == constants.NoAnycastIpTaintKey {
			// already has taint
			return nil
		}
	}

	originTaint := node.Spec.Taints
	newTaint := append(originTaint, corev1.Taint{
		Key:    constants.NoAnycastIpTaintKey,
		Value:  "true",
		Effect: "NoSchedule",
	})

	patches := map[string]interface{}{
		"spec": map[string]interface{}{
			"taints": newTaint,
		},
	}
	patchData, err := json.Marshal(patches)
	if err != nil {
		return err
	}
	klog.V(2).Infof("patch data for taint node %s: %s", node.Name, string(patchData))

	// taint the node if necessary
	if err := m.k8sClient.Patch(context.Background(), node, client.RawPatch(types.MergePatchType, patchData)); err != nil {
		klog.Errorf("patch taint to node %s failed, err: %v", node.Name, err)
		return err
	}
	klog.V(2).Infof("patch taint to node %s success", node.Name)
	return nil
}

func (m *MangerImp) removeNoAnycastTaintAndAddAnnotation(node *corev1.Node, anycastId, anycastIp string) error {
	originTaint := node.Spec.Taints
	newTaint := make([]corev1.Taint, 0)
	hasTaint := false
	for _, taint := range originTaint {
		if taint.Key == constants.NoAnycastIpTaintKey {
			hasTaint = true
		} else {
			newTaint = append(newTaint, taint)
		}
	}

	hasAnno := false
	if _, ok := node.Annotations[constants.AnycastIpIdAnnotationKey]; ok {
		if _, ok2 := node.Annotations[constants.AnycastIpIpAnnotationKey]; ok2 {
			hasAnno = true
		}
	}

	if !hasTaint && hasAnno {
		return nil //no need to remove and patch annotation
	}

	newAnno := node.Annotations
	if !hasAnno {
		newAnno[constants.AnycastIpIdAnnotationKey] = anycastId
		newAnno[constants.AnycastIpIpAnnotationKey] = anycastIp
	}

	patches := map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": newAnno,
		},
		"spec": map[string]interface{}{
			"taints": newTaint,
		},
	}

	patchData, err := json.Marshal(patches)
	if err != nil {
		return err
	}
	klog.V(2).Infof("patch data for removing taint node %s: %s", node.Name, string(patchData))

	if err := m.k8sClient.Patch(context.Background(), node, client.RawPatch(types.MergePatchType, patchData)); err != nil {
		klog.Errorf("patch to remove taint of node %s failed, err: %v", node.Name, err)
		return err
	}

	klog.V(2).Infof("remove anycast taint for node %s success", node.Name)
	return nil
}
