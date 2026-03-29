package dbops

import (
	"context"
	"strings"

	"github.com/pingcap/errors"

	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/clickhouseclient"
	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/querybuilder"
)

type DictionaryAttribute struct {
	Name              string
	Type              string
	Nullable          bool
	DefaultExpression *string
	Expression        *string
	Hierarchical      bool
	Injective         bool
	IsObjectID        bool
}

type Dictionary struct {
	Database        string
	Name            string
	Comment         string
	Attributes      []DictionaryAttribute
	PrimaryKey      []string
	Source          string
	Layout          string
	Lifetime        string
	Settings        string
	CreateStatement string
}

func (i *impl) CreateDictionary(ctx context.Context, dictionary Dictionary, clusterName *string) (*Dictionary, error) {
	builder := querybuilder.NewCreateDictionary(dictionary.Database, dictionary.Name).
		WithCluster(clusterName).
		WithAttributes(toQueryBuilderDictionaryAttributes(dictionary.Attributes)).
		WithPrimaryKey(dictionary.PrimaryKey).
		WithSource(dictionary.Source).
		WithLayout(dictionary.Layout).
		WithLifetime(dictionary.Lifetime)

	if dictionary.Settings != "" {
		builder = builder.WithSettings(dictionary.Settings)
	}
	if dictionary.Comment != "" {
		builder = builder.WithComment(dictionary.Comment)
	}

	sql, err := builder.Build()
	if err != nil {
		return nil, errors.WithMessage(err, "error building query")
	}

	if err := i.clickhouseClient.Exec(ctx, sql); err != nil {
		return nil, errors.WithMessage(err, "error running query")
	}

	return i.GetDictionary(ctx, dictionary.Database, dictionary.Name, clusterName)
}

func (i *impl) GetDictionary(ctx context.Context, database string, name string, clusterName *string) (*Dictionary, error) {
	sql, err := querybuilder.NewSelect(
		[]querybuilder.Field{
			querybuilder.NewField("database"),
			querybuilder.NewField("name"),
			querybuilder.NewField("comment"),
		},
		"system.dictionaries",
	).WithCluster(clusterName).Where(
		querybuilder.WhereEquals("database", database),
		querybuilder.WhereEquals("name", name),
	).Build()
	if err != nil {
		return nil, errors.WithMessage(err, "error building query")
	}

	var dictionary *Dictionary
	err = i.clickhouseClient.Select(ctx, sql, func(data clickhouseclient.Row) error {
		if dictionary != nil {
			return nil
		}

		dbName, err := data.GetString("database")
		if err != nil {
			return errors.WithMessage(err, "error scanning query result, missing 'database' field")
		}
		dictName, err := data.GetString("name")
		if err != nil {
			return errors.WithMessage(err, "error scanning query result, missing 'name' field")
		}
		comment, err := data.GetString("comment")
		if err != nil {
			return errors.WithMessage(err, "error scanning query result, missing 'comment' field")
		}

		dictionary = &Dictionary{
			Database: dbName,
			Name:     dictName,
			Comment:  comment,
		}
		return nil
	})
	if err != nil {
		return nil, errors.WithMessage(err, "error running query")
	}

	if dictionary == nil {
		return nil, nil
	}

	showCreateSQL, err := querybuilder.NewShowCreateDictionary(database, name).Build()
	if err != nil {
		return nil, errors.WithMessage(err, "error building query")
	}

	err = i.clickhouseClient.Select(ctx, showCreateSQL, func(data clickhouseclient.Row) error {
		statement, err := data.GetString("statement")
		if err != nil {
			return errors.WithMessage(err, "error scanning query result, missing 'statement' field")
		}
		dictionary.CreateStatement = statement
		return nil
	})
	if err != nil {
		return nil, errors.WithMessage(err, "error running SHOW CREATE DICTIONARY")
	}

	if dictionary.CreateStatement == "" {
		return nil, errors.Errorf("failed retrieving SHOW CREATE DICTIONARY for %s.%s", database, name)
	}

	definition, err := parseCreateDictionaryDefinition(dictionary.CreateStatement)
	if err != nil {
		return nil, errors.WithMessage(err, "error parsing CREATE DICTIONARY statement")
	}

	dictionary.Attributes = definition.Attributes
	dictionary.PrimaryKey = definition.PrimaryKey
	dictionary.Source = definition.Source
	dictionary.Layout = definition.Layout
	dictionary.Lifetime = definition.Lifetime
	if definition.Settings != "" {
		dictionary.Settings = definition.Settings
	}

	return dictionary, nil
}

func (i *impl) DeleteDictionary(ctx context.Context, database string, name string, clusterName *string) error {
	dictionary, err := i.GetDictionary(ctx, database, name, clusterName)
	if err != nil {
		return err
	}
	return i.deleteIfExists(ctx, dictionary != nil, querybuilder.NewDropDictionary(database, name).WithCluster(clusterName))
}

func toQueryBuilderDictionaryAttributes(attributes []DictionaryAttribute) []querybuilder.DictionaryAttributeDefinition {
	if len(attributes) == 0 {
		return nil
	}

	ret := make([]querybuilder.DictionaryAttributeDefinition, 0, len(attributes))
	for _, attribute := range attributes {
		ret = append(ret, querybuilder.DictionaryAttributeDefinition{
			Name:              attribute.Name,
			Type:              attribute.Type,
			Nullable:          attribute.Nullable,
			DefaultExpression: attribute.DefaultExpression,
			Expression:        attribute.Expression,
			Hierarchical:      attribute.Hierarchical,
			Injective:         attribute.Injective,
			IsObjectID:        attribute.IsObjectID,
		})
	}

	return ret
}

type createDictionaryDefinition struct {
	Attributes []DictionaryAttribute
	PrimaryKey []string
	Source     string
	Layout     string
	Lifetime   string
	Settings   string
}

func parseCreateDictionaryDefinition(createStatement string) (createDictionaryDefinition, error) {
	definition := createDictionaryDefinition{}
	statement := strings.TrimSpace(strings.TrimSuffix(createStatement, ";"))
	if statement == "" {
		return definition, nil
	}

	// Extract attributes from the parenthesized list after the dictionary identifier
	openIdx, closeIdx, ok, err := findTrailingDictionaryColumns(statement)
	if err != nil {
		return definition, err
	}
	if ok {
		attributes, err := parseDictionaryAttributes(statement[openIdx+1 : closeIdx])
		if err != nil {
			return definition, err
		}
		definition.Attributes = attributes
	}

	// Parse clauses after the column list
	remainder := statement
	if ok {
		remainder = strings.TrimSpace(statement[closeIdx+1:])
	}

	// PRIMARY KEY
	pkIndex, err := findTopLevelKeyword(remainder, "PRIMARY KEY", 0)
	if err != nil {
		return definition, err
	}
	if pkIndex != -1 {
		pkValue := remainder[pkIndex+len("PRIMARY KEY"):]
		// PRIMARY KEY value ends at SOURCE
		srcIndex, err := findTopLevelKeyword(pkValue, "SOURCE", 0)
		if err != nil {
			return definition, err
		}
		if srcIndex != -1 {
			pkValue = pkValue[:srcIndex]
		}
		definition.PrimaryKey = parsePrimaryKeyList(strings.TrimSpace(pkValue))
	}

	// SOURCE(...) — extract inner content
	srcIndex, err := findTopLevelKeyword(remainder, "SOURCE", 0)
	if err != nil {
		return definition, err
	}
	if srcIndex != -1 {
		inner, end, err := extractTopLevelParenContent(remainder, srcIndex+len("SOURCE"))
		if err != nil {
			return definition, errors.WithMessage(err, "error parsing SOURCE clause")
		}
		definition.Source = inner
		remainder = remainder[end:]
	}

	// LIFETIME(...) — extract inner content
	ltIndex, err := findTopLevelKeyword(remainder, "LIFETIME", 0)
	if err != nil {
		return definition, err
	}
	if ltIndex != -1 {
		inner, end, err := extractTopLevelParenContent(remainder, ltIndex+len("LIFETIME"))
		if err != nil {
			return definition, errors.WithMessage(err, "error parsing LIFETIME clause")
		}
		definition.Lifetime = inner
		remainder = remainder[end:]
	}

	// LAYOUT(...) — extract inner content
	layIndex, err := findTopLevelKeyword(remainder, "LAYOUT", 0)
	if err != nil {
		return definition, err
	}
	if layIndex != -1 {
		inner, end, err := extractTopLevelParenContent(remainder, layIndex+len("LAYOUT"))
		if err != nil {
			return definition, errors.WithMessage(err, "error parsing LAYOUT clause")
		}
		definition.Layout = inner
		remainder = remainder[end:]
	}

	// SETTINGS
	setIndex, err := findTopLevelKeyword(remainder, "SETTINGS", 0)
	if err != nil {
		return definition, err
	}
	if setIndex != -1 {
		settingsValue := strings.TrimSpace(remainder[setIndex+len("SETTINGS"):])
		// Settings end at COMMENT or end of string
		commentIndex, err := findTopLevelKeyword(settingsValue, "COMMENT", 0)
		if err != nil {
			return definition, err
		}
		if commentIndex != -1 {
			settingsValue = strings.TrimSpace(settingsValue[:commentIndex])
		}
		definition.Settings = settingsValue
	}

	return definition, nil
}

// findTrailingDictionaryColumns finds the first top-level parenthesized group
// in the CREATE DICTIONARY statement — this is the attribute list.
func findTrailingDictionaryColumns(statement string) (int, int, bool, error) {
	// Find PRIMARY KEY to limit our search
	pkIndex, err := findTopLevelKeyword(statement, "PRIMARY KEY", 0)
	if err != nil {
		return 0, 0, false, err
	}
	searchIn := statement
	if pkIndex != -1 {
		searchIn = statement[:pkIndex]
	}

	return findTrailingTopLevelParentheses(searchIn)
}

// extractTopLevelParenContent extracts the content between balanced parentheses
// starting the search from startIdx. Returns inner content and end position.
func extractTopLevelParenContent(raw string, startIdx int) (string, int, error) {
	// Find opening paren
	openIdx := strings.IndexByte(raw[startIdx:], '(')
	if openIdx == -1 {
		return "", startIdx, errors.New("expected '(' not found")
	}
	openIdx += startIdx

	// Scan for matching close, tracking paren depth independently of the SQL
	// scanner state. The scanner state is used only to skip quoted strings so
	// that parentheses inside string literals are not counted.
	depth := 0
	state := querybuilder.SQLScanState{}
	for i := openIdx; i < len(raw); i++ {
		ch := raw[i]
		var err error
		i, err = querybuilder.AdvanceSQLScanState(raw, i, &state)
		if err != nil {
			return "", 0, err
		}
		// Skip characters inside quoted strings.
		if state.InQuote != 0 {
			continue
		}
		if ch == '(' {
			depth++
		} else if ch == ')' {
			depth--
			if depth == 0 {
				return strings.TrimSpace(raw[openIdx+1 : i]), i + 1, nil
			}
		}
	}
	return "", 0, errors.New("unbalanced parentheses")
}

// parseDictionaryAttributes parses the comma-separated attribute definitions.
func parseDictionaryAttributes(raw string) ([]DictionaryAttribute, error) {
	parts, err := splitTopLevelCSV(raw)
	if err != nil {
		return nil, err
	}

	attributes := make([]DictionaryAttribute, 0, len(parts))
	for _, part := range parts {
		attr, err := parseDictionaryAttribute(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		attributes = append(attributes, attr)
	}
	return attributes, nil
}

// parseDictionaryAttribute parses a single dictionary attribute definition.
// Format: name Type [DEFAULT expr] [EXPRESSION expr] [HIERARCHICAL] [INJECTIVE] [IS_OBJECT_ID]
func parseDictionaryAttribute(raw string) (DictionaryAttribute, error) {
	attr := DictionaryAttribute{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return attr, errors.New("empty dictionary attribute definition")
	}

	// Extract name
	nameEnd, err := findColumnNameEnd(raw)
	if err != nil {
		return attr, err
	}
	attr.Name = unquoteIdentifier(raw[:nameEnd])
	remainder := strings.TrimSpace(raw[nameEnd:])

	// Extract type — everything up to the first keyword or end
	typEnd := len(remainder)
	for _, kw := range []string{"DEFAULT", "EXPRESSION", "HIERARCHICAL", "INJECTIVE", "IS_OBJECT_ID"} {
		idx, err := findTopLevelKeyword(remainder, kw, 0)
		if err != nil {
			return attr, err
		}
		if idx != -1 && idx < typEnd {
			typEnd = idx
		}
	}

	rawType := strings.TrimSpace(remainder[:typEnd])
	if inner, ok := unwrapNullableType(rawType); ok {
		attr.Type = inner
		attr.Nullable = true
	} else {
		attr.Type = rawType
	}

	remainder = strings.TrimSpace(remainder[typEnd:])

	// Parse modifier keywords
	for remainder != "" {
		if strings.HasPrefix(remainder, "DEFAULT") {
			remainder = strings.TrimSpace(remainder[len("DEFAULT"):])
			expr, rest := extractExpressionUntilKeyword(remainder)
			attr.DefaultExpression = &expr
			remainder = rest
		} else if strings.HasPrefix(remainder, "EXPRESSION") {
			remainder = strings.TrimSpace(remainder[len("EXPRESSION"):])
			expr, rest := extractExpressionUntilKeyword(remainder)
			attr.Expression = &expr
			remainder = rest
		} else if strings.HasPrefix(remainder, "HIERARCHICAL") {
			attr.Hierarchical = true
			remainder = strings.TrimSpace(remainder[len("HIERARCHICAL"):])
		} else if strings.HasPrefix(remainder, "INJECTIVE") {
			attr.Injective = true
			remainder = strings.TrimSpace(remainder[len("INJECTIVE"):])
		} else if strings.HasPrefix(remainder, "IS_OBJECT_ID") {
			attr.IsObjectID = true
			remainder = strings.TrimSpace(remainder[len("IS_OBJECT_ID"):])
		} else {
			break
		}
	}

	return attr, nil
}

// extractExpressionUntilKeyword extracts an expression value that ends before
// the next dictionary attribute keyword or end of string.
func extractExpressionUntilKeyword(raw string) (string, string) {
	keywords := []string{"DEFAULT", "EXPRESSION", "HIERARCHICAL", "INJECTIVE", "IS_OBJECT_ID"}
	minIdx := len(raw)
	for _, kw := range keywords {
		idx, _ := findTopLevelKeyword(raw, kw, 0)
		if idx != -1 && idx < minIdx {
			minIdx = idx
		}
	}
	return strings.TrimSpace(raw[:minIdx]), strings.TrimSpace(raw[minIdx:])
}

// parsePrimaryKeyList parses a PRIMARY KEY value which may be a single identifier
// or a parenthesized comma-separated list.
func parsePrimaryKeyList(raw string) []string {
	raw = strings.TrimSpace(raw)
	// Strip outer parens if present
	if strings.HasPrefix(raw, "(") && strings.HasSuffix(raw, ")") {
		raw = raw[1 : len(raw)-1]
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, unquoteIdentifier(p))
		}
	}
	return result
}
