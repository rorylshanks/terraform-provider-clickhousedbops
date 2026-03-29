Use the *clickhousedbops_dictionary* resource to manage ClickHouse dictionaries created with `CREATE DICTIONARY`.

The `attributes` block is a list of objects so you can define a single local value and reuse it across dictionaries and the tables they read from.

Known limitations:

- Schema changes are currently applied as drop-and-recreate operations.
