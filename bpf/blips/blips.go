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
