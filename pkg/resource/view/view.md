Use the *clickhousedbops_view* resource to manage ClickHouse views created with `CREATE VIEW`.

The `columns` attribute is optional and can be shared from Terraform locals in the same way as table column definitions.

Known limitations:

- Schema changes are currently applied as drop-and-recreate operations.
