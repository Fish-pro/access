package app

import (
	"context"
	"fmt"
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
	"k8s.io/klog/v2"

	"github.com/access-io/access/bpf/blips"
	"github.com/access-io/access/cmd/agent/app/config"
	"github.com/access-io/access/cmd/agent/app/options"
	accessctr "github.com/access-io/access/pkg/controllers/access"
	accessinformers "github.com/access-io/access/pkg/generated/informers/externalversions"
)

func NewAgentCommand() *cobra.Command {
	o := options.NewAgentOptions()

	cmd := &cobra.Command{
		Use: "access-agent",
		Run: func(cmd *cobra.Command, args []string) {
			verflag.PrintAndExitIfRequested()
			cliflag.PrintFlags(cmd.Flags())

			c, err := o.Config()
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}

			if err := Run(c.Complete(), wait.NeverStop); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}
		},
	}

	fs := cmd.Flags()
	namedFlagSets := o.Flags()
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
func Run(c *config.CompletedConfig, stopCh <-chan struct{}) error {
	// To help debugging, immediately log version
	klog.Infof("Version: %+v", version.Get())

	klog.InfoS("Golang settings", "GOGC", os.Getenv("GOGC"), "GOMAXPROCS", os.Getenv("GOMAXPROCS"), "GOTRACEBACK", os.Getenv("GOTRACEBACK"))

	// attach ebpf program
	engine, err := blips.NewEbpfEngine()
	if err != nil {
		klog.Errorf("failed to attach ebpf program: %w", err)
		return err
	}
	defer engine.Close()

	// new normal informer factory
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(c.Client, time.Second*30)
	// new access informer factory
	accessInformerFactory := accessinformers.NewSharedInformerFactory(c.AClient, time.Second*30)

	// new controller
	controller, err := accessctr.NewController(
		c.AClient,
		accessInformerFactory.Sample().V1alpha1().Accesses(),
		kubeInformerFactory.Core().V1().Nodes(),
		c.EventRecorder,
		engine,
	)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	go controller.Run(ctx)

	kubeInformerFactory.Start(stopCh)
	accessInformerFactory.Start(stopCh)

	<-stopCh
	return nil
}
