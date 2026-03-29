package querybuilder

import "testing"

func Test_createDictionary(t *testing.T) {
	clusterName := "cluster1"
	defaultExpr := "'unknown'"
	comment := "team lookup"

	got, err := NewCreateDictionary("posthog", "teams_dict").
		WithCluster(&clusterName).
		WithAttributes([]DictionaryAttributeDefinition{
			{Name: "id", Type: "UInt64"},
			{Name: "name", Type: "String", DefaultExpression: &defaultExpr, Injective: true},
		}).
		WithPrimaryKey([]string{"id"}).
		WithSource("CLICKHOUSE(HOST 'localhost' PORT tcpPort() USER 'default' PASSWORD 'test' DB 'posthog' TABLE 'teams_source')").
		WithLayout("FLAT()").
		WithLifetime("0").
		WithSettings("allow_read_expired_keys = 1").
		WithComment(comment).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	want := "CREATE DICTIONARY `posthog`.`teams_dict` ON CLUSTER 'cluster1' (`id` UInt64, `name` String DEFAULT 'unknown' INJECTIVE) PRIMARY KEY `id` SOURCE(CLICKHOUSE(HOST 'localhost' PORT tcpPort() USER 'default' PASSWORD 'test' DB 'posthog' TABLE 'teams_source')) LAYOUT(FLAT()) LIFETIME(0) SETTINGS allow_read_expired_keys = 1 COMMENT 'team lookup';"
	if got != want {
		t.Fatalf("Build() got = %v, want %v", got, want)
	}
}

func Test_createDictionaryRejectsConflictingAttributeClauses(t *testing.T) {
	defaultExpr := "'x'"
	expr := "toString(id)"

	_, err := NewCreateDictionary("posthog", "teams_dict").
		WithAttributes([]DictionaryAttributeDefinition{
			{
				Name:              "name",
				Type:              "String",
				DefaultExpression: &defaultExpr,
				Expression:        &expr,
			},
		}).
		WithPrimaryKey([]string{"name"}).
		WithSource("NULL()").
		WithLayout("FLAT()").
		WithLifetime("0").
		Build()
	if err == nil {
		t.Fatal("expected Build() to fail when multiple attribute expressions are set")
	}
}

func Test_showCreateDictionary(t *testing.T) {
	got, err := NewShowCreateDictionary("posthog", "teams_dict").Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	want := "SHOW CREATE DICTIONARY `posthog`.`teams_dict`;"
	if got != want {
		t.Fatalf("Build() got = %v, want %v", got, want)
	}
}
