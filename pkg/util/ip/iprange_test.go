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

package ip

import (
	"net"
	"reflect"
	"testing"
)

func TestParseIPRange(t *testing.T) {
	tests := []struct {
		name string
		args string
		want []net.IP
	}{
		{
			name: "case 0",
			args: "10.244.1.0-10.244.1.5",
			want: []net.IP{
				net.ParseIP("10.244.1.0"),
				net.ParseIP("10.244.1.1"),
				net.ParseIP("10.244.1.2"),
				net.ParseIP("10.244.1.3"),
				net.ParseIP("10.244.1.4"),
				net.ParseIP("10.244.1.5"),
			},
		},
		{
			name: "case 1",
			args: "10.244.1.255-10.244.2.2",
			want: []net.IP{
				net.ParseIP("10.244.1.255"),
				net.ParseIP("10.244.2.0"),
				net.ParseIP("10.244.2.1"),
				net.ParseIP("10.244.2.2"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseIPRange(tt.args); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseIPRange() = %v, want %v", got, tt.want)
			}
		})
	}
}
