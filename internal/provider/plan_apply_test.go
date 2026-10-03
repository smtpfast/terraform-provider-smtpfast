package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// These tests run real terraform plan/apply/import cycles against fakeAPI.
// Every step also checks that a second plan is empty, which is what catches
// perpetual diffs and "inconsistent result after apply" errors.

func TestPlanApplyAPIKey(t *testing.T) {
	skipWithoutTerraform(t)
	api, url := newFakeAPI(t)
	provider := fakeProviderConfig(url)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Omitted scopes plan as the API's default set.
				Config: provider + `resource "smtpfast_api_key" "test" { name = "ci" }`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_api_key.test", "scopes.#", "10"),
					resource.TestCheckResourceAttr("smtpfast_api_key.test", "scopes.0", "email:send"),
					resource.TestCheckResourceAttrSet("smtpfast_api_key.test", "key"),
					resource.TestCheckResourceAttr("smtpfast_api_key.test", "prefix", "sf_live_x"),
					resource.TestCheckResourceAttrSet("smtpfast_api_key.test", "created_at"),
				),
			},
			{
				// Rename and narrow the scopes in place: same key, same secret.
				Config: provider + `resource "smtpfast_api_key" "test" {
  name   = "ci-renamed"
  scopes = ["email:send", "logs:read"]
}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("smtpfast_api_key.test", plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_api_key.test", "name", "ci-renamed"),
					resource.TestCheckResourceAttr("smtpfast_api_key.test", "scopes.#", "2"),
					resource.TestCheckResourceAttr("smtpfast_api_key.test", "key", "sf_live_secret_key_1"),
				),
			},
			{
				// The API storing the same scopes in another order is not drift.
				PreConfig: func() { api.keys["key_1"].Scopes = []string{"logs:read", "email:send"} },
				Config: provider + `resource "smtpfast_api_key" "test" {
  name   = "ci-renamed"
  scopes = ["email:send", "logs:read"]
}`,
				PlanOnly: true,
			},
			{
				// A scope added in the dashboard is drift, and the apply removes it.
				PreConfig: func() { api.keys["key_1"].Scopes = []string{"email:send", "logs:read", "apikey:manage"} },
				Config: provider + `resource "smtpfast_api_key" "test" {
  name   = "ci-renamed"
  scopes = ["email:send", "logs:read"]
}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("smtpfast_api_key.test", plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckResourceAttr("smtpfast_api_key.test", "scopes.#", "2"),
			},
			{
				ResourceName:      "smtpfast_api_key.test",
				ImportState:       true,
				ImportStateVerify: true,
				// The secret is only returned on create.
				ImportStateVerifyIgnore: []string{"key"},
			},
			{
				// Revoked in the dashboard: Terraform plans a new key.
				PreConfig: func() { api.keys["key_1"].RevokedAt = "2026-10-02T00:00:00.000Z" },
				Config: provider + `resource "smtpfast_api_key" "test" {
  name   = "ci-renamed"
  scopes = ["email:send", "logs:read"]
}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("smtpfast_api_key.test", plancheck.ResourceActionCreate)},
				},
			},
		},
	})
}

func TestPlanApplyAPIKeyRejectsUnknownScope(t *testing.T) {
	skipWithoutTerraform(t)
	_, url := newFakeAPI(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fakeProviderConfig(url) + `resource "smtpfast_api_key" "test" {
  name   = "ci"
  scopes = ["emails:send"]
}`,
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
		},
	})
}

func TestPlanApplyWebhook(t *testing.T) {
	skipWithoutTerraform(t)
	_, url := newFakeAPI(t)
	provider := fakeProviderConfig(url)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Legacy unprefixed names are normalised by the API, so they
				// would come back different from the plan: refuse them.
				Config: provider + `resource "smtpfast_webhook" "test" {
  url    = "https://example.com/hooks/smtpfast"
  events = ["delivered"]
}`,
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
			{
				Config: provider + `resource "smtpfast_webhook" "test" {
  url    = "https://discord.com/api/webhooks/1/x"
  events = ["email.bounced", "email.received"]
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_webhook.test", "format", "discord"),
					resource.TestCheckResourceAttr("smtpfast_webhook.test", "active", "true"),
					resource.TestCheckResourceAttrSet("smtpfast_webhook.test", "signing_secret"),
					resource.TestCheckResourceAttrSet("smtpfast_webhook.test", "created_at"),
				),
			},
			{
				// The update response has no created_at; it must survive.
				Config: provider + `resource "smtpfast_webhook" "test" {
  url    = "https://example.com/hooks/smtpfast"
  events = ["email.delivered", "domain.updated", "contact.subscribed"]
  format = "standard"
  active = false
}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("smtpfast_webhook.test", plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_webhook.test", "active", "false"),
					resource.TestCheckResourceAttr("smtpfast_webhook.test", "format", "standard"),
					resource.TestCheckResourceAttr("smtpfast_webhook.test", "events.#", "3"),
					resource.TestCheckResourceAttrSet("smtpfast_webhook.test", "created_at"),
					resource.TestCheckResourceAttrSet("smtpfast_webhook.test", "signing_secret"),
				),
			},
			{
				ResourceName:            "smtpfast_webhook.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"signing_secret"},
			},
		},
	})
}

func TestPlanApplyWebhookCreatedPaused(t *testing.T) {
	skipWithoutTerraform(t)
	_, url := newFakeAPI(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fakeProviderConfig(url) + `resource "smtpfast_webhook" "test" {
  url    = "https://example.com/hooks/smtpfast"
  events = ["email.sent"]
  active = false
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_webhook.test", "active", "false"),
					resource.TestCheckResourceAttr("smtpfast_webhook.test", "format", "standard"),
					resource.TestCheckResourceAttrSet("smtpfast_webhook.test", "signing_secret"),
				),
			},
		},
	})
}

const testTemplateConfig = `resource "smtpfast_template" "test" {
  name         = "Order confirmation"
  alias        = "order-confirmation"
  subject      = "Your order {{{ORDER_ID}}}"
  from         = "Acme <orders@acme.com>"
  reply_to     = ["help@acme.com"]
  preview_text = "Thanks for your order"
  html         = "<p>{{{PRODUCT}}} costs {{{PRICE}}}</p>\n"

  variables = [
    { key = "ORDER_ID", type = "string" },
    { key = "PRODUCT", type = "string", fallback_value = "item" },
    { key = "PRICE", type = "number", fallback_value = "25.0" },
  ]
}
`

// A webhook paused outside Terraform stays paused while the configuration
// does not set active.
func TestPlanApplyWebhookKeepsOutOfBandPause(t *testing.T) {
	skipWithoutTerraform(t)
	api, url := newFakeAPI(t)
	config := fakeProviderConfig(url) + `resource "smtpfast_webhook" "test" {
  url    = "https://example.com/hooks/smtpfast"
  events = ["email.sent"]
}`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.TestCheckResourceAttr("smtpfast_webhook.test", "active", "true"),
			},
			{
				PreConfig: func() {
					api.mu.Lock()
					defer api.mu.Unlock()
					for _, wh := range api.webhooks {
						wh.Active = false
					}
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.TestCheckResourceAttr("smtpfast_webhook.test", "active", "false"),
			},
		},
	})
}

func TestPlanApplyTemplate(t *testing.T) {
	skipWithoutTerraform(t)
	api, url := newFakeAPI(t)
	provider := fakeProviderConfig(url)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Created and published; "25.0" coming back as 25 is not drift.
				Config: provider + testTemplateConfig,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_template.test", "published", "true"),
					resource.TestCheckResourceAttr("smtpfast_template.test", "status", "published"),
					resource.TestCheckResourceAttr("smtpfast_template.test", "has_unpublished_versions", "false"),
					resource.TestCheckResourceAttrSet("smtpfast_template.test", "current_version_id"),
					resource.TestCheckResourceAttr("smtpfast_template.test", "variables.2.fallback_value", "25.0"),
				),
			},
			{
				// An unpublished edit in the dashboard shows up as drift.
				PreConfig: func() {
					for _, tpl := range api.templates {
						tpl.Content["html"] = "<p>Edited in the dashboard</p>"
						tpl.UpdatedAt = api.now()
					}
				},
				Config:             provider + testTemplateConfig,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				// Applying puts the configured content back and publishes it.
				Config: provider + testTemplateConfig,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("smtpfast_template.test", plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_template.test", "has_unpublished_versions", "false"),
					resource.TestCheckResourceAttr("smtpfast_template.test", "published", "true"),
				),
			},
			{
				// Switch to markdown, drop optional fields: they are cleared.
				Config: provider + `resource "smtpfast_template" "test" {
  name     = "Order confirmation"
  markdown = "# Thanks"
  text     = ""
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("smtpfast_template.test", "html"),
					resource.TestCheckNoResourceAttr("smtpfast_template.test", "alias"),
					resource.TestCheckNoResourceAttr("smtpfast_template.test", "variables"),
					resource.TestCheckResourceAttr("smtpfast_template.test", "text", ""),
				),
			},
			{
				// Someone edits the template between the plan and the apply.
				PreConfig: func() { api.conflictNextTemplateWrite = true },
				Config: provider + `resource "smtpfast_template" "test" {
  name     = "Order confirmation v2"
  markdown = "# Thanks"
  text     = ""
}`,
				ExpectError: regexp.MustCompile(`changed in SMTPfast`),
			},
			{
				// The next plan shows the dashboard edit, and applying wins it back.
				Config: provider + `resource "smtpfast_template" "test" {
  name     = "Order confirmation v2"
  markdown = "# Thanks"
  text     = ""
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_template.test", "name", "Order confirmation v2"),
					resource.TestCheckNoResourceAttr("smtpfast_template.test", "subject"),
				),
			},
			{
				ResourceName:      "smtpfast_template.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestPlanApplyTemplateDraftOnly(t *testing.T) {
	skipWithoutTerraform(t)
	_, url := newFakeAPI(t)
	provider := fakeProviderConfig(url)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: provider + `resource "smtpfast_template" "test" {
  name      = "Draft"
  html      = "<p>Hi</p>"
  published = false
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_template.test", "status", "draft"),
					resource.TestCheckResourceAttr("smtpfast_template.test", "has_unpublished_versions", "true"),
					resource.TestCheckNoResourceAttr("smtpfast_template.test", "current_version_id"),
				),
			},
			{
				// Turning publishing on publishes the existing draft in place.
				Config: provider + `resource "smtpfast_template" "test" {
  name = "Draft"
  html = "<p>Hi</p>"
}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("smtpfast_template.test", plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_template.test", "status", "published"),
					resource.TestCheckResourceAttr("smtpfast_template.test", "has_unpublished_versions", "false"),
				),
			},
		},
	})
}

func TestPlanApplyTemplateValidation(t *testing.T) {
	skipWithoutTerraform(t)
	_, url := newFakeAPI(t)
	provider := fakeProviderConfig(url)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      provider + `resource "smtpfast_template" "test" { name = "No body" }`,
				ExpectError: regexp.MustCompile(`(?s)html.*markdown`),
			},
			{
				Config: provider + `resource "smtpfast_template" "test" {
  name      = "Bad fallback"
  html      = "<p>{{{PRICE}}}</p>"
  variables = [{ key = "PRICE", type = "number", fallback_value = "cheap" }]
}`,
				ExpectError: regexp.MustCompile(`is not a number`),
			},
			{
				Config: provider + `resource "smtpfast_template" "test" {
  name      = "Duplicate"
  html      = "<p>{{{plan}}}</p>"
  variables = [{ key = "plan", type = "string" }, { key = "PLAN", type = "string" }]
}`,
				ExpectError: regexp.MustCompile(`declared more than once`),
			},
			{
				Config: provider + `resource "smtpfast_template" "test" {
  name      = "Reserved"
  html      = "<p>{{{EMAIL}}}</p>"
  variables = [{ key = "EMAIL", type = "string" }]
}`,
				ExpectError: regexp.MustCompile(`reserved`),
			},
		},
	})
}

func TestPlanApplyInbox(t *testing.T) {
	skipWithoutTerraform(t)
	_, url := newFakeAPI(t)
	provider := fakeProviderConfig(url)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: provider + `resource "smtpfast_inbox" "test" {
  email_address = "Support@Inbound.example.com"
}`,
				ExpectError: regexp.MustCompile(`use lowercase`),
			},
			{
				Config: provider + `resource "smtpfast_inbox" "test" {
  email_address = "support@inbound.example.com"
  from_name     = "Ada from Support"
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_inbox.test", "name", "support@inbound.example.com"),
					resource.TestCheckResourceAttr("smtpfast_inbox.test", "domain_id", "dom_inbound"),
				),
			},
			{
				// Renamed, and from_name removed from the configuration is cleared.
				Config: provider + `resource "smtpfast_inbox" "test" {
  email_address = "support@inbound.example.com"
  name          = "Support"
}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("smtpfast_inbox.test", plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_inbox.test", "name", "Support"),
					resource.TestCheckNoResourceAttr("smtpfast_inbox.test", "from_name"),
				),
			},
			{
				ResourceName:      "smtpfast_inbox.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: provider + `resource "smtpfast_inbox" "test" {
  email_address = "help@inbound.example.com"
  name          = "Support"
}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("smtpfast_inbox.test", plancheck.ResourceActionDestroyBeforeCreate)},
				},
			},
		},
	})
}

func TestPlanApplyContactProperty(t *testing.T) {
	skipWithoutTerraform(t)
	_, url := newFakeAPI(t)
	provider := fakeProviderConfig(url)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: provider + `resource "smtpfast_contact_property" "test" {
  key            = "seats"
  type           = "number"
  fallback_value = "1"
}`,
				Check: resource.TestCheckResourceAttr("smtpfast_contact_property.test", "fallback_value", "1"),
			},
			{
				Config: provider + `resource "smtpfast_contact_property" "test" {
  key            = "seats"
  type           = "number"
  fallback_value = "2.5"
}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("smtpfast_contact_property.test", plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckResourceAttr("smtpfast_contact_property.test", "fallback_value", "2.5"),
			},
			{
				Config: provider + `resource "smtpfast_contact_property" "test" {
  key  = "seats"
  type = "number"
}`,
				Check: resource.TestCheckNoResourceAttr("smtpfast_contact_property.test", "fallback_value"),
			},
			{
				ResourceName:      "smtpfast_contact_property.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: provider + `resource "smtpfast_contact_property" "test" {
  key  = "seats"
  type = "string"
}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("smtpfast_contact_property.test", plancheck.ResourceActionDestroyBeforeCreate)},
				},
			},
		},
	})
}

func TestPlanApplyDomainDataSourceByName(t *testing.T) {
	skipWithoutTerraform(t)
	api, url := newFakeAPI(t)
	api.domains["dom_1"] = &fakeDomain{ID: "dom_1", Domain: "mail.example.com", Status: "verified"}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fakeProviderConfig(url) + `data "smtpfast_domain" "test" { domain = "mail.example.com" }`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.smtpfast_domain.test", "id", "dom_1"),
					resource.TestCheckResourceAttr("data.smtpfast_domain.test", "status", "verified"),
					resource.TestCheckResourceAttr("data.smtpfast_domain.test", "dns_records.3.priority", "10"),
					resource.TestCheckNoResourceAttr("data.smtpfast_domain.test", "dns_records.1.priority"),
				),
			},
			{
				Config:      fakeProviderConfig(url) + `data "smtpfast_domain" "test" { domain = "other.example.com" }`,
				ExpectError: regexp.MustCompile(`No domain named`),
			},
		},
	})
}

// The README pattern: a for_each over dns_records. Once the domain exists,
// turning receiving on or off must keep the records known at plan time, or
// that for_each fails.
func TestPlanApplyDomainReceivingToggle(t *testing.T) {
	skipWithoutTerraform(t)
	api, url := newFakeAPI(t)
	provider := fakeProviderConfig(url)
	domain := func(receiving string) string {
		return provider + `resource "smtpfast_domain" "test" {
  domain = "mail.example.com"
` + receiving + `
}
`
	}
	config := func(receiving string) string {
		return domain(receiving) + `
resource "terraform_data" "record" {
  # Same plan-time need as the README's for_each: the list length must be
  # known. (count, because the test framework cannot read for_each state.)
  count = length(smtpfast_domain.test.dns_records)
  input = smtpfast_domain.test.dns_records[count.index]
}
`
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// On create the records are unknown until the domain exists,
				// so the DNS resources come in the next apply.
				Config: domain(""),
			},
			{
				Config: config(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_domain.test", "dns_records.#", "5"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "dns_records.3.priority", "10"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "receiving_enabled", "false"),
				),
			},
			{
				PreConfig: func() {
					for _, d := range api.domains {
						d.Status = "verified"
					}
				},
				Config: config("  receiving_enabled = true"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_domain.test", "dns_records.#", "6"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "dns_records.5.name", "mail.example.com"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "receiving_status", "pending"),
					resource.TestCheckResourceAttr("terraform_data.record.5", "output.type", "MX"),
				),
			},
			{
				Config: config("  receiving_enabled = false"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_domain.test", "dns_records.#", "5"),
					resource.TestCheckResourceAttr("smtpfast_domain.test", "receiving_status", "disabled"),
				),
			},
			{
				ResourceName:      "smtpfast_domain.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
