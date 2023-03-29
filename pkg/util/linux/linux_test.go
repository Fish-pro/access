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

package linux

import (
	"net"
	"testing"
)

func TestIP2Long(t *testing.T) {
	tests := []struct {
		name    string
		args    net.IP
		want    uint
		wantErr bool
	}{
		{
			name: "case 0",
			args: net.ParseIP("10.29.4.21"),
			want: 352591114,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := IP2Long(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("IP2Long() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("IP2Long() got = %v, want %v", got, tt.want)
			}
		})
	}
}
