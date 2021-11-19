package app

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"tkestack.io/aia-ip-controller/cmd/aia-ip-controller/app/config"
	"tkestack.io/aia-ip-controller/pkg/controller/aia"
)

func setupControllers(mgr ctrl.Manager, cfg *config.ControllerConfig) error {

	reconciler, err := aia.NewReconcile(
		mgr.GetClient(),
		mgr.GetEventRecorderFor(componentAiaIpController),
		cfg,
		ctrl.Log.WithName(componentAiaIpController),
	)
	if err != nil {
		return err
	}

	// loop to do reverse reconcile, default is disable
	if reconciler.EnableReverseReconcile {
		go wait.Until(reconciler.ReverseReconcile, time.Minute*1, wait.NeverStop)
	}

	// aia-ip-controller only interested in Create, Update and Delete events
	nodePredicate := predicate.Funcs{
		// ignore update and generic event
		UpdateFunc: reconciler.ProcessNodeUpdate,
		GenericFunc: func(genericEvent event.GenericEvent) bool {
			return false
		},
		// process node create and delete event
		CreateFunc: reconciler.ProcessNodeCreate,
		DeleteFunc: reconciler.ProcessNodeDelete,
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		WithEventFilter(nodePredicate).
		WithOptions(controller.Options{MaxConcurrentReconciles: cfg.MaxAiaIpControllerConcurrentReconciles}).
		Complete(reconciler)
}
