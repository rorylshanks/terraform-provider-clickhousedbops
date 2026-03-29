Use the *clickhousedbops_materialized_view* resource to manage ClickHouse materialized views created with `CREATE MATERIALIZED VIEW`.

You can either define a destination `to_table` or an inline `engine`, and `to_columns` lets you reuse the same shared column list across the target table and the materialized view definition.

Known limitations:

- Schema changes are currently applied as drop-and-recreate operations.
