package provider

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// The documented pattern: one apply creates the domain, publishes its
// records with for_each over records, and waits for verification.
//
// The test framework cannot read for_each state, so a plan-only step proves
// that the for_each plans before the domain exists, and the applies use
// count = length(records), which needs the same thing: a map whose keys are
// known at plan time.
func TestPlanApplyDomainOneApply(t *testing.T) {
	skipWithoutTerraform(t)
	fastVerificationPolls(t)
	api, url := newFakeAPI(t)
	api.domainVerifyAfter = 2
	provider := fakeProviderConfig(url)
	domain := func(receiving string) string {
		return provider + `resource "smtpfast_domain" "test" {
  domain = "mail.example.com"
` + receiving + `
}

resource "smtpfast_domain_verification" "test" {
  domain_id  = smtpfast_domain.test.id
  depends_on = [terraform_data.record]
}
`
	}
	forEach := func(receiving string) string {
		return domain(receiving) + `
resource "terraform_data" "record" {
  for_each = smtpfast_domain.test.records
  input    = each.value
}
`
	}
	// keys() sorts: dkim_1, dkim_2, dkim_3, dmarc, mail_from_mx,
	// mail_from_spf, (receiving_mx,) spf.
	count := func(receiving string) string {
		return domain(receiving) + `
resource "terraform_data" "record" {
  count = length(smtpfast_domain.test.records)
  input = {
    key    = keys(smtpfast_domain.test.records)[count.index]
    record = values(smtpfast_domain.test.records)[count.index]
  }
}
`
	}
	creates := func(keys ...string) []plancheck.PlanCheck {
		checks := []plancheck.PlanCheck{}
		for _, key := range keys {
			checks = append(checks, plancheck.ExpectResourceAction(`terraform_data.record["`+key+`"]`, plancheck.ResourceActionCreate))
		}
		return checks
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             forEach(""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPreRefresh: creates("dkim_1", "dkim_2", "dkim_3", "spf", "dmarc", "mail_from_mx", "mail_from_spf"),
				},
			},
			{
				Config: count(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_domain.test", "records.%", "7"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "records.dkim_1.name", "tok1._domainkey.mail.example.com"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "records.dkim_3.value", "tok3.dkim.amazonses.com"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "records.spf.name", "mail.example.com"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "records.spf.purpose", "SPF authorization"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "records.dmarc.name", "_dmarc.mail.example.com"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "records.mail_from_mx.name", "bounce.mail.example.com"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "records.mail_from_mx.priority", "10"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "records.mail_from_mx.category", "sending"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "records.mail_from_spf.type", "TXT"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "region", "eu-west-2"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "mail_from_domain", "bounce.mail.example.com"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "spf_include", "amazonses.com"),
					resource.TestCheckResourceAttr("terraform_data.record.6", "output.key", "spf"),
					resource.TestCheckResourceAttr("terraform_data.record.6", "output.record.value", "v=spf1 include:amazonses.com ~all"),
					resource.TestCheckResourceAttr("smtpfast_domain_verification.test", "status", "verified"),
					resource.TestCheckResourceAttr("smtpfast_domain_verification.test", "domain", "mail.example.com"),
					func(*terraform.State) error {
						if got := api.domains["dom_1"].VerifyCalls; got != 3 {
							return fmt.Errorf("verify calls = %d, want 3", got)
						}
						return nil
					},
				),
			},
			{
				// The refresh after verification reads the new statuses.
				Config: count(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_domain.test", "status", "verified"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "dkim_status", "verified"),
				),
			},
			{
				// Turning receiving on adds the receiving_mx key at plan time.
				Config: count("  receiving_enabled = true"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("terraform_data.record[7]", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction("smtpfast_domain_verification.test", plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_domain.test", "records.%", "8"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "records.receiving_mx.name", "mail.example.com"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "records.receiving_mx.purpose", "Inbound mail"),
					resource.TestCheckResourceAttr("terraform_data.record.6", "output.key", "receiving_mx"),
				),
			},
			{
				Config: count("  receiving_enabled = false"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("terraform_data.record[7]", plancheck.ResourceActionDestroy),
					},
				},
				Check: resource.TestCheckResourceAttr("smtpfast_domain.test", "records.%", "7"),
			},
			{
				// A new domain name replaces the domain; the keys stay known,
				// and the verification waits again for the new domain.
				Config: strings.Replace(count(""), `"mail.example.com"`, `"mail2.example.com"`, 1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("smtpfast_domain.test", plancheck.ResourceActionDestroyBeforeCreate),
						plancheck.ExpectResourceAction("smtpfast_domain_verification.test", plancheck.ResourceActionDestroyBeforeCreate),
						plancheck.ExpectResourceAction("terraform_data.record[6]", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_domain.test", "records.spf.name", "mail2.example.com"),
					resource.TestCheckResourceAttr("smtpfast_domain_verification.test", "domain", "mail2.example.com"),
				),
			},
			{
				// The domain was read before the verification ran.
				RefreshState: true,
			},
			{
				ResourceName:      "smtpfast_domain.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// A check that fails with a server error is tried again.
func TestPlanApplyDomainVerificationRetries(t *testing.T) {
	skipWithoutTerraform(t)
	fastVerificationPolls(t)
	api, url := newFakeAPI(t)
	api.domainVerifyFailures = 2
	api.domainVerifyAfter = 1

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fakeProviderConfig(url) + `resource "smtpfast_domain" "test" {
  domain = "mail.example.com"
}

resource "smtpfast_domain_verification" "test" {
  domain_id = smtpfast_domain.test.id
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_domain_verification.test", "status", "verified"),
					func(*terraform.State) error {
						if got := api.domains["dom_1"].VerifyCalls; got != 4 {
							return fmt.Errorf("verify calls = %d, want 4", got)
						}
						return nil
					},
				),
			},
		},
	})
}

// A key that is unknown at plan time (from another resource here) does not
// stop the plan; the apply gets the real value.
func TestPlanApplyUnknownAPIKeyAtPlan(t *testing.T) {
	skipWithoutTerraform(t)
	t.Setenv("SMTPFAST_API_KEY", "")
	_, url := newFakeAPI(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `resource "terraform_data" "key" {
  input = "test-key"
}

provider "smtpfast" {
  api_key  = terraform_data.key.output
  base_url = "` + url + `"
}

resource "smtpfast_domain" "test" {
  domain = "mail.example.com"
}
`,
				Check: resource.TestCheckResourceAttr("smtpfast_domain.test", "records.%", "7"),
			},
		},
	})
}

// A new domain can receive from its first apply when it is a subdomain of a
// verified domain. The plan then has the receiving_mx key too.
func TestPlanApplyDomainCreatedReceiving(t *testing.T) {
	skipWithoutTerraform(t)
	api, url := newFakeAPI(t)
	api.domains["dom_parent"] = &fakeDomain{ID: "dom_parent", Domain: "example.com", Status: "verified"}
	config := fakeProviderConfig(url) + `resource "smtpfast_domain" "test" {
  domain            = "in.example.com"
  receiving_enabled = true
}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config + `
resource "terraform_data" "record" {
  for_each = smtpfast_domain.test.records
  input    = each.value
}
`,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPreRefresh: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(`terraform_data.record["receiving_mx"]`, plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction(`terraform_data.record["spf"]`, plancheck.ResourceActionCreate),
					},
				},
			},
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_domain.test", "receiving_enabled", "true"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "records.%", "8"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "records.receiving_mx.name", "in.example.com"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "dns_records.#", "8"),
				),
			},
		},
	})
}

func TestPlanApplyDomainVerificationTimeout(t *testing.T) {
	skipWithoutTerraform(t)
	fastVerificationPolls(t)
	api, url := newFakeAPI(t)
	api.domainVerifyAfter = -1

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fakeProviderConfig(url) + `resource "smtpfast_domain" "test" {
  domain = "mail.example.com"
}

resource "smtpfast_domain_verification" "test" {
  domain_id = smtpfast_domain.test.id
  timeouts {
    create = "1s"
  }
}
`,
				ExpectError: regexp.MustCompile(`(?s)mail\.example\.com is not verified after 1s.*phase dns_missing.*TXT mail\.example\.com \(SPF authorization\): missing.*found "google-site-verification=abc"`),
			},
		},
	})
}

func TestPlanApplyDomainVerificationUnknownDomain(t *testing.T) {
	skipWithoutTerraform(t)
	_, url := newFakeAPI(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      fakeProviderConfig(url) + `resource "smtpfast_domain_verification" "test" { domain_id = "dom_missing" }`,
				ExpectError: regexp.MustCompile(`Error verifying domain(.|\n)*404`),
			},
		},
	})
}

// A configuration where nothing uses SMTPfast plans and applies without a
// key; the first call that needs one fails with a clear message.
func TestPlanApplyWithoutAPIKey(t *testing.T) {
	skipWithoutTerraform(t)
	t.Setenv("SMTPFAST_API_KEY", "")
	_, url := newFakeAPI(t)
	provider := `provider "smtpfast" {
  base_url = "` + url + `"
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: provider + `resource "smtpfast_domain" "test" {
  count  = 0
  domain = "mail.example.com"
}
`,
			},
			{
				Config: provider + `resource "smtpfast_domain" "test" {
  count  = 1
  domain = "mail.example.com"
}
`,
				ExpectError: regexp.MustCompile(`missing SMTPfast API key`),
			},
			{
				Config:      provider + `data "smtpfast_domain" "test" { domain = "mail.example.com" }`,
				ExpectError: regexp.MustCompile(`missing SMTPfast API key`),
			},
			{
				// Fails at once, not after the verification timeout.
				Config:      provider + `resource "smtpfast_domain_verification" "test" { domain_id = "dom_1" }`,
				ExpectError: regexp.MustCompile(`Error verifying domain(.|\n)*missing SMTPfast API key`),
			},
		},
	})
}

func fastVerificationPolls(t *testing.T) {
	t.Helper()
	interval, maxWait := verificationPollInterval, verificationPollMax
	verificationPollInterval, verificationPollMax = 20*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { verificationPollInterval, verificationPollMax = interval, maxWait })
}
