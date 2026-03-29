Use the *clickhousedbops_table* resource to manage ClickHouse objects created with `CREATE TABLE`, including local tables, distributed tables, Kafka tables, and other engine-backed table definitions.

The `columns` attribute is a list of objects so you can define a single local value and reuse it across multiple resources.

Update behavior is engine-aware:

- MergeTree-family tables are updated in place for supported column changes, `SAMPLE BY`, `TTL`, append-only `ORDER BY` extensions that introduce newly-added columns in the same change, and mutable table settings.
- Distributed tables are updated in place for column changes, but settings changes still force replacement.
- Kafka tables are treated as replacement-oriented for schema changes because ClickHouse does not support the necessary `ALTER TABLE` operations there.
- Engines without an explicit in-place strategy currently fall back to replacement for schema changes.

Changes that ClickHouse cannot alter safely in place, such as engine changes, `partition_by`, `primary_key`, `as_select`, unsupported `order_by` rewrites, or readonly table settings, still force replacement.
