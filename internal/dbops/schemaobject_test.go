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
