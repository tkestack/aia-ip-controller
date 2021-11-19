package aia

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"tkestack.io/aia-ip-controller/pkg/constants"
)

// ProcessNodeCreate will check if the created node has aia label, if so, dive into reconcile logic
func (r *reconciler) ProcessNodeCreate(createEvent event.CreateEvent) bool {
	r.isLeader = true
	node := createEvent.Object.(*corev1.Node)
	cvmInsId := node.Labels[constants.TkeNodeInsIdAnnoKey]
	klog.V(2).Infof("watched node %s(%s) create event", node.Name, cvmInsId)
	// 1. check if the node has all specified labels
	return r.AiaManger.IsAiaNode(r.Conf.Node.Labels, node)
}
