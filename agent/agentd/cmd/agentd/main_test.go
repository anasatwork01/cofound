package main

import "testing"

// TestServiceName is a tripwire: the service name is used as a metric label,
// a log field and an OTel service.name, so renaming it is never incidental.
func TestServiceName(t *testing.T) {
	if service != "agentd" {
		t.Fatalf("service name = %q, want %q", service, "agentd")
	}
}
