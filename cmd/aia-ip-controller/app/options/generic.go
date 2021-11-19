package options

import "github.com/spf13/pflag"

// GenericOptions holds the options which are generic.
type GenericOptions struct {
	// ContentType is the content type used when sending data to the server from this client.
	ContentType string
	// QPS controls the number of queries per second allowed for this connection.
	QPS float32
	// Burst allows extra queries to accumulate when a client is exceeding its rate.
	Burst int
}

// NewGenericOptions returns generic configuration default values for aia controller.
func NewGenericOptions() *GenericOptions {
	return &GenericOptions{
		QPS:         20.0,
		Burst:       30,
		ContentType: "application/vnd.kubernetes.protobuf",
	}
}

// AddFlags adds flags related to generic for controller to the specified FlagSet.
func (o *GenericOptions) AddFlags(fs *pflag.FlagSet) {
	if o == nil {
		return
	}

	fs.StringVar(&o.ContentType, "kube-api-content-type", o.ContentType, "Content type of requests sent to apiserver.")
	fs.Float32Var(&o.QPS, "kube-api-qps", o.QPS, "QPS to use while talking with kubernetes apiserver.")
	fs.IntVar(&o.Burst, "kube-api-burst", o.Burst, "Burst to use while talking with kubernetes apiserver.")
}

// Validate checks validation of GenericOptions.
func (o *GenericOptions) Validate() []error {
	if o == nil {
		return nil
	}

	var errs []error
	return errs
}
