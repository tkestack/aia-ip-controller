package app

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/cli/globalflag"
	"k8s.io/component-base/term"
	"k8s.io/component-base/version/verflag"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"tkestack.io/aia-ip-controller/cmd/aia-ip-controller/app/config"
	"tkestack.io/aia-ip-controller/cmd/aia-ip-controller/app/options"
)

const (
	// componentAiaIpController is the name of the CLI application.
	componentAiaIpController = "aia-ip-controller"
	// aiaIpControllerDesc is the long message shown in the 'help <this-command>' output.
	aiaIpControllerDesc = `The aia-ip-controller used for integrating tke with tencentcloud anycast eip`
)

func NewControllerCommand() *cobra.Command {
	opts := options.NewControllerOptions()

	cmd := &cobra.Command{
		Use:  componentAiaIpController,
		Long: aiaIpControllerDesc,
		Run: func(cmd *cobra.Command, args []string) {
			verflag.PrintAndExitIfRequested()
			cliflag.PrintFlags(cmd.Flags())

			c, err := opts.Config()
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}

			if err := Run(c); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}
		},
	}

	fs := cmd.Flags()
	namedFlagSets := opts.Flags()
	verflag.AddFlags(namedFlagSets.FlagSet("global"))
	globalflag.AddGlobalFlags(namedFlagSets.FlagSet("global"), cmd.Name())
	for _, f := range namedFlagSets.FlagSets {
		fs.AddFlagSet(f)
	}
	usageFmt := "Usage:\n  %s\n"
	cols, _, _ := term.TerminalSize(cmd.OutOrStdout())
	cmd.SetUsageFunc(func(cmd *cobra.Command) error {
		_, _ = fmt.Fprintf(cmd.OutOrStderr(), usageFmt, cmd.UseLine())
		cliflag.PrintSections(cmd.OutOrStderr(), namedFlagSets, cols)
		return nil
	})
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\n\n"+usageFmt, cmd.Long, cmd.UseLine())
		cliflag.PrintSections(cmd.OutOrStdout(), namedFlagSets, cols)
	})

	return cmd
}

// Run runs the aia-controller.  This should never exit.
func Run(c *config.Config) error {
	if err := setupControllers(c.ControllerManager, &c.ControllerConfig); err != nil {
		klog.Errorf("Unable to setup controllers: %v", err)
		return err
	}

	if err := c.ControllerManager.Start(ctrl.SetupSignalHandler()); err != nil {
		klog.Errorf("Unable to start the controller manager: %v", err)
		return err
	}

	return nil
}
