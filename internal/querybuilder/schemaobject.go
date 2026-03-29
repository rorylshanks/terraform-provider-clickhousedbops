package querybuilder

import (
	"fmt"
	"strings"

	"github.com/pingcap/errors"
)

type ColumnDefinition struct {
	Name                   string
	Type                   string
	Nullable               bool
	Comment                *string
	DefaultExpression      *string
	MaterializedExpression *string
	AliasExpression        *string
}

type CreateTableQuery struct {
	Database    string
	Name        string
	ClusterName *string
	Columns     []ColumnDefinition
	Engine      string
	PartitionBy string
	OrderBy     string
	PrimaryKey  string
	SampleBy    string
	TTL         string
	Settings    string
	AsSelect    string
}

type CreateViewQuery struct {
	Database    string
	Name        string
	ClusterName *string
	Columns     []ColumnDefinition
	Query       string
}

type CreateMaterializedViewQuery struct {
	Database    string
	Name        string
	ClusterName *string
	Columns     []ColumnDefinition
	Engine      string
	Populate    bool
	ToTable     string
	ToColumns   []ColumnDefinition
	Query       string
}

func (q CreateTableQuery) Build() (string, error) {
	if err := validateRequiredField(q.Database, "database", "CREATE TABLE"); err != nil {
		return "", err
	}
	if err := validateRequiredField(q.Name, "name", "CREATE TABLE"); err != nil {
		return "", err
	}
	if strings.TrimSpace(q.Engine) == "" {
		return "", errors.New("engine cannot be empty for CREATE TABLE queries")
	}
	if len(q.Columns) == 0 && strings.TrimSpace(q.AsSelect) == "" {
		return "", errors.New("CREATE TABLE queries require at least one column or an as_select query")
	}

	tokens := []string{
		"CREATE",
		"TABLE",
		qualifiedIdentifier(q.Database, q.Name),
	}
	tokens = appendClusterClause(tokens, q.ClusterName)
	if len(q.Columns) > 0 {
		definitions, err := buildColumnDefinitions(q.Columns)
		if err != nil {
			return "", err
		}
		tokens = append(tokens, fmt.Sprintf("(%s)", strings.Join(definitions, ", ")))
	}

	tokens = append(tokens, "ENGINE", "=", strings.TrimSpace(q.Engine))

	if strings.TrimSpace(q.PartitionBy) != "" {
		tokens = append(tokens, "PARTITION BY", strings.TrimSpace(q.PartitionBy))
	}
	if strings.TrimSpace(q.OrderBy) != "" {
		tokens = append(tokens, "ORDER BY", strings.TrimSpace(q.OrderBy))
	}
	if strings.TrimSpace(q.PrimaryKey) != "" {
		tokens = append(tokens, "PRIMARY KEY", strings.TrimSpace(q.PrimaryKey))
	}
	if strings.TrimSpace(q.SampleBy) != "" {
		tokens = append(tokens, "SAMPLE BY", strings.TrimSpace(q.SampleBy))
	}
	if strings.TrimSpace(q.TTL) != "" {
		tokens = append(tokens, "TTL", strings.TrimSpace(q.TTL))
	}
	if strings.TrimSpace(q.Settings) != "" {
		tokens = append(tokens, "SETTINGS", strings.TrimSpace(q.Settings))
	}
	if strings.TrimSpace(q.AsSelect) != "" {
		tokens = append(tokens, "AS", strings.TrimSpace(q.AsSelect))
	}

	return strings.Join(tokens, " ") + ";", nil
}

func (q CreateViewQuery) Build() (string, error) {
	if err := validateRequiredField(q.Database, "database", "CREATE VIEW"); err != nil {
		return "", err
	}
	if err := validateRequiredField(q.Name, "name", "CREATE VIEW"); err != nil {
		return "", err
	}
	if strings.TrimSpace(q.Query) == "" {
		return "", errors.New("query cannot be empty for CREATE VIEW queries")
	}

	tokens := []string{
		"CREATE",
		"VIEW",
		qualifiedIdentifier(q.Database, q.Name),
	}
	tokens = appendClusterClause(tokens, q.ClusterName)
	if len(q.Columns) > 0 {
		signature, err := buildColumnSignatures(q.Columns)
		if err != nil {
			return "", err
		}
		tokens = append(tokens, fmt.Sprintf("(%s)", strings.Join(signature, ", ")))
	}
	tokens = append(tokens, "AS", strings.TrimSpace(q.Query))

	return strings.Join(tokens, " ") + ";", nil
}

func (q CreateMaterializedViewQuery) Build() (string, error) {
	if err := validateRequiredField(q.Database, "database", "CREATE MATERIALIZED VIEW"); err != nil {
		return "", err
	}
	if err := validateRequiredField(q.Name, "name", "CREATE MATERIALIZED VIEW"); err != nil {
		return "", err
	}
	if strings.TrimSpace(q.Query) == "" {
		return "", errors.New("query cannot be empty for CREATE MATERIALIZED VIEW queries")
	}

	hasToTable := strings.TrimSpace(q.ToTable) != ""
	hasEngine := strings.TrimSpace(q.Engine) != ""
	if hasToTable == hasEngine {
		return "", errors.New("CREATE MATERIALIZED VIEW queries require exactly one of to_table or engine")
	}
	if !hasToTable && len(q.ToColumns) > 0 {
		return "", errors.New("to_columns can only be set when to_table is set")
	}
	if hasToTable && len(q.Columns) > 0 {
		return "", errors.New("columns can only be set for engine-backed materialized views")
	}

	tokens := []string{
		"CREATE",
		"MATERIALIZED",
		"VIEW",
		qualifiedIdentifier(q.Database, q.Name),
	}
	tokens = appendClusterClause(tokens, q.ClusterName)
	if len(q.Columns) > 0 {
		definitions, err := buildColumnDefinitions(q.Columns)
		if err != nil {
			return "", err
		}
		tokens = append(tokens, fmt.Sprintf("(%s)", strings.Join(definitions, ", ")))
	}
	if hasToTable {
		tokens = append(tokens, "TO", rawOrQualifiedIdentifier(strings.TrimSpace(q.ToTable)))
		if len(q.ToColumns) > 0 {
			signature, err := buildColumnSignatures(q.ToColumns)
			if err != nil {
				return "", err
			}
			tokens = append(tokens, fmt.Sprintf("(%s)", strings.Join(signature, ", ")))
		}
	}
	if hasEngine {
		tokens = append(tokens, "ENGINE", "=", strings.TrimSpace(q.Engine))
	}
	if q.Populate {
		tokens = append(tokens, "POPULATE")
	}
	tokens = append(tokens, "AS", strings.TrimSpace(q.Query))

	return strings.Join(tokens, " ") + ";", nil
}

func buildColumnDefinitions(columns []ColumnDefinition) ([]string, error) {
	return buildDefinitions(columns, buildColumnDefinition)
}

func buildColumnSignatures(columns []ColumnDefinition) ([]string, error) {
	return buildDefinitions(columns, buildColumnSignature)
}

func buildColumnDefinition(column ColumnDefinition) (string, error) {
	return buildColumnDefinitionWithComment(column, true)
}

func buildColumnSignature(column ColumnDefinition) (string, error) {
	name := strings.TrimSpace(column.Name)
	if name == "" {
		return "", errors.New("column name cannot be empty")
	}

	typeSQL, err := columnTypeSQL(column)
	if err != nil {
		return "", err
	}

	return strings.Join([]string{backtick(name), typeSQL}, " "), nil
}

func columnTypeSQL(column ColumnDefinition) (string, error) {
	return typeSQL(column.Type, column.Nullable, "column")
}

func typeSQL(rawType string, nullable bool, label string) (string, error) {
	t := strings.TrimSpace(rawType)
	if t == "" {
		return "", fmt.Errorf("%s type cannot be empty", label)
	}

	if nullable {
		return fmt.Sprintf("Nullable(%s)", t), nil
	}

	return t, nil
}
