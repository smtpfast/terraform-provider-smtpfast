package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories wires the provider into the acceptance test
// framework.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"smtpfast": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccPreCheck verifies the environment is set up for acceptance tests.
// These tests create and destroy real resources, so they need a real key.
func testAccPreCheck(t *testing.T) {
	if os.Getenv("SMTPFAST_API_KEY") == "" {
		t.Fatal("SMTPFAST_API_KEY must be set for acceptance tests")
	}
}
