package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccWebhookResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `resource "smtpfast_webhook" "test" {
  url    = "https://example.com/tf-acc"
  events = ["email.delivered"]
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_webhook.test", "url", "https://example.com/tf-acc"),
					resource.TestCheckResourceAttr("smtpfast_webhook.test", "events.#", "1"),
					resource.TestCheckResourceAttrSet("smtpfast_webhook.test", "id"),
				),
			},
			{
				// Update the URL and add an event.
				Config: `resource "smtpfast_webhook" "test" {
  url    = "https://example.com/tf-acc-updated"
  events = ["email.delivered", "email.bounced"]
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_webhook.test", "url", "https://example.com/tf-acc-updated"),
					resource.TestCheckResourceAttr("smtpfast_webhook.test", "events.#", "2"),
				),
			},
			{
				ResourceName:      "smtpfast_webhook.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
