package main

import "testing"

func TestEnvFlag(t *testing.T) {
	cases := []struct {
		name         string
		value        string
		defaultValue bool
		want         bool
	}{
		{name: "unset uses default true", value: "", defaultValue: true, want: true},
		{name: "unset uses default false", value: "", defaultValue: false, want: false},
		{name: "zero disables", value: "0", defaultValue: true, want: false},
		{name: "false disables", value: "false", defaultValue: true, want: false},
		{name: "one enables", value: "1", defaultValue: false, want: true},
		{name: "true enables", value: "true", defaultValue: false, want: true},
		{name: "on enables", value: "on", defaultValue: false, want: true},
		{name: "yes enables", value: "yes", defaultValue: false, want: true},
		{name: "off disables", value: "off", defaultValue: true, want: false},
		{name: "no disables", value: "no", defaultValue: true, want: false},
		{name: "surrounding whitespace", value: " true ", defaultValue: false, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AURELIA_TEST_FLAG", tc.value)
			if got := envFlag("AURELIA_TEST_FLAG", tc.defaultValue); got != tc.want {
				t.Fatalf("envFlag(%q, %t) = %t, want %t", tc.value, tc.defaultValue, got, tc.want)
			}
		})
	}
}

func TestEnvFlagInvalidFailsClosed(t *testing.T) {
	t.Setenv("AURELIA_TEST_FLAG", "maybe")
	if got := envFlag("AURELIA_TEST_FLAG", true); got {
		t.Fatal("invalid value must not enable the surface")
	}
	if got := envFlag("AURELIA_TEST_FLAG", false); got {
		t.Fatal("invalid value must not enable the surface")
	}
}
