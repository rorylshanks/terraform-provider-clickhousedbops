package dbops

import (
	"context"

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

	return dictionary, nil
}

func (i *impl) DeleteDictionary(ctx context.Context, database string, name string, clusterName *string) error {
	dictionary, err := i.GetDictionary(ctx, database, name, clusterName)
	if err != nil {
		return err
	}
	if dictionary == nil {
		return nil
	}

	sql, err := querybuilder.NewDropDictionary(database, name).WithCluster(clusterName).Build()
	if err != nil {
		return errors.WithMessage(err, "error building query")
	}

	if err := i.clickhouseClient.Exec(ctx, sql); err != nil {
		return errors.WithMessage(err, "error running query")
	}

	return nil
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
