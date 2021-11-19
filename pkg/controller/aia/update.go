package aia

import (
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"tkestack.io/aia-ip-controller/pkg/constants"
)

// ProcessNodeUpdate will take care of the updated nodes
func (r *reconciler) ProcessNodeUpdate(updateEvent event.UpdateEvent) bool {
	oldNode, ok := updateEvent.ObjectOld.(*corev1.Node)
	if !ok {
		newNode, ok2 := updateEvent.ObjectNew.(*corev1.Node)
		if !ok2 {
			klog.V(4).Infof("update event old node and new node type assertion are not ok, return false, not going to enqueue")
			return false
		}
		return r.AiaManger.IsAiaNode(r.Conf.Node.Labels, newNode)
	}

	newNode, ok2 := updateEvent.ObjectNew.(*corev1.Node)
	if !ok2 {
		klog.V(4).Infof("update event old node type assertion ok, but new node not ok, return false, not going to enqueue")
		return false
	}

	cvmInsId := newNode.Labels[constants.TkeNodeInsIdAnnoKey]
	klog.V(2).Infof("watched node %s(%s) update event", newNode.Name, cvmInsId)

	// new and old node are the same, not process
	if reflect.DeepEqual(oldNode.ObjectMeta.Labels, newNode.ObjectMeta.Labels) {
		klog.V(4).Infof("node %s meta.labels not changed, not going to enqueue", newNode.Name)
		return false
	}

	// check if the node has all specified labels
	return r.AiaManger.IsAiaNode(r.Conf.Node.Labels, newNode)
}
