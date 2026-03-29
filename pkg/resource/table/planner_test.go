package table

import (
	"strings"
	"testing"

	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/dbops"
)

func TestValidateTableForEngineKafkaDefaults(t *testing.T) {
	defaultExpr := "1"
	diags := validateTableForEngine(
		dbops.Table{
			Engine: "Kafka('localhost:9092', 'events', 'g1', 'JSONEachRow')",
			Columns: []dbops.Column{
				{Name: "id", Type: "UInt64", DefaultExpression: &defaultExpr},
			},
		},
		dbops.TableEngineCapabilities{
			Name:             "Kafka",
			Known:            true,
			SupportsSettings: true,
		},
	)

	if !diags.HasError() {
		t.Fatal("expected Kafka default-expression validation to fail")
	}
}

func TestValidateTableForEngineMergeTreeRequiresOrderBy(t *testing.T) {
	diags := validateTableForEngine(
		dbops.Table{
			Engine: "MergeTree()",
			Columns: []dbops.Column{
				{Name: "id", Type: "UInt64"},
			},
		},
		dbops.TableEngineCapabilities{
			Name:              "MergeTree",
			Known:             true,
			SupportsSettings:  true,
			SupportsSortOrder: true,
			SupportsTTL:       true,
		},
	)

	if !diags.HasError() {
		t.Fatal("expected MergeTree validation to require ORDER BY")
	}
}

func TestPlanTableUpdateMergeTreeOrderByAppendNewColumn(t *testing.T) {
	current := dbops.Table{
		Engine:  "MergeTree()",
		OrderBy: "id",
		Columns: []dbops.Column{
			{Name: "id", Type: "UInt64"},
			{Name: "ts", Type: "DateTime"},
		},
	}
	desired := dbops.Table{
		Engine:  "MergeTree()",
		OrderBy: "(id, extra)",
		Columns: []dbops.Column{
			{Name: "id", Type: "UInt64"},
			{Name: "ts", Type: "DateTime"},
			{Name: "extra", Type: "UInt64"},
		},
	}

	plan, err := planTableUpdate(current, desired, dbops.TableEngineCapabilities{
		Name:              "MergeTree",
		Known:             true,
		SupportsSettings:  true,
		SupportsSortOrder: true,
		SupportsTTL:       true,
	}, nil)
	if err != nil {
		t.Fatalf("planTableUpdate() error = %v", err)
	}
	if len(plan.ReplaceAttrs) != 0 {
		t.Fatalf("expected in-place update, got replacement attrs %v", plan.ReplaceAttrs)
	}

	foundOrderBy := false
	for _, group := range plan.ActionGroups {
		for _, action := range group {
			if strings.Contains(action, "MODIFY ORDER BY") {
				foundOrderBy = true
			}
		}
	}
	if !foundOrderBy {
		t.Fatal("expected update plan to include MODIFY ORDER BY")
	}
}

func TestPlanTableUpdateMergeTreeReadonlySettingRequiresReplace(t *testing.T) {
	plan, err := planTableUpdate(
		dbops.Table{Engine: "MergeTree()", Settings: ""},
		dbops.Table{Engine: "MergeTree()", Settings: "index_granularity = 4096"},
		dbops.TableEngineCapabilities{Name: "MergeTree", Known: true, SupportsSettings: true, SupportsSortOrder: true, SupportsTTL: true},
		map[string]dbops.TableSettingCapability{
			"index_granularity": {Name: "index_granularity", Known: true, Readonly: true},
		},
	)
	if err != nil {
		t.Fatalf("planTableUpdate() error = %v", err)
	}
	if _, ok := plan.ReplaceAttrs["settings"]; !ok {
		t.Fatalf("expected readonly setting change to require replacement, got %v", plan.ReplaceAttrs)
	}
}

func TestPlanTableUpdateMergeTreeMutableSettingUsesAlter(t *testing.T) {
	plan, err := planTableUpdate(
		dbops.Table{Engine: "MergeTree()", Settings: ""},
		dbops.Table{Engine: "MergeTree()", Settings: "ttl_only_drop_parts = 1"},
		dbops.TableEngineCapabilities{Name: "MergeTree", Known: true, SupportsSettings: true, SupportsSortOrder: true, SupportsTTL: true},
		map[string]dbops.TableSettingCapability{
			"ttl_only_drop_parts": {Name: "ttl_only_drop_parts", Known: true, Readonly: false},
		},
	)
	if err != nil {
		t.Fatalf("planTableUpdate() error = %v", err)
	}
	if len(plan.ReplaceAttrs) != 0 {
		t.Fatalf("expected in-place settings update, got replacement attrs %v", plan.ReplaceAttrs)
	}

	foundSetting := false
	for _, group := range plan.ActionGroups {
		for _, action := range group {
			if strings.Contains(action, "MODIFY SETTING") {
				foundSetting = true
			}
		}
	}
	if !foundSetting {
		t.Fatal("expected update plan to include MODIFY SETTING")
	}
}

func TestPlanSettingsUpdateReturnsParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		current string
		desired string
		want    string
	}{
		{
			name:    "current settings",
			current: "ttl_only_drop_parts = 'unterminated",
			desired: "ttl_only_drop_parts = 1",
			want:    "unable to parse current table settings",
		},
		{
			name:    "desired settings",
			current: "ttl_only_drop_parts = 0",
			desired: "ttl_only_drop_parts = 'unterminated",
			want:    "unable to parse desired table settings",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := planSettingsUpdate(tt.current, tt.desired, engineUpdateStrategy{allowSettingsAlter: true}, nil)
			if err == nil {
				t.Fatal("expected parse error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error to contain %q, got %q", tt.want, err.Error())
			}
		})
	}
}

func TestPlanTableUpdateKafkaColumnChangeRequiresReplace(t *testing.T) {
	plan, err := planTableUpdate(
		dbops.Table{Engine: "Kafka(...)", Columns: []dbops.Column{{Name: "id", Type: "UInt64"}}},
		dbops.Table{Engine: "Kafka(...)", Columns: []dbops.Column{{Name: "id", Type: "UInt64"}, {Name: "extra", Type: "String"}}},
		dbops.TableEngineCapabilities{Name: "Kafka", Known: true, SupportsSettings: true},
		nil,
	)
	if err != nil {
		t.Fatalf("planTableUpdate() error = %v", err)
	}
	if _, ok := plan.ReplaceAttrs["columns"]; !ok {
		t.Fatalf("expected Kafka column change to require replacement, got %v", plan.ReplaceAttrs)
	}
}

func TestPlanTableUpdateDistributedColumnChangeUsesAlter(t *testing.T) {
	plan, err := planTableUpdate(
		dbops.Table{Engine: "Distributed(...)", Columns: []dbops.Column{{Name: "id", Type: "UInt64"}, {Name: "ts", Type: "DateTime"}}},
		dbops.Table{Engine: "Distributed(...)", Columns: []dbops.Column{{Name: "ts", Type: "DateTime"}, {Name: "id", Type: "UInt64"}}},
		dbops.TableEngineCapabilities{Name: "Distributed", Known: true, SupportsSettings: true},
		nil,
	)
	if err != nil {
		t.Fatalf("planTableUpdate() error = %v", err)
	}
	if len(plan.ReplaceAttrs) != 0 {
		t.Fatalf("expected in-place distributed column reorder, got replacement attrs %v", plan.ReplaceAttrs)
	}
	if len(plan.ActionGroups) == 0 {
		t.Fatal("expected distributed column reorder to produce ALTER actions")
	}
}

func TestPlanTableUpdateTreatsEquivalentOrderByAsUnchanged(t *testing.T) {
	plan, err := planTableUpdate(
		dbops.Table{Engine: "MergeTree()", OrderBy: "id, ts"},
		dbops.Table{Engine: "MergeTree()", OrderBy: "(id, ts)"},
		dbops.TableEngineCapabilities{Name: "MergeTree", Known: true, SupportsSettings: true, SupportsSortOrder: true, SupportsTTL: true},
		nil,
	)
	if err != nil {
		t.Fatalf("planTableUpdate() error = %v", err)
	}
	if len(plan.ReplaceAttrs) != 0 || len(plan.ActionGroups) != 0 {
		t.Fatalf("expected equivalent ORDER BY expressions to be unchanged, got replace=%v actions=%v", plan.ReplaceAttrs, plan.ActionGroups)
	}
}

func TestPlanTableUpdateTreatsEquivalentTTLAsUnchanged(t *testing.T) {
	plan, err := planTableUpdate(
		dbops.Table{Engine: "MergeTree()", OrderBy: "tuple()", TTL: "ts + toIntervalDay(1)"},
		dbops.Table{Engine: "MergeTree()", OrderBy: "tuple()", TTL: "ts + INTERVAL 1 DAY"},
		dbops.TableEngineCapabilities{Name: "MergeTree", Known: true, SupportsSettings: true, SupportsSortOrder: true, SupportsTTL: true},
		nil,
	)
	if err != nil {
		t.Fatalf("planTableUpdate() error = %v", err)
	}
	if len(plan.ReplaceAttrs) != 0 || len(plan.ActionGroups) != 0 {
		t.Fatalf("expected equivalent TTL expressions to be unchanged, got replace=%v actions=%v", plan.ReplaceAttrs, plan.ActionGroups)
	}
}

func TestSplitTopLevelHandlesBackslashEscapedSingleQuote(t *testing.T) {
	parts, err := splitTopLevel("path = 'it\\'s,ok', retries = 3", ',')
	if err != nil {
		t.Fatalf("splitTopLevel() error = %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d: %#v", len(parts), parts)
	}
}

func TestSplitTopLevelHandlesDoubledSingleQuote(t *testing.T) {
	parts, err := splitTopLevel("comment = 'team''s,blue', retries = 3", ',')
	if err != nil {
		t.Fatalf("splitTopLevel() error = %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d: %#v", len(parts), parts)
	}
}

func TestUnwrapOuterParensIgnoresQuotedParen(t *testing.T) {
	got := unwrapOuterParens("('a)b')")
	want := "'a)b'"
	if got != want {
		t.Fatalf("unwrapOuterParens() got = %q, want %q", got, want)
	}
}
