package aia

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tag "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tag/v20180813"
	vpc "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vpc/v20170312"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"tkestack.io/aia-ip-controller/pkg/constants"
	"tkestack.io/aia-ip-controller/pkg/controller/util"
)

// ReverseReconcile should use leader election too
func (r *reconciler) ReverseReconcile() {
	// only leader election success instance can run reverse reconcile
	if !r.isLeader {
		return
	}
	klog.V(2).Infof("ReverseReconcile, try to process legacy anycast ip")

	// 1. get nodes of cluster
	nodes := &corev1.NodeList{}
	if err := r.k8sClient.List(context.TODO(), nodes); err != nil {
		klog.Errorf("ReverseReconcile list nodes of cluster failed, err: %v", err)
		return
	}
	nodeNames := make([]string, 0)
	for _, node := range nodes.Items {
		nodeNames = append(nodeNames, node.Name)
	}
	klog.V(2).Infof("ReverseReconcile found node names in cluster len %d", len(nodeNames))

	// 2. get all anycast ip from tag api, be careful about offset and limit
	anycastIds, err := r.getAllAnycastIpOfCluster()
	if err != nil {
		klog.Warningf("getAllAnycastIpOfCluster not success, msg: %v", err)
		return // do not process the left logic
	}
	klog.V(2).Infof("ReverseReconcile found cluster has anycast ip resourceIds len %d, detail: %s", len(anycastIds), strings.Join(anycastIds, ","))
	if len(anycastIds) <= 0 {
		klog.Infof("no anycast ip found from tag api, no need to do clean job, just return")
		return
	}

	// 3. get legacyAnycastIds by checking if nodeName in aia's tag exists in cluster nodeNames
	legacyAnycastIds, err := r.getLegacyAnycastIds(nodeNames, anycastIds)
	if err != nil {
		klog.Warningf("getLegacyAnycastIds not success, msg: %v", err)
		return
	}
	klog.V(2).Infof("ReverseReconcile found legacyAnycastIds len %d, detail: %s", len(legacyAnycastIds), strings.Join(legacyAnycastIds, ","))
	if len(legacyAnycastIds) <= 0 {
		klog.Infof("found no legacyAnycast, no need to do clean job, just return")
		return
	}

	// 4. get anycast ip that should be disassociate or release.
	unbindLegacyAnycastId, needDisassociateAnycastId, errGetUnBindAndLegacy := r.getUnBindAndNeedDisassociateLegacyAnycastIds(legacyAnycastIds)
	if errGetUnBindAndLegacy != nil {
		klog.Warningf("getUnBindAndNeedDisassociateLegacyAnycastIds not success, msg: %v", errGetUnBindAndLegacy)
		return
	}

	// 5. disassociate and release anycast ip
	if len(unbindLegacyAnycastId) > 0 {
		releaseAddrReq := vpc.NewReleaseAddressesRequest()
		releaseAddrReq.AddressIds = common.StringPtrs(unbindLegacyAnycastId)
		_, rErr := r.vpcClient.ReleaseAddresses(releaseAddrReq)
		if rErr != nil {
			klog.Errorf("release legacy unbind anycast ip (%s) failed, err: %v", strings.Join(unbindLegacyAnycastId, ","), rErr)
			return
		}
	}

	// wait for the anycast ip to be unbind
	if len(needDisassociateAnycastId) > 0 {
		time.Sleep(10 * time.Second)
		releaseAddrReqAgain := vpc.NewReleaseAddressesRequest()
		releaseAddrReqAgain.AddressIds = common.StringPtrs(needDisassociateAnycastId)
		_, rErr2 := r.vpcClient.ReleaseAddresses(releaseAddrReqAgain)
		if rErr2 != nil {
			klog.Errorf("release legacy disassociated anycast ip (%s) failed, err: %v", strings.Join(needDisassociateAnycastId, ","), rErr2)
			return
		}
	}
	if len(unbindLegacyAnycastId) > 0 || len(needDisassociateAnycastId) > 0 {
		klog.Infof("ReverseReconcile release %d legacy anycast ip, %d in BIND (%s) and %d in UNBIND (%s) status originally",
			len(unbindLegacyAnycastId)+len(needDisassociateAnycastId),
			len(needDisassociateAnycastId), strings.Join(needDisassociateAnycastId, ","), len(unbindLegacyAnycastId), strings.Join(unbindLegacyAnycastId, ","))
	}
}

// getAllAnycastIpOfCluster get all anycast ip from tag api, be careful about offset and limit
func (r *reconciler) getAllAnycastIpOfCluster() ([]string, error) {
	limit := 200 //change limit here if necessary, based on testing-result, we can use limit 200 here.
	offset := 0
	totalCount := math.MaxInt32 // a big number that will not reach
	retryTimeLimit := 101       // avoid infinite loop even if tag api response has wrong info
	resourceIds := make([]string, 0)
	for i := 1; i < retryTimeLimit && (limit+offset) <= totalCount; i++ {
		descTagReq := tag.NewDescribeResourcesByTagsRequest()
		descTagReq.TagFilters = []*tag.TagFilter{
			{
				TagKey:   common.StringPtr(constants.AiaIpControllerClusterUuidAnnoKey),
				TagValue: common.StringPtrs([]string{r.clusterUuid}),
			},
		}
		descTagReq.Limit = common.Uint64Ptr(uint64(limit))
		descTagReq.Offset = common.Uint64Ptr(uint64(offset))
		descTagReq.ServiceType = common.StringPtr("vpc") // tag api may return duplicate eip, in cvm and vpc type
		descTagResp, err := r.tagClient.DescribeResourcesByTags(descTagReq)
		if err != nil {
			klog.Errorf("DescribeResourcesByTags failed in DescribeResourcesByTags, err: %v", err)
			return resourceIds, err
		}
		if descTagResp == nil || descTagResp.Response == nil {
			klog.Errorf("DescribeResourcesByTags in ReverseReconcile has no response")
			return resourceIds, err
		}

		descTagRespB, _ := json.Marshal(descTagResp)
		klog.V(4).Infof("ReverseReconcile DescribeResourcesByTags response: %s", string(descTagRespB))

		for _, row := range descTagResp.Response.Rows {
			if row == nil || row.ResourceId == nil {
				klog.V(2).Infof("found a row is empty or without resource id")
				continue
			}
			resourceIds = append(resourceIds, *row.ResourceId)
		}
		// update total count from api response, so that we can end this loop
		totalCount = int(*descTagResp.Response.TotalCount)
		offset += len(descTagResp.Response.Rows)
		klog.V(2).Infof("ReverseReconcile DescribeResourcesByTags round %d, changing totalCount to %d, offset to %d", i, totalCount, offset)
	}

	return resourceIds, nil
}

// getLegacyAnycastIds if existedAnycastIds is lager than 20, need to call tag api multiple times
func (r *reconciler) getLegacyAnycastIds(existedNodeNames, existedAnycastIds []string) ([]string, error) {
	legacyAnycastIds := make([]string, 0)
	originAnycastIds := existedAnycastIds
	// tag api cannot set resourceIds more than 20 at a time, but tag api document does not mention that..
	// https://cloud.tencent.com/document/product/651/43061
	roundCount := 0
	for len(existedAnycastIds) > 0 {
		roundCount++
		curUsedAnycastId := make([]string, 0)
		if len(existedAnycastIds) > 20 {
			curUsedAnycastId = existedAnycastIds[:20]
			existedAnycastIds = existedAnycastIds[20:]
		} else {
			curUsedAnycastId = existedAnycastIds
			existedAnycastIds = []string{}
		}

		limit2 := 400 //change limit here, default can be 400
		offset2 := 0
		totalCount2 := math.MaxInt32 // a big number that will not reach
		retryTimeLimit2 := 501       // avoid infinite loop even if tag api response has wrong info
		for i := 1; i < retryTimeLimit2 && (offset2+limit2) <= totalCount2; i++ {
			descResourceByTagKeysReq := tag.NewDescribeResourceTagsByTagKeysRequest()
			descResourceByTagKeysReq.ServiceType = common.StringPtr("vpc")
			descResourceByTagKeysReq.ResourceRegion = common.StringPtr(r.Conf.Region.LongName)
			descResourceByTagKeysReq.TagKeys = common.StringPtrs([]string{constants.AiaIpControllerClusterUuidAnnoKey, constants.AiaNodeNameAnnoKey})
			descResourceByTagKeysReq.ResourceIds = common.StringPtrs(curUsedAnycastId)
			descResourceByTagKeysReq.ResourcePrefix = common.StringPtr("eip")
			descResourceByTagKeysReq.Limit = common.Uint64Ptr(uint64(limit2))
			descResourceByTagKeysReq.Offset = common.Uint64Ptr(uint64(offset2))
			descResourceByTagKeysResp, err := r.tagClient.DescribeResourceTagsByTagKeys(descResourceByTagKeysReq)
			if err != nil {
				klog.Errorf("ReverseReconcile DescribeResourceTagsByTagKeys failed, err: %s", err)
				return legacyAnycastIds, err
			}
			if descResourceByTagKeysResp == nil || descResourceByTagKeysResp.Response == nil {
				klog.Errorf("ReverseReconcile DescribeResourceTagsByTagKeys has no response, round(%d)", i)
				return legacyAnycastIds, fmt.Errorf("ReverseReconcile DescribeResourceTagsByTagKeys has no response, round(%d)", i)
			}

			descResourceByTagKeysRespB, _ := json.Marshal(descResourceByTagKeysResp)
			klog.V(2).Infof("ReverseReconcile descResourceByTagKeysResp(round:%d): %s", i, string(descResourceByTagKeysRespB))

			for _, row := range descResourceByTagKeysResp.Response.Rows {
				nodeNameInTag := ""
				for _, aTag := range row.TagKeyValues {
					if aTag != nil && aTag.TagKey != nil && *aTag.TagKey == constants.AiaNodeNameAnnoKey && aTag.TagValue != nil {
						nodeNameInTag = *aTag.TagValue
					}
				}
				if nodeNameInTag == "" {
					continue
				}
				if !util.ContainString(existedNodeNames, nodeNameInTag) && row.ResourceId != nil { // found an anycast ip in tag but not in cluster
					legacyAnycastIds = append(legacyAnycastIds, *row.ResourceId)
				}
			}

			totalCount2 = int(*descResourceByTagKeysResp.Response.TotalCount)
			offset2 += len(descResourceByTagKeysResp.Response.Rows) // here we maybe should times 2, but we choose a conservative policy
		}
		klog.V(2).Infof("getLegacyAnycastIds round %d origin existed anycast ids len: %d, current len: %d", roundCount, len(originAnycastIds), len(existedAnycastIds))
	}

	return legacyAnycastIds, nil
}

// getUnBindAndNeedDisassociateLegacyAnycastIds will return unbindLegacyAnycastIds and needDisassociateAnycastIds.
// During procession, it will also disassociate the needDisassociateAnycastIds
func (r *reconciler) getUnBindAndNeedDisassociateLegacyAnycastIds(legacyAnycastIds []string) ([]string, []string, error) {
	unbindLegacyAnycastId := make([]string, 0)
	needDisassociateAnycastId := make([]string, 0)

	descAnycastReq := vpc.NewDescribeAddressesRequest()
	descAnycastReq.AddressIds = common.StringPtrs(legacyAnycastIds)
	descAnycastResp, err := r.vpcClient.DescribeAddresses(descAnycastReq)
	if err != nil {
		klog.Errorf("DescribeAddresses to get legacy anycast ip status info failed, err: %v", err)
		return []string{}, []string{}, err
	}
	if descAnycastResp == nil || descAnycastResp.Response == nil {
		klog.Errorf("DescribeAddresses legacy anycast ip has no response")
		return []string{}, []string{}, fmt.Errorf("DescribeAddresses legacy anycast ip has no response")
	}

	for _, addr := range descAnycastResp.Response.AddressSet {
		if addr == nil || addr.AddressStatus == nil || addr.AddressId == nil {
			klog.Warningf("found an anycast ip in DescribeAddresses response has no necessary info(addr status or address id), just skip it")
			continue
		}
		if *addr.AddressStatus == constants.AnycastStatusUnBind {
			unbindLegacyAnycastId = append(unbindLegacyAnycastId, *addr.AddressId)
		}
		if *addr.AddressStatus == constants.AnycastStatusBIND {
			if addr.InstanceId == nil {
				klog.Warningf("found an anycast ip %s is in BIND status, but has no address id", addr.AddressId)
				continue
			}
			for _, aTag := range addr.TagSet {
				if aTag != nil && aTag.Key != nil && *aTag.Key == constants.AiaNodeInsIdAnnoKey && aTag.Value != nil && *aTag.Value == *addr.InstanceId {
					needDisassociateAnycastId = append(needDisassociateAnycastId, *addr.AddressId)
					// no batch api for disassociate, so we just call api here
					disAssReq := vpc.NewDisassociateAddressRequest()
					disAssReq.AddressId = common.StringPtr(*addr.AddressId)
					_, err := r.vpcClient.DisassociateAddress(disAssReq)
					if err != nil {
						klog.Warningf("ReverseReconcile disassociate address %s failed, err: %v", *addr.AddressId, err)
						break
					}
				}
			}
		}
	}

	return unbindLegacyAnycastId, needDisassociateAnycastId, nil
}
