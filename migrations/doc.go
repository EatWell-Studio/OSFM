// Package migrations holds the database migrations: Go migrations registered with PocketBase's app migration list
// (core.AppMigrations) when the package is imported. [Bind] makes the application apply them at every start, after
// backing up an existing database that has migrations to apply.
//
// Each file creates or changes a group of collections. An Up migration builds the wanted state onto what already
// exists, so it can run again without changing anything; its Down migration removes what it added.
package migrations
