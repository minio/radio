package config

import "testing"

func TestValidRegion(t *testing.T) {
	tests := []struct {
		name    string
		success bool
	}{
		{name: "us-east-1", success: true},
		{name: "us_east", success: true},
		{name: "helloWorld", success: true},
		{name: "-fdslka", success: false},
		{name: "^00[", success: false},
		{name: "my region", success: false},
		{name: "%%$#!", success: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ok := validRegionRegex.MatchString(test.name)
			if test.success != ok {
				t.Errorf("Expected %t, got %t", test.success, ok)
			}
		})
	}
}
