package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
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

const testSignupFormFull = `resource "smtpfast_signup_form" "test" {
  name                    = "Newsletter"
  fields                  = ["first_name", "email", "last_name"]
  button_text             = "Join"
  button_color            = "#0af"
  success_message         = ""
  double_opt_in           = false
  redirect_url            = "https://example.com/thanks"
  captcha_enabled         = true
  turnstile_site_key      = "0x4AAA"
  turnstile_secret_key    = "0x4BBB"
  block_disposable_emails = false
  active                  = false
  confirmation_email_from = "Acme <hello@mail.example.com>"
  welcome_email_enabled   = true
  welcome_email_from      = "Acme <hello@mail.example.com>"
  welcome_email_subject   = "Welcome"
  welcome_email_markdown  = "# Hi\n"
}
`

func TestPlanApplySignupForm(t *testing.T) {
	skipWithoutTerraform(t)
	api, url := newFakeAPI(t)
	provider := fakeProviderConfig(url)
	const name = "smtpfast_signup_form.test"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Omitted settings plan as the defaults of a new form.
				Config: provider + `resource "smtpfast_signup_form" "test" { name = "Newsletter" }`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "fields.#", "2"),
					resource.TestCheckResourceAttr(name, "fields.1", "first_name"),
					resource.TestCheckResourceAttr(name, "button_text", "Subscribe"),
					resource.TestCheckResourceAttr(name, "button_color", "#10b981"),
					resource.TestCheckResourceAttr(name, "success_message", "Thanks for subscribing!"),
					resource.TestCheckResourceAttr(name, "double_opt_in", "true"),
					resource.TestCheckResourceAttr(name, "captcha_enabled", "false"),
					resource.TestCheckResourceAttr(name, "turnstile_secret_configured", "false"),
					resource.TestCheckResourceAttr(name, "active", "true"),
					resource.TestCheckResourceAttr(name, "welcome_email_enabled", "false"),
					resource.TestCheckNoResourceAttr(name, "redirect_url"),
					resource.TestCheckResourceAttrSet(name, "created_at"),
				),
			},
			{
				Config: provider + testSignupFormFull,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "fields.#", "3"),
					resource.TestCheckResourceAttr(name, "fields.0", "first_name"),
					resource.TestCheckResourceAttr(name, "success_message", ""),
					resource.TestCheckResourceAttr(name, "turnstile_secret_configured", "true"),
					resource.TestCheckResourceAttr(name, "welcome_email_markdown", "# Hi\n"),
					resource.TestCheckResourceAttr(name, "confirmation_email_from", "Acme <hello@mail.example.com>"),
				),
			},
			{
				// The secret is write-only: cleared in the dashboard shows up
				// as drift, and the apply sends it again.
				PreConfig:          func() { api.forms["form_1"].SecretKey = nil },
				Config:             provider + testSignupFormFull,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: provider + testSignupFormFull,
				Check:  resource.TestCheckResourceAttr(name, "turnstile_secret_configured", "true"),
			},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"turnstile_secret_key"},
			},
			{
				// Captcha stays on while omitted, so dropping the keys is refused.
				Config:      provider + `resource "smtpfast_signup_form" "test" { name = "Newsletter" }`,
				ExpectError: regexp.MustCompile(`Turnstile keys required`),
			},
			{
				// Settings with a default keep their value; text settings clear.
				Config: provider + `resource "smtpfast_signup_form" "test" {
  name            = "Newsletter"
  captcha_enabled = false
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "button_text", "Join"),
					resource.TestCheckResourceAttr(name, "active", "false"),
					resource.TestCheckResourceAttr(name, "fields.#", "3"),
					resource.TestCheckResourceAttr(name, "welcome_email_enabled", "true"),
					resource.TestCheckResourceAttr(name, "turnstile_secret_configured", "false"),
					resource.TestCheckNoResourceAttr(name, "turnstile_site_key"),
					resource.TestCheckNoResourceAttr(name, "redirect_url"),
					resource.TestCheckNoResourceAttr(name, "welcome_email_markdown"),
				),
			},
			{
				// Turned back on in the dashboard: not drift while omitted.
				PreConfig: func() { api.forms["form_1"].Active = true },
				Config: provider + `resource "smtpfast_signup_form" "test" {
  name            = "Newsletter"
  captcha_enabled = false
}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.TestCheckResourceAttr(name, "active", "true"),
			},
		},
	})
}

// Create ignores the email settings, so they take an update right after.
func TestPlanApplySignupFormCreatedWithEmails(t *testing.T) {
	skipWithoutTerraform(t)
	_, url := newFakeAPI(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fakeProviderConfig(url) + testSignupFormFull,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_signup_form.test", "welcome_email_enabled", "true"),
					resource.TestCheckResourceAttr("smtpfast_signup_form.test", "welcome_email_subject", "Welcome"),
					resource.TestCheckResourceAttr("smtpfast_signup_form.test", "confirmation_email_from", "Acme <hello@mail.example.com>"),
					resource.TestCheckResourceAttr("smtpfast_signup_form.test", "turnstile_secret_configured", "true"),
				),
			},
		},
	})
}

func TestPlanApplySignupFormValidation(t *testing.T) {
	skipWithoutTerraform(t)
	_, url := newFakeAPI(t)
	provider := fakeProviderConfig(url)
	form := func(body string) string {
		return provider + "resource \"smtpfast_signup_form\" \"test\" {\n  name = \"Newsletter\"\n" + body + "\n}\n"
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: form(`  fields = ["first_name"]`), ExpectError: regexp.MustCompile(`must include "email"`)},
			{Config: form(`  button_text = "Join "`), ExpectError: regexp.MustCompile(`leading or trailing whitespace`)},
			{Config: form(`  button_color = "10b981"`), ExpectError: regexp.MustCompile(`hex color`)},
			{Config: form(`  redirect_url = "ftp://example.com"`), ExpectError: regexp.MustCompile(`http:// or https://`)},
			{Config: form(`  success_message = " Thanks"`), ExpectError: regexp.MustCompile(`start or end with whitespace`)},
			{Config: form(`  captcha_enabled = true`), ExpectError: regexp.MustCompile(`Turnstile keys required`)},
		},
	})
}

func TestPlanApplySegment(t *testing.T) {
	skipWithoutTerraform(t)
	api, url := newFakeAPI(t)
	provider := fakeProviderConfig(url)
	const name = "smtpfast_segment.test"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: provider + `resource "smtpfast_segment" "test" {
  name        = "VIP"
  description = "Top customers.\nReviewed monthly."
  color       = "#10B981"
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "color", "#10B981"),
					resource.TestCheckResourceAttrSet(name, "created_at"),
				),
			},
			{
				// Renamed in place; removed description and color are cleared.
				Config: provider + `resource "smtpfast_segment" "test" { name = "VIP customers" }

data "smtpfast_segment" "by_name" {
  name = smtpfast_segment.test.name
}

data "smtpfast_segment" "by_id" {
  id = smtpfast_segment.test.id
}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "name", "VIP customers"),
					resource.TestCheckNoResourceAttr(name, "description"),
					resource.TestCheckNoResourceAttr(name, "color"),
					resource.TestCheckResourceAttrPair("data.smtpfast_segment.by_name", "id", name, "id"),
					resource.TestCheckResourceAttr("data.smtpfast_segment.by_id", "name", "VIP customers"),
				),
			},
			{
				ResourceName:      name,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Deleted in the dashboard: Terraform plans it again.
				PreConfig:          func() { delete(api.segments, "seg_1") },
				Config:             provider + `resource "smtpfast_segment" "test" { name = "VIP customers" }`,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestPlanApplySegmentValidation(t *testing.T) {
	skipWithoutTerraform(t)
	_, url := newFakeAPI(t)
	provider := fakeProviderConfig(url)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      provider + `resource "smtpfast_segment" "test" { name = "VIP " }`,
				ExpectError: regexp.MustCompile(`leading or trailing whitespace`),
			},
			{
				Config: provider + `resource "smtpfast_segment" "test" {
  name  = "VIP"
  color = "#abc"
}`,
				ExpectError: regexp.MustCompile(`six-digit hex color`),
			},
			{
				Config: provider + `resource "smtpfast_segment" "a" { name = "VIP" }
resource "smtpfast_segment" "b" {
  name       = "VIP"
  depends_on = [smtpfast_segment.a]
}`,
				ExpectError: regexp.MustCompile(`Segment already exists`),
			},
			{
				Config:      provider + `data "smtpfast_segment" "test" { name = "Nobody" }`,
				ExpectError: regexp.MustCompile(`No segment named`),
			},
		},
	})
}

func TestPlanApplyInboxLabel(t *testing.T) {
	skipWithoutTerraform(t)
	api, url := newFakeAPI(t)
	for _, id := range []string{"inb_a", "inb_b"} {
		api.inboxes[id] = &fakeInbox{ID: id, Name: id, EmailAddress: id + "@inbound.example.com", DomainID: "dom_inbound", CreatedAt: "2026-10-01T09:00:00.000Z"}
	}
	provider := fakeProviderConfig(url)
	const name = "smtpfast_inbox_label.test"
	label := func(body string) string {
		return provider + "resource \"smtpfast_inbox_label\" \"test\" {\n" + body + "\n}\n"
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: label(`  inbox_id = "inb_a"
  name     = "Urgent"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "color", "mauve"),
					resource.TestCheckResourceAttrSet(name, "created_at"),
				),
			},
			{
				Config: label(`  inbox_id = "inb_a"
  name     = "Very urgent"
  color    = "crimson"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "name", "Very urgent"),
					resource.TestCheckResourceAttr(name, "color", "crimson"),
				),
			},
			{
				// A color picked in the dashboard stays while color is omitted.
				PreConfig: func() {
					for _, l := range api.labels {
						l.Color = "teal"
					}
				},
				Config: label(`  inbox_id = "inb_a"
  name     = "Very urgent"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.TestCheckResourceAttr(name, "color", "teal"),
			},
			{
				ResourceName:      name,
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources[name]
					return rs.Primary.Attributes["inbox_id"] + "/" + rs.Primary.ID, nil
				},
			},
			{
				ResourceName:  name,
				ImportState:   true,
				ImportStateId: "lbl_2",
				ExpectError:   regexp.MustCompile(`Expected <inbox_id>/<label_id>`),
			},
			{
				Config: label(`  inbox_id = "inb_b"
  name     = "Very urgent"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionDestroyBeforeCreate)},
				},
				Check: resource.TestCheckResourceAttr(name, "color", "mauve"),
			},
			{
				// The whole inbox deleted: the label goes with it.
				PreConfig: func() { delete(api.inboxes, "inb_b") },
				Config: label(`  inbox_id = "inb_b"
  name     = "Very urgent"`),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func newFakeTeam(t *testing.T) (*fakeAPI, string) {
	t.Helper()
	api, url := newFakeAPI(t)
	api.members["mem_owner"] = &fakeMember{ID: "mem_owner", UserID: "usr_owner", Email: "owner@example.com", Role: "owner", CreatedAt: "2026-09-01T10:00:00.000Z"}
	api.callerMemberID = "mem_owner"
	return api, url
}

func TestPlanApplyTeamInvite(t *testing.T) {
	skipWithoutTerraform(t)
	api, url := newFakeTeam(t)
	provider := fakeProviderConfig(url)
	const name = "smtpfast_team_invite.test"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: provider + `resource "smtpfast_team_invite" "test" { email = "ada@example.com" }`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "role", "member"),
					resource.TestCheckResourceAttr(name, "status", "pending"),
					resource.TestCheckResourceAttrSet(name, "expires_at"),
				),
			},
			{
				ResourceName:      name,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      name,
				ImportState:       true,
				ImportStateId:     "ada@example.com",
				ImportStateVerify: true,
			},
			{
				// Invitations are replaced, not updated.
				Config: provider + `resource "smtpfast_team_invite" "test" {
  email = "ada@example.com"
  role  = "admin"
}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionDestroyBeforeCreate)},
				},
				Check: resource.TestCheckResourceAttr(name, "role", "admin"),
			},
			{
				// Accepted: no longer pending, but nothing to change.
				PreConfig: func() { api.acceptInvite("ada@example.com") },
				Config: provider + `resource "smtpfast_team_invite" "test" {
  email = "ada@example.com"
  role  = "admin"
}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.TestCheckResourceAttr(name, "status", "accepted"),
			},
			{
				// Destroying an accepted invitation leaves the person on the team.
				Config: provider,
				Check: func(*terraform.State) error {
					for _, m := range api.members {
						if m.Email == "ada@example.com" && m.Role == "admin" {
							return nil
						}
					}
					return fmt.Errorf("ada@example.com is no longer an admin on the team")
				},
			},
		},
	})
}

func TestPlanApplyTeamInviteExpired(t *testing.T) {
	skipWithoutTerraform(t)
	api, url := newFakeTeam(t)
	config := fakeProviderConfig(url) + `resource "smtpfast_team_invite" "test" { email = "ada@example.com" }`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if len(api.invites) != 0 {
				return fmt.Errorf("invitation not revoked: %v", api.invites)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					for _, inv := range api.invites {
						inv.Expired = true
					}
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("smtpfast_team_invite.test", plancheck.ResourceActionCreate)},
				},
				Check: resource.TestCheckResourceAttr("smtpfast_team_invite.test", "status", "pending"),
			},
		},
	})
}

func TestPlanApplyTeamInviteErrors(t *testing.T) {
	skipWithoutTerraform(t)
	api, url := newFakeTeam(t)
	provider := fakeProviderConfig(url)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      provider + `resource "smtpfast_team_invite" "test" { email = "owner@example.com" }`,
				ExpectError: regexp.MustCompile(`already a member`),
			},
			{
				Config: provider + `resource "smtpfast_team_invite" "test" {
  email = "ada@example.com"
  role  = "owner"
}`,
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
			{
				PreConfig:   func() { api.freeTeam = true },
				Config:      provider + `resource "smtpfast_team_invite" "test" { email = "ada@example.com" }`,
				ExpectError: regexp.MustCompile(`requires a paid plan`),
			},
		},
	})
}

func TestPlanApplyTeamMember(t *testing.T) {
	skipWithoutTerraform(t)
	api, url := newFakeTeam(t)
	api.members["mem_grace"] = &fakeMember{ID: "mem_grace", UserID: "usr_grace", Email: "grace@example.com", Role: "member", CreatedAt: "2026-09-02T10:00:00.000Z"}
	provider := fakeProviderConfig(url)
	const name = "smtpfast_team_member.test"
	member := func(body string) string {
		return provider + "resource \"smtpfast_team_member\" \"test\" {\n" + body + "\n}\n"
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if _, ok := api.members["mem_grace"]; ok {
				return fmt.Errorf("destroy did not remove grace@example.com from the team")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				// Adopted as is.
				Config: member(`  email = "grace@example.com"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "id", "mem_grace"),
					resource.TestCheckResourceAttr(name, "user_id", "usr_grace"),
					resource.TestCheckResourceAttr(name, "role", "member"),
					resource.TestCheckResourceAttr(name, "can_manage_billing", "false"),
					resource.TestCheckNoResourceAttr(name, "name"),
				),
			},
			{
				Config: member(`  email              = "grace@example.com"
  role               = "admin"
  can_manage_billing = true`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "role", "admin"),
					resource.TestCheckResourceAttr(name, "can_manage_billing", "true"),
				),
			},
			{
				// Demoted in the dashboard: drift, and the apply restores it.
				PreConfig: func() { api.members["mem_grace"].Role = "member" },
				Config: member(`  email              = "grace@example.com"
  role               = "admin"
  can_manage_billing = true`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckResourceAttr(name, "role", "admin"),
			},
			{
				ResourceName:      name,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      name,
				ImportState:       true,
				ImportStateId:     "grace@example.com",
				ImportStateVerify: true,
			},
			{
				// Owners always manage billing, so it plans as true.
				Config: member(`  email = "grace@example.com"
  role  = "owner"`),
				Check: resource.TestCheckResourceAttr(name, "can_manage_billing", "true"),
			},
			{
				// After a demotion the API reports the member's own flag again.
				Config: member(`  email = "grace@example.com"
  role  = "member"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "role", "member"),
					resource.TestCheckResourceAttr(name, "can_manage_billing", "true"),
				),
			},
			{
				Config: member(`  email              = "grace@example.com"
  role               = "owner"
  can_manage_billing = false`),
				ExpectError: regexp.MustCompile(`Owners always manage billing`),
			},
			{
				// The same person named by user ID instead: nothing to do.
				Config: member(`  user_id = "usr_grace"
  role    = "member"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

func TestPlanApplyTeamMemberErrors(t *testing.T) {
	skipWithoutTerraform(t)
	_, url := newFakeTeam(t)
	provider := fakeProviderConfig(url)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      provider + `resource "smtpfast_team_member" "test" { email = "ada@example.com" }`,
				ExpectError: regexp.MustCompile(`(?s)Not a team member.*smtpfast_team_invite for "ada@example.com"`),
			},
			{
				Config: provider + `resource "smtpfast_team_member" "test" {
  email   = "owner@example.com"
  user_id = "usr_owner"
}`,
				ExpectError: regexp.MustCompile(`(?i)exactly one`),
			},
			{
				// The key's own user: adopting works, removing is refused...
				Config: provider + `resource "smtpfast_team_member" "self" { email = "owner@example.com" }`,
				Check:  resource.TestCheckResourceAttr("smtpfast_team_member.self", "role", "owner"),
			},
			{
				Config:      provider,
				ExpectError: regexp.MustCompile(`Cannot remove the API key's own user`),
			},
			{
				// ...so stop managing it without destroying it.
				Config: provider + `removed {
  from = smtpfast_team_member.self
  lifecycle {
    destroy = false
  }
}`,
			},
		},
	})
}

// Accepted is final: when the person later leaves or is removed, Terraform
// must not send them a new invitation by itself.
func TestPlanApplyTeamInviteStaysAcceptedAfterRemoval(t *testing.T) {
	skipWithoutTerraform(t)
	api, url := newFakeTeam(t)
	config := fakeProviderConfig(url) + `resource "smtpfast_team_invite" "test" {
  email = "ada@example.com"
  role  = "admin"
}`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() { api.acceptInvite("ada@example.com") },
				Config:    config,
				Check:     resource.TestCheckResourceAttr("smtpfast_team_invite.test", "status", "accepted"),
			},
			{
				PreConfig: func() {
					api.mu.Lock()
					defer api.mu.Unlock()
					for id, m := range api.members {
						if m.Email == "ada@example.com" {
							delete(api.members, id)
						}
					}
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_team_invite.test", "status", "accepted"),
					func(*terraform.State) error {
						for _, inv := range api.invites {
							if !inv.Accepted {
								return fmt.Errorf("a new invitation was sent to %s", inv.Email)
							}
						}
						return nil
					},
				),
			},
		},
	})
}

// With create_before_destroy, the replacement's create re-sends the same
// invitation (same id and role, new expiry). Destroying the old instance must
// not then revoke it.
func TestPlanApplyTeamInviteCreateBeforeDestroy(t *testing.T) {
	skipWithoutTerraform(t)
	api, url := newFakeTeam(t)
	config := func(trigger string) string {
		return fakeProviderConfig(url) + `resource "terraform_data" "trigger" {
  input = "` + trigger + `"
}

resource "smtpfast_team_invite" "test" {
  email = "ada@example.com"

  lifecycle {
    create_before_destroy = true
    replace_triggered_by  = [terraform_data.trigger]
  }
}`
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config("1")},
			{
				Config: config("2"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("smtpfast_team_invite.test", plancheck.ResourceActionCreateBeforeDestroy)},
				},
				Check: func(*terraform.State) error {
					if len(api.invites) != 1 {
						return fmt.Errorf("want the re-sent invitation to stay pending, have %d invitations", len(api.invites))
					}
					return nil
				},
			},
		},
	})
}

// A refused change during adoption must leave nothing in state: a tainted
// member would be replaced on the next apply, and replacing removes them.
func TestPlanApplyTeamMemberFailedAdoptionLeavesNoState(t *testing.T) {
	skipWithoutTerraform(t)
	api, url := newFakeTeam(t)
	api.members["mem_grace"] = &fakeMember{ID: "mem_grace", UserID: "usr_grace", Email: "grace@example.com", Role: "member", CreatedAt: "2026-09-02T10:00:00.000Z"}
	api.callerRole = "admin"
	provider := fakeProviderConfig(url)
	graceUntouched := func(*terraform.State) error {
		m, ok := api.members["mem_grace"]
		if !ok {
			return fmt.Errorf("grace@example.com was removed from the team")
		}
		if m.Role != "admin" || m.Billing {
			return fmt.Errorf("grace@example.com is %s with billing %t, want admin without billing", m.Role, m.Billing)
		}
		return nil
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// An admin's key cannot grant billing access.
				Config: provider + `resource "smtpfast_team_member" "test" {
  email              = "grace@example.com"
  role               = "admin"
  can_manage_billing = true
}`,
				ExpectError: regexp.MustCompile(`Only an owner can change billing access`),
			},
			{
				// Nothing was recorded, so this adopts again instead of
				// replacing (which would remove her).
				PreConfig: func() { api.members["mem_grace"].Role = "admin" },
				Config: provider + `resource "smtpfast_team_member" "test" {
  email = "grace@example.com"
  role  = "admin"
}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("smtpfast_team_member.test", plancheck.ResourceActionCreate)},
				},
				Check: graceUntouched,
			},
		},
	})
}
