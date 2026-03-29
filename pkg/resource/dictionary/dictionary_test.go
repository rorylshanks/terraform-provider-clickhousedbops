package dictionary_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"

	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/dbops"
	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/testutils/runner"
)

func TestDictionary_acceptance(t *testing.T) {
	databaseName := fmt.Sprintf("posthog_%s", acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum))

	checkNotExistsFunc := func(ctx context.Context, dbopsClient dbops.Client, clusterName *string, attrs map[string]string) (bool, error) {
		dictionary, err := dbopsClient.GetDictionary(ctx, attrs["database"], attrs["name"], clusterName)
		return dictionary != nil, err
	}

	checkAttributesFunc := func(ctx context.Context, dbopsClient dbops.Client, clusterName *string, attrs map[string]interface{}) error {
		dictionary, err := dbopsClient.GetDictionary(ctx, attrs["database"].(string), attrs["name"].(string), clusterName)
		if err != nil {
			return err
		}
		if dictionary == nil {
			return fmt.Errorf("dictionary %s.%s was not found", attrs["database"], attrs["name"])
		}
		if attrs["qualified_name"].(string) != fmt.Sprintf("%s.%s", attrs["database"], attrs["name"]) {
			return fmt.Errorf("unexpected qualified_name value: %v", attrs["qualified_name"])
		}
		if !strings.Contains(dictionary.CreateStatement, "CREATE DICTIONARY") {
			return fmt.Errorf("expected create statement to describe a dictionary, got %q", dictionary.CreateStatement)
		}
		if !strings.Contains(dictionary.CreateStatement, "SOURCE(CLICKHOUSE(") {
			return fmt.Errorf("expected create statement to contain the source clause, got %q", dictionary.CreateStatement)
		}
		if !strings.Contains(dictionary.CreateStatement, "LAYOUT(FLAT())") {
			return fmt.Errorf("expected create statement to contain the layout clause, got %q", dictionary.CreateStatement)
		}
		return nil
	}

	resource := fmt.Sprintf(`
locals {
  dictionary_source_columns = [
    { name = "id", type = "UInt64", nullable = false },
    { name = "value", type = "String", nullable = false },
  ]
  dictionary_attributes = [
    { name = "id", type = "UInt64", nullable = false },
    { name = "value", type = "String", nullable = false, default_expression = "'unknown'" },
  ]
}

resource "clickhousedbops_database" "posthog" {
  name = %q
}

resource "clickhousedbops_table" "dictionary_source" {
  database = clickhousedbops_database.posthog.name
  name     = "dictionary_source"
  engine   = "MergeTree()"
  order_by = "id"
  columns  = local.dictionary_source_columns
}

resource "clickhousedbops_dictionary" "teams" {
  database    = clickhousedbops_database.posthog.name
  name        = "teams"
  attributes  = local.dictionary_attributes
  primary_key = ["id"]
  source      = format("CLICKHOUSE(HOST 'localhost' PORT tcpPort() USER 'default' PASSWORD 'test' DB '%%s' TABLE '%%s')", clickhousedbops_database.posthog.name, clickhousedbops_table.dictionary_source.name)
  layout      = "FLAT()"
  lifetime    = "0"
}
`, databaseName)

	runner.RunTests(t, []runner.TestCase{
		{
			Name:                "Create dictionary backed by a managed source table",
			ChEnv:               map[string]string{"CONFIGFILE": "config-single.xml"},
			Protocol:            "native",
			Resource:            resource,
			ResourceName:        "teams",
			ResourceAddress:     "clickhousedbops_dictionary.teams",
			CheckNotExistsFunc:  checkNotExistsFunc,
			CheckAttributesFunc: checkAttributesFunc,
		},
	})
}
