package view_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/dbops"
	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/testutils/factories"
	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/testutils/runner"
)

func TestView_acceptance(t *testing.T) {
	databaseName := fmt.Sprintf("posthog_%s", acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum))

	checkNotExistsFunc := func(ctx context.Context, dbopsClient dbops.Client, clusterName *string, attrs map[string]string) (bool, error) {
		view, err := dbopsClient.GetView(ctx, attrs["database"], attrs["name"], clusterName)
		return view != nil, err
	}

	checkAttributesFunc := func(ctx context.Context, dbopsClient dbops.Client, clusterName *string, attrs map[string]interface{}) error {
		view, err := dbopsClient.GetView(ctx, attrs["database"].(string), attrs["name"].(string), clusterName)
		if err != nil {
			return err
		}
		if view == nil {
			return fmt.Errorf("view %s.%s was not found", attrs["database"], attrs["name"])
		}
		if !strings.Contains(view.CreateStatement, "CREATE VIEW") {
			return fmt.Errorf("expected create statement to describe a view, got %q", view.CreateStatement)
		}
		if !strings.Contains(view.CreateStatement, "GROUP BY team_id") {
			return fmt.Errorf("expected create statement to contain the query, got %q", view.CreateStatement)
		}
		return nil
	}

	resource := fmt.Sprintf(`
locals {
  event_columns = [
    { name = "team_id", type = "UInt64", nullable = false },
    { name = "event", type = "String", nullable = false },
    { name = "created_at", type = "DateTime", nullable = false, default_expression = "now()" },
  ]
  team_event_count_columns = [
    { name = "team_id", type = "UInt64", nullable = false },
    { name = "event_count", type = "UInt64", nullable = false },
  ]
}

resource "clickhousedbops_database" "posthog" {
  name = %q
}

resource "clickhousedbops_table" "events" {
  database = clickhousedbops_database.posthog.name
  name     = "events"
  engine   = "MergeTree()"
  order_by = "(team_id, created_at)"
  columns  = local.event_columns
}

resource "clickhousedbops_view" "team_event_counts" {
  database = clickhousedbops_database.posthog.name
  name     = "team_event_counts"
  columns  = local.team_event_count_columns
  query    = <<-SQL
    SELECT team_id, count() AS event_count
    FROM ${clickhousedbops_table.events.database}.${clickhousedbops_table.events.name}
    GROUP BY team_id
  SQL
}
`, databaseName)

	runner.RunTests(t, []runner.TestCase{
		{
			Name:                "Create view from shared local columns",
			ChEnv:               map[string]string{"CONFIGFILE": "config-single.xml"},
			Protocol:            "native",
			Resource:            resource,
			ResourceName:        "team_event_counts",
			ResourceAddress:     "clickhousedbops_view.team_event_counts",
			CheckNotExistsFunc:  checkNotExistsFunc,
			CheckAttributesFunc: checkAttributesFunc,
		},
	})
}

func TestView_planAllowsSharedTableColumns(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories.ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
locals {
  shared_columns = [
    { name = "team_id", type = "UInt64", nullable = false },
    { name = "created_at", type = "DateTime", nullable = false, default_expression = "now()" },
  ]
}

resource "clickhousedbops_view" "test" {
  database = "posthog"
  name     = "events_view"
  columns  = local.shared_columns
  query    = "SELECT team_id, created_at FROM posthog.events"
}
`,
				PlanOnly: true,
			},
		},
	})
}
