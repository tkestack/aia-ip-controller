package options

import (
	"time"

	"github.com/spf13/pflag"
)

type LeaderElectionOptions struct {
	Enable           bool
	Namespace        string
	ResourceLockName string
	LeaseDuration    time.Duration
	RenewDeadline    time.Duration
	RetryPeriod      time.Duration
}

// NewLeaderElectionOptions returns leader elections configuration default values for aia-controller
func NewLeaderElectionOptions() *LeaderElectionOptions {
	return &LeaderElectionOptions{
		Enable:           true,
		Namespace:        "kube-system",
		ResourceLockName: "tke-aia-ip-controller",
		LeaseDuration:    20 * time.Second,
		RenewDeadline:    15 * time.Second,
		RetryPeriod:      5 * time.Second,
	}
}

// AddFlags adds flags related to leader election for controller to the
// specified FlagSet.
func (o *LeaderElectionOptions) AddFlags(fs *pflag.FlagSet) {
	if o == nil {
		return
	}

	fs.BoolVar(&o.Enable, "leader-elect", o.Enable,
		"If true, aia-controller will perform leader election between instances to ensure no more "+
			"than one instance of aia-controller operates at a time.")
	fs.StringVar(&o.Namespace, "leader-election-namespace", o.Namespace,
		"Namespace used to perform leader election. Only used if leader election is enabled.")
	fs.DurationVar(&o.LeaseDuration, "leader-election-lease-duration", o.LeaseDuration,
		"The duration that non-leader candidates will wait after observing a leadership "+
			"renewal until attempting to acquire leadership of a led but un-renewed leader "+
			"slot. This is effectively the maximum duration that a leader can be stopped "+
			"before it is replaced by another candidate. This is only applicable if leader "+
			"election is enabled.")
	fs.DurationVar(&o.RenewDeadline, "leader-election-renew-deadline", o.RenewDeadline,
		"The interval between attempts by the acting master to renew a leadership slot "+
			"before it stops leading. This must be less than or equal to the lease duration. "+
			"This is only applicable if leader election is enabled.")
	fs.DurationVar(&o.RetryPeriod, "leader-election-retry-period", o.RetryPeriod,
		"The duration the clients should wait between attempting acquisition and renewal "+
			"of a leadership. This is only applicable if leader election is enabled.")
	fs.StringVar(&o.ResourceLockName, "resource-lock-name", o.ResourceLockName, "Resource lock name used for aia-ip-controller leader election")
}

// Validate checks validation of LeaderElectionOptions.
func (o *LeaderElectionOptions) Validate() []error {
	if o == nil {
		return nil
	}

	var errs []error
	return errs
}
