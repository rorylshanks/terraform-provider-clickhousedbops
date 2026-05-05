package materializedview_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/dbops"
	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/testutils/factories"
	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/testutils/runner"
)

func TestMaterializedView_acceptance(t *testing.T) {
	databaseName := fmt.Sprintf("posthog_%s", acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum))

	checkNotExistsFunc := func(ctx context.Context, dbopsClient dbops.Client, clusterName *string, attrs map[string]string) (bool, error) {
		view, err := dbopsClient.GetMaterializedView(ctx, attrs["database"], attrs["name"], clusterName)
		return view != nil, err
	}

	checkAttributesFunc := func(ctx context.Context, dbopsClient dbops.Client, clusterName *string, attrs map[string]interface{}) error {
		view, err := dbopsClient.GetMaterializedView(ctx, attrs["database"].(string), attrs["name"].(string), clusterName)
		if err != nil {
			return err
		}
		if view == nil {
			return fmt.Errorf("materialized view %s.%s was not found", attrs["database"], attrs["name"])
		}
		if !strings.Contains(view.CreateStatement, "CREATE MATERIALIZED VIEW") {
			return fmt.Errorf("expected create statement to describe a materialized view, got %q", view.CreateStatement)
		}
		if !strings.Contains(view.CreateStatement, "TO "+attrs["database"].(string)+".daily_event_counts") {
			return fmt.Errorf("expected create statement to reference the destination table, got %q", view.CreateStatement)
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
  daily_count_columns = [
    { name = "team_id", type = "UInt64", nullable = false },
    { name = "event_date", type = "Date", nullable = false },
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

resource "clickhousedbops_table" "daily_event_counts" {
  database = clickhousedbops_database.posthog.name
  name     = "daily_event_counts"
  engine   = "MergeTree()"
  order_by = "(team_id, event_date)"
  columns  = local.daily_count_columns
}

resource "clickhousedbops_materialized_view" "events_daily_mv" {
  database   = clickhousedbops_database.posthog.name
  name       = "events_daily_mv"
  to_table   = format("%%s.%%s", clickhousedbops_table.daily_event_counts.database, clickhousedbops_table.daily_event_counts.name)
  to_columns = local.daily_count_columns
  query      = <<-SQL
    SELECT team_id, toDate(created_at) AS event_date, count() AS event_count
    FROM ${clickhousedbops_table.events.database}.${clickhousedbops_table.events.name}
    GROUP BY team_id, event_date
  SQL
}
`, databaseName)

	runner.RunTests(t, []runner.TestCase{
		{
			Name:                "Create materialized view wired to a managed target table",
			ChEnv:               map[string]string{"CONFIGFILE": "config-single.xml"},
			Protocol:            "native",
			Resource:            resource,
			ResourceName:        "events_daily_mv",
			ResourceAddress:     "clickhousedbops_materialized_view.events_daily_mv",
			CheckNotExistsFunc:  checkNotExistsFunc,
			CheckAttributesFunc: checkAttributesFunc,
		},
	})
}

func TestMaterializedView_validation(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories.ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
resource "clickhousedbops_materialized_view" "test" {
  database   = "posthog"
  name       = "events_daily_mv"
  engine     = "MergeTree()"
  order_by   = "team_id"
  to_columns = [
    { name = "team_id", type = "UInt64", nullable = false }
  ]
  query = "SELECT 1"
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Combination.*to_columns.*cannot be combined with 'engine'`),
			},
			{
				Config: `
resource "clickhousedbops_materialized_view" "test" {
  database = "posthog"
  name     = "events_daily_mv"
  engine   = "MergeTree()"
  query    = "SELECT 1"
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)Missing Required Attribute.*order_by`),
			},
			{
				Config: `
resource "clickhousedbops_materialized_view" "test" {
  database = "posthog"
  name     = "events_daily_mv"
  to_table = "posthog.daily_event_counts"
  populate = true
  query    = "SELECT 1"
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Combination.*populate.*engine-backed materialized views`),
			},
			{
				Config: `
resource "clickhousedbops_materialized_view" "test" {
  database = "posthog"
  name     = "events_daily_mv"
  to_table = "posthog.daily_event_counts"
  columns = [
    { name = "team_id", type = "UInt64", nullable = false }
  ]
  query = "SELECT 1"
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Combination.*columns.*engine-backed materialized views`),
			},
		},
	})
}

func TestMaterializedView_engineBackedAcceptance(t *testing.T) {
	databaseName := fmt.Sprintf("posthog_%s", acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum))

	checkNotExistsFunc := func(ctx context.Context, dbopsClient dbops.Client, clusterName *string, attrs map[string]string) (bool, error) {
		view, err := dbopsClient.GetMaterializedView(ctx, attrs["database"], attrs["name"], clusterName)
		return view != nil, err
	}

	checkAttributesFunc := func(ctx context.Context, dbopsClient dbops.Client, clusterName *string, attrs map[string]interface{}) error {
		view, err := dbopsClient.GetMaterializedView(ctx, attrs["database"].(string), attrs["name"].(string), clusterName)
		if err != nil {
			return err
		}
		if view == nil {
			return fmt.Errorf("materialized view %s.%s was not found", attrs["database"], attrs["name"])
		}
		if view.Engine != "MergeTree()" {
			return fmt.Errorf("expected engine MergeTree(), got %q", view.Engine)
		}
		if view.OrderBy != "team_id" {
			return fmt.Errorf("expected order_by team_id, got %q", view.OrderBy)
		}
		if !strings.Contains(view.CreateStatement, "ORDER BY team_id") {
			return fmt.Errorf("expected create statement to include ORDER BY, got %q", view.CreateStatement)
		}
		return nil
	}

	resource := fmt.Sprintf(`
locals {
  rollup_columns = [
    { name = "team_id", type = "UInt64", nullable = false },
    { name = "event_count", type = "UInt64", nullable = false },
  ]
}

resource "clickhousedbops_database" "posthog" {
  name = %q
}

resource "clickhousedbops_materialized_view" "events_rollup_mv" {
  database = clickhousedbops_database.posthog.name
  name     = "events_rollup_mv"
  engine   = "MergeTree()"
  order_by = "team_id"
  columns  = local.rollup_columns
  query    = <<-SQL
    SELECT team_id, count() AS event_count
    FROM system.numbers
    WHERE number < 10
    GROUP BY team_id
  SQL
}
`, databaseName)

	runner.RunTests(t, []runner.TestCase{
		{
			Name:                "Create engine-backed materialized view with first-class table clauses",
			ChEnv:               map[string]string{"CONFIGFILE": "config-single.xml"},
			Protocol:            "native",
			Resource:            resource,
			ResourceName:        "events_rollup_mv",
			ResourceAddress:     "clickhousedbops_materialized_view.events_rollup_mv",
			CheckNotExistsFunc:  checkNotExistsFunc,
			CheckAttributesFunc: checkAttributesFunc,
		},
	})
}

func TestMaterializedView_planAllowsSharedTableColumnsForToSignature(t *testing.T) {
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

resource "clickhousedbops_materialized_view" "test" {
  database   = "posthog"
  name       = "events_mv"
  to_table   = "posthog.events_rollup"
  to_columns = local.shared_columns
  query      = "SELECT team_id, created_at FROM posthog.events"
}
`,
				PlanOnly: true,
			},
		},
	})
}
