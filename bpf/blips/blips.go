package blips

import (
	"github.com/cilium/ebpf/link"
	"k8s.io/klog/v2"
)

// $BPF_CLANG and $BPF_CFLAGS are set by the Makefile.
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc $BPF_CLANG -cflags $BPF_CFLAGS bpf bl_ips.c -- -I../headers

type EbpfEngine struct {
	BpfObjs bpfObjects
	Link    link.Link
}

func NewEbpfEngine() (*EbpfEngine, error) {
	objs := bpfObjects{}
	if err := loadBpfObjects(&objs, nil); err != nil {
		klog.Fatalf("loading objects: %s", err)
		return nil, err
	}

	// Attach the program.
	l, err := link.AttachXDP(link.XDPOptions{
		Program: objs.DropBlArp,
	})
	if err != nil {
		klog.Fatalf("could not attach XDP program: %s", err)
		return nil, err
	}

	return &EbpfEngine{BpfObjs: objs, Link: l}, nil
}

func (e *EbpfEngine) Close() error {
	err := e.BpfObjs.Close()
	if err != nil {
		return err
	}
	return e.Link.Close()
}
