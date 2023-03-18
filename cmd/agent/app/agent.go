package app

import (
	"fmt"
	"github.com/access-io/access/pkg/ebpfs"
	"os"
	"os/signal"
	"syscall"
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

			if err := ebpfs.LoadMBProgs(); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}

			c, err := s.Config()
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
func Run(c *config.CompletedConfig, stopCh <-chan struct{}) error {
	// To help debugging, immediately log version
	klog.Infof("Version: %+v", version.Get())

	if err := ebpfs.InitLoadPinnedMap(); err != nil {
		return fmt.Errorf("failed to load ebpf maps: %v", err)
	}
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
	)
	if err != nil {
		return err
	}

	go controller.Run(1, stopCh)

	kubeInformerFactory.Start(stopCh)
	accessInformerFactory.Start(stopCh)

	if err = ebpfs.AttachMBProgs(); err != nil {
		return fmt.Errorf("failed to attach ebpf programs: %v", err)
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT)
	<-ch

	if err = ebpfs.UnLoadMBProgs(); err != nil {
		return fmt.Errorf("unload failed: %v", err)
	}
	return nil
}
