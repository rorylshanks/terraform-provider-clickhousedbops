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

type CreateTableQueryBuilder interface {
	QueryBuilder
	WithCluster(clusterName *string) CreateTableQueryBuilder
	WithColumns(columns []ColumnDefinition) CreateTableQueryBuilder
	WithEngine(engine string) CreateTableQueryBuilder
	WithPartitionBy(partitionBy string) CreateTableQueryBuilder
	WithOrderBy(orderBy string) CreateTableQueryBuilder
	WithPrimaryKey(primaryKey string) CreateTableQueryBuilder
	WithSampleBy(sampleBy string) CreateTableQueryBuilder
	WithTTL(ttl string) CreateTableQueryBuilder
	WithSettings(settings string) CreateTableQueryBuilder
	WithAsSelect(query string) CreateTableQueryBuilder
}

type CreateViewQueryBuilder interface {
	QueryBuilder
	WithCluster(clusterName *string) CreateViewQueryBuilder
	WithColumns(columns []ColumnDefinition) CreateViewQueryBuilder
	WithQuery(query string) CreateViewQueryBuilder
}

type CreateMaterializedViewQueryBuilder interface {
	QueryBuilder
	WithCluster(clusterName *string) CreateMaterializedViewQueryBuilder
	WithColumns(columns []ColumnDefinition) CreateMaterializedViewQueryBuilder
	WithEngine(engine string) CreateMaterializedViewQueryBuilder
	WithPopulate(populate bool) CreateMaterializedViewQueryBuilder
	WithToTable(table string) CreateMaterializedViewQueryBuilder
	WithToColumns(columns []ColumnDefinition) CreateMaterializedViewQueryBuilder
	WithQuery(query string) CreateMaterializedViewQueryBuilder
}

type createTableQueryBuilder struct {
	database    string
	name        string
	clusterName *string
	columns     []ColumnDefinition
	engine      *string
	partitionBy *string
	orderBy     *string
	primaryKey  *string
	sampleBy    *string
	ttl         *string
	settings    *string
	asSelect    *string
}

type createViewQueryBuilder struct {
	database    string
	name        string
	clusterName *string
	columns     []ColumnDefinition
	query       *string
}

type createMaterializedViewQueryBuilder struct {
	database    string
	name        string
	clusterName *string
	columns     []ColumnDefinition
	engine      *string
	populate    bool
	toTable     *string
	toColumns   []ColumnDefinition
	query       *string
}

func NewCreateTable(database string, name string) CreateTableQueryBuilder {
	return &createTableQueryBuilder{
		database: database,
		name:     name,
	}
}

func NewCreateView(database string, name string) CreateViewQueryBuilder {
	return &createViewQueryBuilder{
		database: database,
		name:     name,
	}
}

func NewCreateMaterializedView(database string, name string) CreateMaterializedViewQueryBuilder {
	return &createMaterializedViewQueryBuilder{
		database: database,
		name:     name,
	}
}

func (q *createTableQueryBuilder) WithCluster(clusterName *string) CreateTableQueryBuilder {
	q.clusterName = clusterName
	return q
}

func (q *createTableQueryBuilder) WithColumns(columns []ColumnDefinition) CreateTableQueryBuilder {
	q.columns = columns
	return q
}

func (q *createTableQueryBuilder) WithEngine(engine string) CreateTableQueryBuilder {
	q.engine = &engine
	return q
}

func (q *createTableQueryBuilder) WithPartitionBy(partitionBy string) CreateTableQueryBuilder {
	q.partitionBy = &partitionBy
	return q
}

func (q *createTableQueryBuilder) WithOrderBy(orderBy string) CreateTableQueryBuilder {
	q.orderBy = &orderBy
	return q
}

func (q *createTableQueryBuilder) WithPrimaryKey(primaryKey string) CreateTableQueryBuilder {
	q.primaryKey = &primaryKey
	return q
}

func (q *createTableQueryBuilder) WithSampleBy(sampleBy string) CreateTableQueryBuilder {
	q.sampleBy = &sampleBy
	return q
}

func (q *createTableQueryBuilder) WithTTL(ttl string) CreateTableQueryBuilder {
	q.ttl = &ttl
	return q
}

func (q *createTableQueryBuilder) WithSettings(settings string) CreateTableQueryBuilder {
	q.settings = &settings
	return q
}

func (q *createTableQueryBuilder) WithAsSelect(query string) CreateTableQueryBuilder {
	q.asSelect = &query
	return q
}

func (q *createTableQueryBuilder) Build() (string, error) {
	if err := validateRequiredField(q.database, "database", "CREATE TABLE"); err != nil {
		return "", err
	}
	if err := validateRequiredField(q.name, "name", "CREATE TABLE"); err != nil {
		return "", err
	}
	if q.engine == nil || strings.TrimSpace(*q.engine) == "" {
		return "", errors.New("engine cannot be empty for CREATE TABLE queries")
	}
	if len(q.columns) == 0 && isNilOrEmpty(q.asSelect) {
		return "", errors.New("CREATE TABLE queries require at least one column or an as_select query")
	}

	tokens := []string{
		"CREATE",
		"TABLE",
		qualifiedIdentifier(q.database, q.name),
	}
	tokens = appendClusterClause(tokens, q.clusterName)
	if len(q.columns) > 0 {
		definitions, err := buildColumnDefinitions(q.columns)
		if err != nil {
			return "", err
		}
		tokens = append(tokens, fmt.Sprintf("(%s)", strings.Join(definitions, ", ")))
	}

	tokens = append(tokens, "ENGINE", "=", strings.TrimSpace(*q.engine))

	if !isNilOrEmpty(q.partitionBy) {
		tokens = append(tokens, "PARTITION BY", strings.TrimSpace(*q.partitionBy))
	}
	if !isNilOrEmpty(q.orderBy) {
		tokens = append(tokens, "ORDER BY", strings.TrimSpace(*q.orderBy))
	}
	if !isNilOrEmpty(q.primaryKey) {
		tokens = append(tokens, "PRIMARY KEY", strings.TrimSpace(*q.primaryKey))
	}
	if !isNilOrEmpty(q.sampleBy) {
		tokens = append(tokens, "SAMPLE BY", strings.TrimSpace(*q.sampleBy))
	}
	if !isNilOrEmpty(q.ttl) {
		tokens = append(tokens, "TTL", strings.TrimSpace(*q.ttl))
	}
	if !isNilOrEmpty(q.settings) {
		tokens = append(tokens, "SETTINGS", strings.TrimSpace(*q.settings))
	}
	if !isNilOrEmpty(q.asSelect) {
		tokens = append(tokens, "AS", strings.TrimSpace(*q.asSelect))
	}

	return strings.Join(tokens, " ") + ";", nil
}

func (q *createViewQueryBuilder) WithCluster(clusterName *string) CreateViewQueryBuilder {
	q.clusterName = clusterName
	return q
}

func (q *createViewQueryBuilder) WithColumns(columns []ColumnDefinition) CreateViewQueryBuilder {
	q.columns = columns
	return q
}

func (q *createViewQueryBuilder) WithQuery(query string) CreateViewQueryBuilder {
	q.query = &query
	return q
}

func (q *createViewQueryBuilder) Build() (string, error) {
	if err := validateRequiredField(q.database, "database", "CREATE VIEW"); err != nil {
		return "", err
	}
	if err := validateRequiredField(q.name, "name", "CREATE VIEW"); err != nil {
		return "", err
	}
	if isNilOrEmpty(q.query) {
		return "", errors.New("query cannot be empty for CREATE VIEW queries")
	}

	tokens := []string{
		"CREATE",
		"VIEW",
		qualifiedIdentifier(q.database, q.name),
	}
	tokens = appendClusterClause(tokens, q.clusterName)
	if len(q.columns) > 0 {
		signature, err := buildColumnSignatures(q.columns)
		if err != nil {
			return "", err
		}
		tokens = append(tokens, fmt.Sprintf("(%s)", strings.Join(signature, ", ")))
	}
	tokens = append(tokens, "AS", strings.TrimSpace(*q.query))

	return strings.Join(tokens, " ") + ";", nil
}

func (q *createMaterializedViewQueryBuilder) WithCluster(clusterName *string) CreateMaterializedViewQueryBuilder {
	q.clusterName = clusterName
	return q
}

func (q *createMaterializedViewQueryBuilder) WithColumns(columns []ColumnDefinition) CreateMaterializedViewQueryBuilder {
	q.columns = columns
	return q
}

func (q *createMaterializedViewQueryBuilder) WithEngine(engine string) CreateMaterializedViewQueryBuilder {
	q.engine = &engine
	return q
}

func (q *createMaterializedViewQueryBuilder) WithPopulate(populate bool) CreateMaterializedViewQueryBuilder {
	q.populate = populate
	return q
}

func (q *createMaterializedViewQueryBuilder) WithToTable(table string) CreateMaterializedViewQueryBuilder {
	q.toTable = &table
	return q
}

func (q *createMaterializedViewQueryBuilder) WithToColumns(columns []ColumnDefinition) CreateMaterializedViewQueryBuilder {
	q.toColumns = columns
	return q
}

func (q *createMaterializedViewQueryBuilder) WithQuery(query string) CreateMaterializedViewQueryBuilder {
	q.query = &query
	return q
}

func (q *createMaterializedViewQueryBuilder) Build() (string, error) {
	if err := validateRequiredField(q.database, "database", "CREATE MATERIALIZED VIEW"); err != nil {
		return "", err
	}
	if err := validateRequiredField(q.name, "name", "CREATE MATERIALIZED VIEW"); err != nil {
		return "", err
	}
	if isNilOrEmpty(q.query) {
		return "", errors.New("query cannot be empty for CREATE MATERIALIZED VIEW queries")
	}

	hasToTable := !isNilOrEmpty(q.toTable)
	hasEngine := !isNilOrEmpty(q.engine)
	if hasToTable == hasEngine {
		return "", errors.New("CREATE MATERIALIZED VIEW queries require exactly one of to_table or engine")
	}
	if !hasToTable && len(q.toColumns) > 0 {
		return "", errors.New("to_columns can only be set when to_table is set")
	}
	if hasToTable && len(q.columns) > 0 {
		return "", errors.New("columns can only be set for engine-backed materialized views")
	}

	tokens := []string{
		"CREATE",
		"MATERIALIZED",
		"VIEW",
		qualifiedIdentifier(q.database, q.name),
	}
	tokens = appendClusterClause(tokens, q.clusterName)
	if len(q.columns) > 0 {
		definitions, err := buildColumnDefinitions(q.columns)
		if err != nil {
			return "", err
		}
		tokens = append(tokens, fmt.Sprintf("(%s)", strings.Join(definitions, ", ")))
	}
	if hasToTable {
		tokens = append(tokens, "TO", rawOrQualifiedIdentifier(strings.TrimSpace(*q.toTable)))
		if len(q.toColumns) > 0 {
			signature, err := buildColumnSignatures(q.toColumns)
			if err != nil {
				return "", err
			}
			tokens = append(tokens, fmt.Sprintf("(%s)", strings.Join(signature, ", ")))
		}
	}
	if hasEngine {
		tokens = append(tokens, "ENGINE", "=", strings.TrimSpace(*q.engine))
	}
	if q.populate {
		tokens = append(tokens, "POPULATE")
	}
	tokens = append(tokens, "AS", strings.TrimSpace(*q.query))

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

