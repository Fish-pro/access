package app

import (
	"fmt"
	"k8s.io/klog/v2"
	"os"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/cli/globalflag"
	"k8s.io/component-base/term"
	"k8s.io/component-base/version"
	"k8s.io/component-base/version/verflag"

	"github.com/access-io/access/cmd/agent/app/config"
	"github.com/access-io/access/cmd/agent/app/options"
	accessctr "github.com/access-io/access/pkg/controllers/access"
	accessinformers "github.com/access-io/access/pkg/generated/informers/externalversions"
)

func NewControllerCommand() *cobra.Command {
	s := options.NewControllerOptions()

	cmd := &cobra.Command{
		Use: "access-agent",
		Run: func(cmd *cobra.Command, args []string) {
			verflag.PrintAndExitIfRequested()
			cliflag.PrintFlags(cmd.Flags())

			c, err := s.Config()
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}

			Run(c.Complete(), wait.NeverStop)
		},
	}

	fs := cmd.Flags()
	namedFlagSets := s.Flags()
	verflag.AddFlags(namedFlagSets.FlagSet("global"))
	globalflag.AddGlobalFlags(namedFlagSets.FlagSet("global"), cmd.Name())
	for _, f := range namedFlagSets.FlagSets {
		fs.AddFlagSet(f)
	}

	cols, _, _ := term.TerminalSize(cmd.OutOrStdout())
	cliflag.SetUsageAndHelpFunc(cmd, namedFlagSets, cols)

	return cmd
}

// Run runs the ControllerOptions.  This should never exit.
func Run(c *config.CompletedConfig, stopCh <-chan struct{}) {
	// To help debugging, immediately log version
	klog.Infof("Version: %+v", version.Get())

	// new normal informer factory
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(c.Client, time.Second*30)
	// new access informer factory
	accessInformerFactory := accessinformers.NewSharedInformerFactory(c.AClient, time.Second*30)

	// new controller
	controller := accessctr.NewController(
		c.Client,
		c.AClient,
		accessInformerFactory.Sample().V1alpha1().Accesses(),
		c.EventRecorder,
	)

	go controller.Run(1, stopCh)

	// notice that there is no need to run Start methods in a separate goroutine. (i.e. go kubeInformerFactory.Start(stopCh)
	// Start method is non-blocking and runs all registered informers in a dedicated goroutine.
	kubeInformerFactory.Start(stopCh)
	accessInformerFactory.Start(stopCh)

	select {}
}
