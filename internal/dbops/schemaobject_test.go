package dbops

import "testing"

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
			gotType, gotNullable := unwrapNullableType(tt.raw)
			if gotType != tt.wantType || gotNullable != tt.wantNullable {
				t.Fatalf("unwrapNullableType() got (%q, %t), want (%q, %t)", gotType, gotNullable, tt.wantType, tt.wantNullable)
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
