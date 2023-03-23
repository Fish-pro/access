/*
Copyright 2023 The access Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package blips

import (
	"fmt"
	"net"
	"os"

	"github.com/cilium/ebpf/link"
)

// $BPF_CLANG and $BPF_CFLAGS are set by the Makefile.
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc $BPF_CLANG -cflags $BPF_CFLAGS bpf bl_ips.c -- -I../headers

type EbpfEngine struct {
	BpfObjs bpfObjects
	Link    link.Link
}

func NewEbpfEngine(ifaceName string) (*EbpfEngine, error) {
	if os.Getuid() != 0 {
		return nil, fmt.Errorf("root user in required for this process or container")
	}

	// Look up the network interface by name.
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to look up network interface: %w", err)
	}

	objs := bpfObjects{}
	if err := loadBpfObjects(&objs, nil); err != nil {
		return nil, fmt.Errorf("failed to load objects: %s", err)
	}

	// Attach the program.
	l, err := link.AttachXDP(link.XDPOptions{
		Program:   objs.XdpBlDrop,
		Interface: iface.Index,
	})
	if err != nil {
		return nil, fmt.Errorf("could not attach XDP program: %w", err)
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
