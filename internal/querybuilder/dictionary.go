package querybuilder

import (
	"fmt"
	"strings"

	"github.com/pingcap/errors"
)

type DictionaryAttributeDefinition struct {
	Name              string
	Type              string
	Nullable          bool
	DefaultExpression *string
	Expression        *string
	Hierarchical      bool
	Injective         bool
	IsObjectID        bool
}

type CreateDictionaryQueryBuilder interface {
	QueryBuilder
	WithCluster(clusterName *string) CreateDictionaryQueryBuilder
	WithAttributes(attributes []DictionaryAttributeDefinition) CreateDictionaryQueryBuilder
	WithPrimaryKey(primaryKey []string) CreateDictionaryQueryBuilder
	WithSource(source string) CreateDictionaryQueryBuilder
	WithLayout(layout string) CreateDictionaryQueryBuilder
	WithLifetime(lifetime string) CreateDictionaryQueryBuilder
	WithSettings(settings string) CreateDictionaryQueryBuilder
	WithComment(comment string) CreateDictionaryQueryBuilder
}

type ShowCreateDictionaryQueryBuilder interface {
	QueryBuilder
}

type createDictionaryQueryBuilder struct {
	database    string
	name        string
	clusterName *string
	attributes  []DictionaryAttributeDefinition
	primaryKey  []string
	source      *string
	layout      *string
	lifetime    *string
	settings    *string
	comment     *string
}

type showCreateDictionaryQueryBuilder struct {
	database string
	name     string
}

func NewCreateDictionary(database string, name string) CreateDictionaryQueryBuilder {
	return &createDictionaryQueryBuilder{
		database: database,
		name:     name,
	}
}

func NewShowCreateDictionary(database string, name string) ShowCreateDictionaryQueryBuilder {
	return &showCreateDictionaryQueryBuilder{
		database: database,
		name:     name,
	}
}

func (q *createDictionaryQueryBuilder) WithCluster(clusterName *string) CreateDictionaryQueryBuilder {
	q.clusterName = clusterName
	return q
}

func (q *createDictionaryQueryBuilder) WithAttributes(attributes []DictionaryAttributeDefinition) CreateDictionaryQueryBuilder {
	q.attributes = attributes
	return q
}

func (q *createDictionaryQueryBuilder) WithPrimaryKey(primaryKey []string) CreateDictionaryQueryBuilder {
	q.primaryKey = primaryKey
	return q
}

func (q *createDictionaryQueryBuilder) WithSource(source string) CreateDictionaryQueryBuilder {
	q.source = &source
	return q
}

func (q *createDictionaryQueryBuilder) WithLayout(layout string) CreateDictionaryQueryBuilder {
	q.layout = &layout
	return q
}

func (q *createDictionaryQueryBuilder) WithLifetime(lifetime string) CreateDictionaryQueryBuilder {
	q.lifetime = &lifetime
	return q
}

func (q *createDictionaryQueryBuilder) WithSettings(settings string) CreateDictionaryQueryBuilder {
	q.settings = &settings
	return q
}

func (q *createDictionaryQueryBuilder) WithComment(comment string) CreateDictionaryQueryBuilder {
	q.comment = &comment
	return q
}

func (q *createDictionaryQueryBuilder) Build() (string, error) {
	if err := validateRequiredField(q.database, "database", "CREATE DICTIONARY"); err != nil {
		return "", err
	}
	if err := validateRequiredField(q.name, "name", "CREATE DICTIONARY"); err != nil {
		return "", err
	}
	if len(q.attributes) == 0 {
		return "", errors.New("CREATE DICTIONARY queries require at least one attribute")
	}
	if len(q.primaryKey) == 0 {
		return "", errors.New("CREATE DICTIONARY queries require at least one primary key attribute")
	}
	if isNilOrEmpty(q.source) {
		return "", errors.New("source cannot be empty for CREATE DICTIONARY queries")
	}
	if isNilOrEmpty(q.layout) {
		return "", errors.New("layout cannot be empty for CREATE DICTIONARY queries")
	}
	if isNilOrEmpty(q.lifetime) {
		return "", errors.New("lifetime cannot be empty for CREATE DICTIONARY queries")
	}

	attributeDefinitions, err := buildDictionaryAttributeDefinitions(q.attributes)
	if err != nil {
		return "", err
	}

	tokens := []string{
		"CREATE",
		"DICTIONARY",
		qualifiedIdentifier(q.database, q.name),
	}
	tokens = appendClusterClause(tokens, q.clusterName)

	tokens = append(tokens,
		fmt.Sprintf("(%s)", strings.Join(attributeDefinitions, ", ")),
		"PRIMARY KEY",
		strings.Join(backtickAll(q.primaryKey), ", "),
		fmt.Sprintf("SOURCE(%s)", strings.TrimSpace(*q.source)),
		fmt.Sprintf("LAYOUT(%s)", strings.TrimSpace(*q.layout)),
		fmt.Sprintf("LIFETIME(%s)", strings.TrimSpace(*q.lifetime)),
	)

	if !isNilOrEmpty(q.settings) {
		tokens = append(tokens, "SETTINGS", strings.TrimSpace(*q.settings))
	}
	if !isNilOrEmpty(q.comment) {
		tokens = append(tokens, "COMMENT", quote(strings.TrimSpace(*q.comment)))
	}

	return strings.Join(tokens, " ") + ";", nil
}

func (q *showCreateDictionaryQueryBuilder) Build() (string, error) {
	if err := validateRequiredField(q.database, "database", "SHOW CREATE DICTIONARY"); err != nil {
		return "", err
	}
	if err := validateRequiredField(q.name, "name", "SHOW CREATE DICTIONARY"); err != nil {
		return "", err
	}

	return fmt.Sprintf("SHOW CREATE DICTIONARY %s;", qualifiedIdentifier(q.database, q.name)), nil
}

func buildDictionaryAttributeDefinitions(attributes []DictionaryAttributeDefinition) ([]string, error) {
	return buildDefinitions(attributes, buildDictionaryAttributeDefinition)
}

func buildDictionaryAttributeDefinition(attribute DictionaryAttributeDefinition) (string, error) {
	name := strings.TrimSpace(attribute.Name)
	if name == "" {
		return "", errors.New("dictionary attribute name cannot be empty")
	}

	typeSQL, err := dictionaryAttributeTypeSQL(attribute)
	if err != nil {
		return "", err
	}

	tokens := []string{
		backtick(name),
		typeSQL,
	}

	expressionCount := 0
	if !isNilOrEmpty(attribute.DefaultExpression) {
		expressionCount++
		tokens = append(tokens, "DEFAULT", strings.TrimSpace(*attribute.DefaultExpression))
	}
	if !isNilOrEmpty(attribute.Expression) {
		expressionCount++
		tokens = append(tokens, "EXPRESSION", strings.TrimSpace(*attribute.Expression))
	}
	if expressionCount > 1 {
		return "", errors.New("only one of default_expression or expression can be set for a dictionary attribute")
	}

	if attribute.Hierarchical && attribute.Injective {
		return "", errors.New("dictionary attributes cannot be both hierarchical and injective")
	}
	if attribute.Hierarchical {
		tokens = append(tokens, "HIERARCHICAL")
	}
	if attribute.Injective {
		tokens = append(tokens, "INJECTIVE")
	}
	if attribute.IsObjectID {
		tokens = append(tokens, "IS_OBJECT_ID")
	}

	return strings.Join(tokens, " "), nil
}

func dictionaryAttributeTypeSQL(attribute DictionaryAttributeDefinition) (string, error) {
	return typeSQL(attribute.Type, attribute.Nullable, "dictionary attribute")
}
