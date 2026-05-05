package dbops

import (
	"testing"

	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/querybuilder"
)

func TestParseCreateTableDefinition(t *testing.T) {
	definition, err := parseCreateTableDefinition("CREATE TABLE drift_test.events (`id` UInt64, `ts` DateTime DEFAULT now(), `extra` UInt64 ALIAS id) ENGINE = MergeTree PARTITION BY toYYYYMM(ts) ORDER BY (id, ts) SAMPLE BY id TTL ts + toIntervalDay(1) SETTINGS ttl_only_drop_parts = 1, index_granularity = 8192 AS SELECT * FROM drift_test.source")
	if err != nil {
		t.Fatalf("parseCreateTableDefinition() error = %v", err)
	}

	if definition.Engine != "MergeTree" {
		t.Fatalf("expected engine to be MergeTree, got %q", definition.Engine)
	}
	if definition.PartitionBy != "toYYYYMM(ts)" {
		t.Fatalf("unexpected partition_by: %q", definition.PartitionBy)
	}
	if definition.OrderBy != "(id, ts)" {
		t.Fatalf("unexpected order_by: %q", definition.OrderBy)
	}
	if definition.SampleBy != "id" {
		t.Fatalf("unexpected sample_by: %q", definition.SampleBy)
	}
	if definition.TTL != "ts + toIntervalDay(1)" {
		t.Fatalf("unexpected ttl: %q", definition.TTL)
	}
	if definition.Settings != "ttl_only_drop_parts = 1, index_granularity = 8192" {
		t.Fatalf("unexpected settings: %q", definition.Settings)
	}
	if definition.AsSelect != "SELECT * FROM drift_test.source" {
		t.Fatalf("unexpected as_select: %q", definition.AsSelect)
	}
}

func TestUnwrapNullableType(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		wantType     string
		wantNullable bool
	}{
		{
			name:         "outer nullable",
			raw:          "Nullable(Decimal(18, 2))",
			wantType:     "Decimal(18, 2)",
			wantNullable: true,
		},
		{
			name:         "nested nullable stays in type",
			raw:          "LowCardinality(Nullable(String))",
			wantType:     "LowCardinality(Nullable(String))",
			wantNullable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotNullable := querybuilder.UnwrapNullableType(tt.raw)
			if gotType != tt.wantType || gotNullable != tt.wantNullable {
				t.Fatalf("querybuilder.UnwrapNullableType() got (%q, %t), want (%q, %t)", gotType, gotNullable, tt.wantType, tt.wantNullable)
			}
		})
	}
}

func TestParseCreateViewDefinition(t *testing.T) {
	definition, err := parseCreateViewDefinition("CREATE VIEW `posthog`.`team_event_counts` (`team_id` UInt64, `event_count` Nullable(UInt64)) AS SELECT team_id, count() AS event_count FROM posthog.events GROUP BY team_id")
	if err != nil {
		t.Fatalf("parseCreateViewDefinition() error = %v", err)
	}

	if definition.Query != "SELECT team_id, count() AS event_count FROM posthog.events GROUP BY team_id" {
		t.Fatalf("unexpected query: %q", definition.Query)
	}
	if len(definition.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %#v", definition.Columns)
	}
	if definition.Columns[0] != (Column{Name: "team_id", Type: "UInt64", Nullable: false}) {
		t.Fatalf("unexpected first column: %#v", definition.Columns[0])
	}
	if definition.Columns[1] != (Column{Name: "event_count", Type: "UInt64", Nullable: true}) {
		t.Fatalf("unexpected second column: %#v", definition.Columns[1])
	}
}

func TestParseCreateViewDefinitionWithoutSignature(t *testing.T) {
	definition, err := parseCreateViewDefinition("CREATE VIEW `posthog`.`team_event_counts` AS SELECT team_id FROM posthog.events")
	if err != nil {
		t.Fatalf("parseCreateViewDefinition() error = %v", err)
	}

	if definition.Query != "SELECT team_id FROM posthog.events" {
		t.Fatalf("unexpected query: %q", definition.Query)
	}
	if len(definition.Columns) != 0 {
		t.Fatalf("expected no explicit columns, got %#v", definition.Columns)
	}
}

func TestTableColumnKeyStableForDuplicateClusterRows(t *testing.T) {
	left := Column{
		Name:     "id",
		Type:     "UInt64",
		Nullable: false,
	}
	right := Column{
		Name:     "id",
		Type:     "UInt64",
		Nullable: false,
	}

	if tableColumnKey(left) != tableColumnKey(right) {
		t.Fatalf("expected identical columns to produce the same dedupe key")
	}
}

func TestParseCreateTableDefinition_EmptyInput(t *testing.T) {
	definition, err := parseCreateTableDefinition("")
	if err != nil {
		t.Fatalf("parseCreateTableDefinition() error = %v", err)
	}
	if definition.Engine != "" {
		t.Fatalf("expected empty engine, got %q", definition.Engine)
	}
	if definition.PartitionBy != "" {
		t.Fatalf("expected empty partition_by, got %q", definition.PartitionBy)
	}
	if definition.OrderBy != "" {
		t.Fatalf("expected empty order_by, got %q", definition.OrderBy)
	}
	if definition.PrimaryKey != "" {
		t.Fatalf("expected empty primary_key, got %q", definition.PrimaryKey)
	}
	if definition.SampleBy != "" {
		t.Fatalf("expected empty sample_by, got %q", definition.SampleBy)
	}
	if definition.TTL != "" {
		t.Fatalf("expected empty ttl, got %q", definition.TTL)
	}
	if definition.Settings != "" {
		t.Fatalf("expected empty settings, got %q", definition.Settings)
	}
	if definition.AsSelect != "" {
		t.Fatalf("expected empty as_select, got %q", definition.AsSelect)
	}
}

func TestParseCreateTableDefinition_AllClauses(t *testing.T) {
	stmt := "CREATE TABLE `mydb`.`mytable` (`id` UInt64, `ts` DateTime, `value` Float64) ENGINE = MergeTree PARTITION BY toYYYYMM(ts) ORDER BY (id, ts) PRIMARY KEY id SAMPLE BY id TTL ts + toIntervalDay(30) SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1"
	definition, err := parseCreateTableDefinition(stmt)
	if err != nil {
		t.Fatalf("parseCreateTableDefinition() error = %v", err)
	}
	if definition.Engine != "MergeTree" {
		t.Fatalf("expected engine MergeTree, got %q", definition.Engine)
	}
	if definition.PartitionBy != "toYYYYMM(ts)" {
		t.Fatalf("unexpected partition_by: %q", definition.PartitionBy)
	}
	if definition.OrderBy != "(id, ts)" {
		t.Fatalf("unexpected order_by: %q", definition.OrderBy)
	}
	if definition.PrimaryKey != "id" {
		t.Fatalf("unexpected primary_key: %q", definition.PrimaryKey)
	}
	if definition.SampleBy != "id" {
		t.Fatalf("unexpected sample_by: %q", definition.SampleBy)
	}
	if definition.TTL != "ts + toIntervalDay(30)" {
		t.Fatalf("unexpected ttl: %q", definition.TTL)
	}
	if definition.Settings != "index_granularity = 8192, ttl_only_drop_parts = 1" {
		t.Fatalf("unexpected settings: %q", definition.Settings)
	}
}

func TestParseCreateTableDefinition_NestedEngineArgs(t *testing.T) {
	stmt := "CREATE TABLE `mydb`.`mytable` (`id` UInt64) ENGINE = ReplicatedMergeTree('/clickhouse/tables/{shard}', '{replica}') ORDER BY id"
	definition, err := parseCreateTableDefinition(stmt)
	if err != nil {
		t.Fatalf("parseCreateTableDefinition() error = %v", err)
	}
	if definition.Engine != "ReplicatedMergeTree('/clickhouse/tables/{shard}', '{replica}')" {
		t.Fatalf("unexpected engine: %q", definition.Engine)
	}
	if definition.OrderBy != "id" {
		t.Fatalf("unexpected order_by: %q", definition.OrderBy)
	}
}

func TestParseCreateTableDefinition_AsSelect(t *testing.T) {
	stmt := "CREATE TABLE `mydb`.`derived` (`id` UInt64) ENGINE = MergeTree ORDER BY id AS SELECT id FROM mydb.source WHERE id > 0"
	definition, err := parseCreateTableDefinition(stmt)
	if err != nil {
		t.Fatalf("parseCreateTableDefinition() error = %v", err)
	}
	if definition.Engine != "MergeTree" {
		t.Fatalf("unexpected engine: %q", definition.Engine)
	}
	if definition.OrderBy != "id" {
		t.Fatalf("unexpected order_by: %q", definition.OrderBy)
	}
	if definition.AsSelect != "SELECT id FROM mydb.source WHERE id > 0" {
		t.Fatalf("unexpected as_select: %q", definition.AsSelect)
	}
}

func TestParseCreateMaterializedViewDefinition_ToTable(t *testing.T) {
	stmt := "CREATE MATERIALIZED VIEW `mydb`.`mv` TO mydb.target AS SELECT id, count() AS cnt FROM mydb.source GROUP BY id"
	definition, err := parseCreateMaterializedViewDefinition(stmt)
	if err != nil {
		t.Fatalf("parseCreateMaterializedViewDefinition() error = %v", err)
	}
	if definition.ToTable != "mydb.target" {
		t.Fatalf("unexpected to_table: %q", definition.ToTable)
	}
	if definition.Query != "SELECT id, count() AS cnt FROM mydb.source GROUP BY id" {
		t.Fatalf("unexpected query: %q", definition.Query)
	}
	if definition.Engine != "" {
		t.Fatalf("expected empty engine for TO-table MV, got %q", definition.Engine)
	}
	if definition.Populate {
		t.Fatal("expected populate to be false for TO-table MV")
	}
	if len(definition.ToColumns) != 0 {
		t.Fatalf("expected no to_columns, got %#v", definition.ToColumns)
	}
}

func TestParseCreateMaterializedViewDefinition_EngineBacked(t *testing.T) {
	stmt := "CREATE MATERIALIZED VIEW `mydb`.`mv` (`id` UInt64, `cnt` UInt64) ENGINE = MergeTree() PARTITION BY toYYYYMM(ts) ORDER BY id PRIMARY KEY id SAMPLE BY id TTL ts + INTERVAL 1 DAY SETTINGS index_granularity = 8192 AS SELECT id, count() AS cnt FROM mydb.source GROUP BY id"
	definition, err := parseCreateMaterializedViewDefinition(stmt)
	if err != nil {
		t.Fatalf("parseCreateMaterializedViewDefinition() error = %v", err)
	}
	if definition.Engine != "MergeTree()" {
		t.Fatalf("unexpected engine: %q", definition.Engine)
	}
	if definition.PartitionBy != "toYYYYMM(ts)" || definition.OrderBy != "id" || definition.PrimaryKey != "id" || definition.SampleBy != "id" {
		t.Fatalf("unexpected engine-backed clauses: %#v", definition)
	}
	if definition.TTL != "ts + INTERVAL 1 DAY" || definition.Settings != "index_granularity = 8192" {
		t.Fatalf("unexpected ttl/settings: %#v", definition)
	}
	if definition.Query != "SELECT id, count() AS cnt FROM mydb.source GROUP BY id" {
		t.Fatalf("unexpected query: %q", definition.Query)
	}
	if definition.ToTable != "" {
		t.Fatalf("expected empty to_table for engine-backed MV, got %q", definition.ToTable)
	}
	if definition.Populate {
		t.Fatal("expected populate to be false")
	}
	if len(definition.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(definition.Columns))
	}
	if definition.Columns[0].Name != "id" || definition.Columns[0].Type != "UInt64" {
		t.Fatalf("unexpected first column: %#v", definition.Columns[0])
	}
	if definition.Columns[1].Name != "cnt" || definition.Columns[1].Type != "UInt64" {
		t.Fatalf("unexpected second column: %#v", definition.Columns[1])
	}
}

func TestParseCreateMaterializedViewDefinition_Empty(t *testing.T) {
	definition, err := parseCreateMaterializedViewDefinition("")
	if err != nil {
		t.Fatalf("parseCreateMaterializedViewDefinition() error = %v", err)
	}
	if definition.Engine != "" {
		t.Fatalf("expected empty engine, got %q", definition.Engine)
	}
	if definition.ToTable != "" {
		t.Fatalf("expected empty to_table, got %q", definition.ToTable)
	}
	if definition.Query != "" {
		t.Fatalf("expected empty query, got %q", definition.Query)
	}
	if len(definition.Columns) != 0 {
		t.Fatalf("expected no columns, got %d", len(definition.Columns))
	}
}

func TestParseCreateMaterializedViewDefinition_WithColumns(t *testing.T) {
	stmt := "CREATE MATERIALIZED VIEW `mydb`.`mv` (`user_id` UInt64, `name` Nullable(String)) TO mydb.target AS SELECT user_id, name FROM mydb.users"
	definition, err := parseCreateMaterializedViewDefinition(stmt)
	if err != nil {
		t.Fatalf("parseCreateMaterializedViewDefinition() error = %v", err)
	}
	if definition.ToTable != "mydb.target" {
		t.Fatalf("unexpected to_table: %q", definition.ToTable)
	}
	if definition.Query != "SELECT user_id, name FROM mydb.users" {
		t.Fatalf("unexpected query: %q", definition.Query)
	}
}

func TestParseCreateMaterializedViewDefinition_ToColumnsAndPopulate(t *testing.T) {
	stmt := "CREATE MATERIALIZED VIEW `mydb`.`mv` TO mydb.target (`user_id` UInt64, `name` Nullable(String)) AS SELECT user_id, name FROM mydb.users"
	definition, err := parseCreateMaterializedViewDefinition(stmt)
	if err != nil {
		t.Fatalf("parseCreateMaterializedViewDefinition() error = %v", err)
	}
	if definition.ToTable != "mydb.target" {
		t.Fatalf("unexpected to_table: %q", definition.ToTable)
	}
	if len(definition.ToColumns) != 2 {
		t.Fatalf("expected 2 to_columns, got %#v", definition.ToColumns)
	}
	if definition.ToColumns[0] != (Column{Name: "user_id", Type: "UInt64", Nullable: false}) {
		t.Fatalf("unexpected first to_column: %#v", definition.ToColumns[0])
	}
	if definition.ToColumns[1] != (Column{Name: "name", Type: "String", Nullable: true}) {
		t.Fatalf("unexpected second to_column: %#v", definition.ToColumns[1])
	}

	stmt = "CREATE MATERIALIZED VIEW `mydb`.`mv` (`id` UInt64) ENGINE = MergeTree() ORDER BY id POPULATE AS SELECT id FROM mydb.source"
	definition, err = parseCreateMaterializedViewDefinition(stmt)
	if err != nil {
		t.Fatalf("parseCreateMaterializedViewDefinition() error = %v", err)
	}
	if !definition.Populate {
		t.Fatal("expected populate to be parsed")
	}
	if definition.Engine != "MergeTree()" {
		t.Fatalf("unexpected engine: %q", definition.Engine)
	}
	if definition.OrderBy != "id" {
		t.Fatalf("unexpected order_by: %q", definition.OrderBy)
	}
}
