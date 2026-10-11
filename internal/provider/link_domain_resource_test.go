package provider

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// A link domain needs a verified sending domain and a paid plan, and creating
// one registers the hostname with Cloudflare, so this test only runs when a
// hostname under a verified domain of the test account is named.
func TestAccLinkDomainResource(t *testing.T) {
	hostname := os.Getenv("SMTPFAST_ACC_LINK_HOSTNAME")

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			if hostname == "" {
				t.Skip("set SMTPFAST_ACC_LINK_HOSTNAME to a subdomain of a verified sending domain of the test account")
			}
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`resource "smtpfast_link_domain" "test" { hostname = %q }`, hostname),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("smtpfast_link_domain.test", "id"),
					resource.TestCheckResourceAttrSet("smtpfast_link_domain.test", "cname_target"),
					resource.TestCheckResourceAttr("smtpfast_link_domain.test", "dns_records.0.type", "CNAME"),
				),
			},
			{
				ResourceName:      "smtpfast_link_domain.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestPlanApplyLinkDomain(t *testing.T) {
	skipWithoutTerraform(t)
	api, url := newFakeAPI(t)
	api.domains["dom_1"] = &fakeDomain{ID: "dom_1", Domain: "example.com", Status: "verified"}
	const name = "smtpfast_link_domain.test"
	// The DNS record stands in for cloudflare_record or aws_route53_record: it
	// is created in the same apply, from values known after the link domain.
	config := fakeProviderConfig(url) + `resource "smtpfast_link_domain" "test" { hostname = "links.example.com" }

resource "terraform_data" "cname" {
  input = {
    name    = smtpfast_link_domain.test.hostname
    content = smtpfast_link_domain.test.cname_target
  }
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "status", "pending"),
					resource.TestCheckResourceAttr(name, "domain_id", "dom_1"),
					resource.TestCheckResourceAttr(name, "domain", "example.com"),
					resource.TestCheckResourceAttr(name, "cname_target", "links.smtpfa.st"),
					resource.TestCheckResourceAttr(name, "dns_records.#", "2"),
					resource.TestCheckResourceAttr(name, "dns_records.0.required", "true"),
					resource.TestCheckResourceAttr(name, "dns_records.1.type", "TXT"),
					resource.TestCheckResourceAttr(name, "dns_records.1.required", "false"),
					resource.TestCheckNoResourceAttr(name, "verified_at"),
					resource.TestCheckResourceAttr("terraform_data.cname", "output.content", "links.smtpfa.st"),
				),
			},
			{
				// Activation is a server-side change to computed values only,
				// so the same configuration plans no changes.
				PreConfig: func() {
					for _, l := range api.links {
						l.Status = "active"
					}
				},
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "status", "active"),
					resource.TestCheckResourceAttr(name, "dns_records.#", "1"),
					resource.TestCheckResourceAttr(name, "dns_records.0.status", "verified"),
					resource.TestCheckResourceAttrSet(name, "verified_at"),
				),
			},
			{
				ResourceName:      name,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestPlanApplyLinkDomainErrors(t *testing.T) {
	skipWithoutTerraform(t)
	api, url := newFakeAPI(t)
	api.domains["dom_1"] = &fakeDomain{ID: "dom_1", Domain: "example.com", Status: "pending"}
	provider := fakeProviderConfig(url)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      provider + `resource "smtpfast_link_domain" "test" { hostname = "Links.Example.com" }`,
				ExpectError: regexp.MustCompile(`lowercase`),
			},
			{
				Config:      provider + `resource "smtpfast_link_domain" "test" { hostname = "links.example.com" }`,
				ExpectError: regexp.MustCompile(`(?s)Verify example.com first.*already\s+verified`),
			},
			{
				Config:      provider + `resource "smtpfast_link_domain" "test" { hostname = "links.other.org" }`,
				ExpectError: regexp.MustCompile(`subdomain of one of your\s+verified sending domains`),
			},
			{
				PreConfig:   func() { api.freeTeam = true },
				Config:      provider + `resource "smtpfast_link_domain" "test" { hostname = "links.example.com" }`,
				ExpectError: regexp.MustCompile(`(?s)paid\s+plans.*Upgrade`),
			},
		},
	})
}
