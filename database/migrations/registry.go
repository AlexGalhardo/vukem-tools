// Package migrations holds the schema migrations.
package migrations

import "github.com/AlexGalhardo/vukem/schema"

// All lists the migrations in order; vukem make:migration appends here.
func All() []schema.Migration {
	return []schema.Migration{
		m20261018134806CreateUsersTable(),
		m20261018134807CreateAuthTables(),
		m20261018150000CreateConversionsTables(),
		m20261018150001CreateFilesTable(),
		m20261018150002CreateUsageCountersTable(),
	}
}
