package table_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/clickhouseclient"
	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/dbops"
	testcompose "github.com/ClickHouse/terraform-provider-clickhousedbops/internal/testutils/compose"
	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/testutils/dbopsclient"
	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/testutils/factories"
	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/testutils/providerconfig"
	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/testutils/runner"
)

func TestTable_acceptance(t *testing.T) {
	clusterName := "cluster1"
	databaseName := fmt.Sprintf("posthog_%s", acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum))

	checkNotExistsFunc := func(ctx context.Context, dbopsClient dbops.Client, clusterName *string, attrs map[string]string) (bool, error) {
		table, err := dbopsClient.GetTable(ctx, attrs["database"], attrs["name"], clusterName)
		return table != nil, err
	}

	checkAttributesFunc := func(ctx context.Context, dbopsClient dbops.Client, clusterName *string, attrs map[string]interface{}) error {
		table, err := dbopsClient.GetTable(ctx, attrs["database"].(string), attrs["name"].(string), clusterName)
		if err != nil {
			return err
		}
		if table == nil {
			return fmt.Errorf("table %s.%s was not found", attrs["database"], attrs["name"])
		}
		if attrs["qualified_name"].(string) != fmt.Sprintf("%s.%s", attrs["database"], attrs["name"]) {
			return fmt.Errorf("unexpected qualified_name value: %v", attrs["qualified_name"])
		}
		if attrs["create_statement"] == nil || attrs["create_statement"].(string) == "" {
			return fmt.Errorf("create_statement should be populated")
		}
		if !strings.Contains(table.CreateStatement, "Distributed(") {
			return fmt.Errorf("expected create statement to contain the Distributed engine, got %q", table.CreateStatement)
		}
		if !strings.Contains(table.CreateStatement, "sharded_events") {
			return fmt.Errorf("expected create statement to reference the local table, got %q", table.CreateStatement)
		}
		return nil
	}

	resource := fmt.Sprintf(`
locals {
  event_columns = [
    { name = "team_id", type = "UInt64", nullable = false },
    { name = "event", type = "String", nullable = false, comment = "event name" },
    { name = "created_at", type = "DateTime", nullable = false, default_expression = "now()" },
  ]
}

resource "clickhousedbops_database" "posthog" {
  cluster_name = %q
  name         = %q
}

resource "clickhousedbops_table" "sharded_events" {
  cluster_name = %q
  database     = clickhousedbops_database.posthog.name
  name         = "sharded_events"
  engine       = "MergeTree()"
  partition_by = "toYYYYMM(created_at)"
  order_by     = "(team_id, created_at)"
  columns      = local.event_columns
}

resource "clickhousedbops_table" "events" {
  cluster_name = %q
  database     = clickhousedbops_database.posthog.name
  name         = "events"
  engine       = "Distributed('cluster1', '${clickhousedbops_database.posthog.name}', '${clickhousedbops_table.sharded_events.name}', rand())"
  columns      = local.event_columns
}
`, clusterName, databaseName, clusterName, clusterName)

	runner.RunTests(t, []runner.TestCase{
		{
			Name:                "Create table pair from shared local columns",
			ChEnv:               map[string]string{"CONFIGFILE": "config-localfile.xml"},
			Protocol:            "native",
			ClusterName:         &clusterName,
			Resource:            resource,
			ResourceName:        "events",
			ResourceAddress:     "clickhousedbops_table.events",
			CheckNotExistsFunc:  checkNotExistsFunc,
			CheckAttributesFunc: checkAttributesFunc,
		},
	})
}

func TestTable_mergeTreeInPlaceAndReplacement_acceptance(t *testing.T) {
	withTableAcceptanceHarness(t, "native", map[string]string{"CONFIGFILE": "config-localfile.xml"}, func(ctx context.Context, dbopsClient dbops.Client, queryClient clickhouseclient.ClickhouseClient, providerCfg string) {
		clusterName := "cluster1"
		databaseName := fmt.Sprintf("posthog_%s", acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum))
		resourceAddress := "clickhousedbops_table.events_local"

		configStep1 := fmt.Sprintf(`
resource "clickhousedbops_database" "posthog" {
  cluster_name = %q
  name         = %q
}

resource "clickhousedbops_table" "events_local" {
  cluster_name = %q
  database     = clickhousedbops_database.posthog.name
  name         = "events_local"
  engine       = "MergeTree()"
  order_by     = "id"
  columns = [
    { name = "id", type = "UInt64", nullable = false },
    { name = "ts", type = "DateTime", nullable = false },
  ]
}
`, clusterName, databaseName, clusterName)

		configStep2 := fmt.Sprintf(`
resource "clickhousedbops_database" "posthog" {
  cluster_name = %q
  name         = %q
}

resource "clickhousedbops_table" "events_local" {
  cluster_name = %q
  database     = clickhousedbops_database.posthog.name
  name         = "events_local"
  engine       = "MergeTree()"
  order_by     = "(id, extra)"
  columns = [
    { name = "id", type = "UInt64", nullable = false },
    { name = "ts", type = "DateTime", nullable = false },
    { name = "extra", type = "UInt64", nullable = false },
  ]
}
`, clusterName, databaseName, clusterName)

		configStep3 := fmt.Sprintf(`
resource "clickhousedbops_database" "posthog" {
  cluster_name = %q
  name         = %q
}

resource "clickhousedbops_table" "events_local" {
  cluster_name = %q
  database     = clickhousedbops_database.posthog.name
  name         = "events_local"
  engine       = "MergeTree()"
  order_by     = "(id, extra)"
  sample_by    = "id"
  ttl          = "ts + INTERVAL 1 DAY"
  settings     = "ttl_only_drop_parts = 1"
  columns = [
    { name = "id", type = "UInt64", nullable = false },
    { name = "ts", type = "DateTime", nullable = false },
    { name = "extra", type = "UInt64", nullable = false },
  ]
}
`, clusterName, databaseName, clusterName)

		configStep4 := fmt.Sprintf(`
resource "clickhousedbops_database" "posthog" {
  cluster_name = %q
  name         = %q
}

resource "clickhousedbops_table" "events_local" {
  cluster_name = %q
  database     = clickhousedbops_database.posthog.name
  name         = "events_local"
  engine       = "MergeTree()"
  order_by     = "(id, extra)"
  sample_by    = "id"
  ttl          = "ts + INTERVAL 1 DAY"
  settings     = "index_granularity = 4096"
  columns = [
    { name = "id", type = "UInt64", nullable = false },
    { name = "ts", type = "DateTime", nullable = false },
    { name = "extra", type = "UInt64", nullable = false },
  ]
}
`, clusterName, databaseName, clusterName)

		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: factories.ProviderFactories(),
			CheckDestroy: func(s *terraform.State) error {
				for address, r := range s.RootModule().Resources {
					if address != resourceAddress {
						continue
					}
					table, err := dbopsClient.GetTable(ctx, r.Primary.Attributes["database"], r.Primary.Attributes["name"], &clusterName)
					if err != nil {
						return err
					}
					if table != nil {
						return fmt.Errorf("expected table to be destroyed")
					}
				}
				return nil
			},
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf("%s\n%s", providerCfg, configStep1),
					Check: resource.ComposeTestCheckFunc(
						checkTableRowCount(ctx, queryClient, databaseName, "events_local", 0),
					),
				},
				{
					PreConfig: func() {
						insertMergeTreeRow(t, ctx, queryClient, databaseName, "events_local")
					},
					Config: fmt.Sprintf("%s\n%s", providerCfg, configStep2),
					ConfigPlanChecks: resource.ConfigPlanChecks{
						PreApply: []plancheck.PlanCheck{
							plancheck.ExpectResourceAction(resourceAddress, plancheck.ResourceActionUpdate),
						},
					},
					Check: resource.ComposeTestCheckFunc(
						checkTableRowCount(ctx, queryClient, databaseName, "events_local", 1),
						checkCreateStatementContains(ctx, dbopsClient, &clusterName, databaseName, "events_local", "ORDER BY (id, extra)"),
					),
				},
				{
					Config: fmt.Sprintf("%s\n%s", providerCfg, configStep3),
					ConfigPlanChecks: resource.ConfigPlanChecks{
						PreApply: []plancheck.PlanCheck{
							plancheck.ExpectResourceAction(resourceAddress, plancheck.ResourceActionUpdate),
						},
					},
					Check: resource.ComposeTestCheckFunc(
						checkTableRowCount(ctx, queryClient, databaseName, "events_local", 1),
						checkCreateStatementContains(ctx, dbopsClient, &clusterName, databaseName, "events_local", "SAMPLE BY id"),
						checkCreateStatementContains(ctx, dbopsClient, &clusterName, databaseName, "events_local", "ttl_only_drop_parts = 1"),
					),
				},
				{
					Config: fmt.Sprintf("%s\n%s", providerCfg, configStep4),
					ConfigPlanChecks: resource.ConfigPlanChecks{
						PreApply: []plancheck.PlanCheck{
							plancheck.ExpectResourceAction(resourceAddress, plancheck.ResourceActionReplace),
						},
					},
					Check: resource.ComposeTestCheckFunc(
						checkTableRowCount(ctx, queryClient, databaseName, "events_local", 0),
						checkCreateStatementContains(ctx, dbopsClient, &clusterName, databaseName, "events_local", "index_granularity = 4096"),
					),
				},
			},
		})
	})
}

func TestTable_engineSpecificUpdatePlanning_acceptance(t *testing.T) {
	withTableAcceptanceHarness(t, "native", map[string]string{"CONFIGFILE": "config-localfile.xml"}, func(ctx context.Context, dbopsClient dbops.Client, _ clickhouseclient.ClickhouseClient, providerCfg string) {
		clusterName := "cluster1"
		databaseName := fmt.Sprintf("posthog_%s", acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum))

		distributedAddress := "clickhousedbops_table.events"
		distributedStep1 := fmt.Sprintf(`
resource "clickhousedbops_database" "posthog" {
  cluster_name = %q
  name         = %q
}

resource "clickhousedbops_table" "events_local" {
  cluster_name = %q
  database     = clickhousedbops_database.posthog.name
  name         = "events_local"
  engine       = "MergeTree()"
  order_by     = "id"
  columns = [
    { name = "id", type = "UInt64", nullable = false },
    { name = "ts", type = "DateTime", nullable = false },
  ]
}

resource "clickhousedbops_table" "events" {
  cluster_name = %q
  database     = clickhousedbops_database.posthog.name
  name         = "events"
  engine       = "Distributed('cluster1', '${clickhousedbops_database.posthog.name}', '${clickhousedbops_table.events_local.name}', rand())"
  columns = [
    { name = "id", type = "UInt64", nullable = false },
    { name = "ts", type = "DateTime", nullable = false },
  ]
}
`, clusterName, databaseName, clusterName, clusterName)

		distributedStep2 := fmt.Sprintf(`
resource "clickhousedbops_database" "posthog" {
  cluster_name = %q
  name         = %q
}

resource "clickhousedbops_table" "events_local" {
  cluster_name = %q
  database     = clickhousedbops_database.posthog.name
  name         = "events_local"
  engine       = "MergeTree()"
  order_by     = "id"
  columns = [
    { name = "id", type = "UInt64", nullable = false },
    { name = "ts", type = "DateTime", nullable = false },
  ]
}

resource "clickhousedbops_table" "events" {
  cluster_name = %q
  database     = clickhousedbops_database.posthog.name
  name         = "events"
  engine       = "Distributed('cluster1', '${clickhousedbops_database.posthog.name}', '${clickhousedbops_table.events_local.name}', rand())"
  columns = [
    { name = "ts", type = "DateTime", nullable = false },
    { name = "id", type = "UInt64", nullable = false },
  ]
}
`, clusterName, databaseName, clusterName, clusterName)

		distributedStep3 := fmt.Sprintf(`
resource "clickhousedbops_database" "posthog" {
  cluster_name = %q
  name         = %q
}

resource "clickhousedbops_table" "events_local" {
  cluster_name = %q
  database     = clickhousedbops_database.posthog.name
  name         = "events_local"
  engine       = "MergeTree()"
  order_by     = "id"
  columns = [
    { name = "id", type = "UInt64", nullable = false },
    { name = "ts", type = "DateTime", nullable = false },
  ]
}

resource "clickhousedbops_table" "events" {
  cluster_name = %q
  database     = clickhousedbops_database.posthog.name
  name         = "events"
  engine       = "Distributed('cluster1', '${clickhousedbops_database.posthog.name}', '${clickhousedbops_table.events_local.name}', rand())"
  settings     = "background_insert_batch = 7"
  columns = [
    { name = "ts", type = "DateTime", nullable = false },
    { name = "id", type = "UInt64", nullable = false },
  ]
}
`, clusterName, databaseName, clusterName, clusterName)

		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: factories.ProviderFactories(),
			CheckDestroy: func(s *terraform.State) error {
				for address, r := range s.RootModule().Resources {
					if address != distributedAddress {
						continue
					}
					table, err := dbopsClient.GetTable(ctx, r.Primary.Attributes["database"], r.Primary.Attributes["name"], &clusterName)
					if err != nil {
						return err
					}
					if table != nil {
						return fmt.Errorf("expected distributed table to be destroyed")
					}
				}
				return nil
			},
			Steps: []resource.TestStep{
				{Config: fmt.Sprintf("%s\n%s", providerCfg, distributedStep1)},
				{
					Config: fmt.Sprintf("%s\n%s", providerCfg, distributedStep2),
					ConfigPlanChecks: resource.ConfigPlanChecks{
						PreApply: []plancheck.PlanCheck{
							plancheck.ExpectResourceAction(distributedAddress, plancheck.ResourceActionUpdate),
						},
					},
				},
				{
					Config: fmt.Sprintf("%s\n%s", providerCfg, distributedStep3),
					ConfigPlanChecks: resource.ConfigPlanChecks{
						PreApply: []plancheck.PlanCheck{
							plancheck.ExpectResourceAction(distributedAddress, plancheck.ResourceActionReplace),
						},
					},
				},
			},
		})

		kafkaDatabaseName := fmt.Sprintf("posthog_%s", acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum))
		kafkaAddress := "clickhousedbops_table.kafka_events"
		kafkaStep1 := fmt.Sprintf(`
resource "clickhousedbops_database" "posthog" {
  cluster_name = %q
  name         = %q
}

resource "clickhousedbops_table" "kafka_events" {
  cluster_name = %q
  database     = clickhousedbops_database.posthog.name
  name         = "kafka_events"
  engine       = "Kafka('localhost:9092', 'events', 'events_consumer', 'JSONEachRow')"
  settings     = "kafka_num_consumers = 1"
  columns = [
    { name = "event", type = "String", nullable = false, comment = "event name" },
  ]
}
`, clusterName, kafkaDatabaseName, clusterName)

		kafkaStep2 := fmt.Sprintf(`
resource "clickhousedbops_database" "posthog" {
  cluster_name = %q
  name         = %q
}

resource "clickhousedbops_table" "kafka_events" {
  cluster_name = %q
  database     = clickhousedbops_database.posthog.name
  name         = "kafka_events"
  engine       = "Kafka('localhost:9092', 'events', 'events_consumer', 'JSONEachRow')"
  settings     = "kafka_num_consumers = 1"
  columns = [
    { name = "event", type = "String", nullable = false, comment = "event name" },
    { name = "extra", type = "String", nullable = false },
  ]
}
`, clusterName, kafkaDatabaseName, clusterName)

		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: factories.ProviderFactories(),
			CheckDestroy: func(s *terraform.State) error {
				for address, r := range s.RootModule().Resources {
					if address != kafkaAddress {
						continue
					}
					table, err := dbopsClient.GetTable(ctx, r.Primary.Attributes["database"], r.Primary.Attributes["name"], &clusterName)
					if err != nil {
						return err
					}
					if table != nil {
						return fmt.Errorf("expected Kafka table to be destroyed")
					}
				}
				return nil
			},
			Steps: []resource.TestStep{
				{Config: fmt.Sprintf("%s\n%s", providerCfg, kafkaStep1)},
				{
					Config: fmt.Sprintf("%s\n%s", providerCfg, kafkaStep2),
					ConfigPlanChecks: resource.ConfigPlanChecks{
						PreApply: []plancheck.PlanCheck{
							plancheck.ExpectResourceAction(kafkaAddress, plancheck.ResourceActionReplace),
						},
					},
				},
			},
		})
	})
}

func withTableAcceptanceHarness(t *testing.T, protocol string, env map[string]string, fn func(context.Context, dbops.Client, clickhouseclient.ClickhouseClient, string)) {
	t.Helper()
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Skipping test because TF_ACC is not set to 1")
	}

	ctx := context.Background()
	dcm := testcompose.NewDockerComposeManager("../../../tests")
	if err := dcm.Up(env); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := dcm.Down(); err != nil {
			t.Fatal(err)
		}
	}()

	dbopsClient, connSettings, err := dbopsclient.NewDbopsClient(protocol)
	if err != nil {
		t.Fatal(err)
	}

	providerCfg, err := providerconfig.ProviderConfig(protocol, connSettings.Host, connSettings.Port, connSettings.Username, connSettings.Password)
	if err != nil {
		t.Fatal(err)
	}

	queryClient, err := clickhouseclient.NewNativeClient(clickhouseclient.NativeClientConfig{
		Host: connSettings.Host,
		Port: connSettings.Port,
		UserPasswordAuth: &clickhouseclient.UserPasswordAuth{
			Username: connSettings.Username,
			Password: connSettings.Password,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	fn(ctx, dbopsClient, queryClient, providerCfg)
}

func insertMergeTreeRow(t *testing.T, ctx context.Context, client clickhouseclient.ClickhouseClient, database string, table string) {
	t.Helper()
	query := fmt.Sprintf("INSERT INTO `%s`.`%s` (`id`, `ts`) VALUES (1, now())", database, table)
	if err := client.Exec(ctx, query); err != nil {
		t.Fatalf("failed to insert test row: %v", err)
	}
}

func checkTableRowCount(ctx context.Context, client clickhouseclient.ClickhouseClient, database string, table string, expected uint64) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		var actual uint64
		query := fmt.Sprintf("SELECT count() AS count FROM `%s`.`%s`", database, table)
		if err := client.Select(ctx, query, func(row clickhouseclient.Row) error {
			var rowErr error
			actual, rowErr = row.GetUInt64("count")
			return rowErr
		}); err != nil {
			return err
		}
		if actual != expected {
			return fmt.Errorf("unexpected row count for %s.%s: got %d want %d", database, table, actual, expected)
		}
		return nil
	}
}

func checkCreateStatementContains(ctx context.Context, client dbops.Client, clusterName *string, database string, table string, needle string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		schemaObject, err := client.GetTable(ctx, database, table, clusterName)
		if err != nil {
			return err
		}
		if schemaObject == nil {
			return fmt.Errorf("table %s.%s was not found", database, table)
		}
		if !strings.Contains(schemaObject.CreateStatement, needle) {
			return fmt.Errorf("expected create statement to contain %q, got %q", needle, schemaObject.CreateStatement)
		}
		return nil
	}
}
