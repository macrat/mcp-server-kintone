package main

import (
	"encoding/json"
	"testing"
)

func TestPermissionList(t *testing.T) {
	tests := []struct {
		Input  string
		Output Permissions
		Error  string
	}{
		{
			Input: `{}`,
			Output: Permissions{
				Read:   true,
				Write:  false,
				Delete: false,
			},
		},
		{
			Input: `{"read": true, "write": true, "delete": true}`,
			Output: Permissions{
				Read:   true,
				Write:  true,
				Delete: true,
			},
		},
		{
			Input: `{"read": false, "write": false, "delete": false}`,
			Output: Permissions{
				Read:   false,
				Write:  false,
				Delete: false,
			},
		},
		{
			Input: `{"delete": true}`,
			Output: Permissions{
				Read:   true,
				Write:  false,
				Delete: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Input, func(t *testing.T) {
			var permissions Permissions
			if err := json.Unmarshal([]byte(tt.Input), &permissions); err != nil {
				if tt.Error == "" {
					t.Errorf("unexpected error: %s", err)
				}
				if tt.Error != err.Error() {
					t.Errorf("expected error: %s, got: %s", tt.Error, err)
				}
				return
			}

			if permissions.Read != tt.Output.Read {
				t.Errorf("expected Read: %t, got: %t", tt.Output.Read, permissions.Read)
			}
			if permissions.Write != tt.Output.Write {
				t.Errorf("expected Write: %t, got: %t", tt.Output.Write, permissions.Write)
			}
			if permissions.Delete != tt.Output.Delete {
				t.Errorf("expected Delete: %t, got: %t", tt.Output.Delete, permissions.Delete)
			}
		})
	}
}
