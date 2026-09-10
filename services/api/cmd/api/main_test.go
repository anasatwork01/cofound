package main

import "testing"

// TestServiceName is a tripwire: the service name is used as a metric label,
// a log field and an OTel service.name, so renaming it is never incidental.
func TestServiceName(t *testing.T) {
	if service != "api" {
		t.Fatalf("service name = %q, want %q", service, "api")
	}
}

// TestBuildMetadataLivesInPackageMain guards the ldflags contract.
//
// The Makefile injects -X main.version and -X main.commit. Moving these vars
// into the chassis would compile fine and silently produce "dev"/"unknown" in
// every production log line and every OTel resource, with no build failure to
// notice.
func TestBuildMetadataLivesInPackageMain(t *testing.T) {
	if version == "" || commit == "" {
		t.Fatal("version and commit must exist in package main for -X to target")
	}
}
