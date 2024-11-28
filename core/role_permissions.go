package core

// RolePermissions defines the interface for role-based permission management
type RolePermissions interface {
	// GetTableQueryPermission returns query permissions for a specific table
	GetTableQueryPermission(roleKey, schema, table, field string) (*Query, error)

	// GetTableInsertPermission returns insert permissions for a specific table
	GetTableInsertPermission(roleKey, schema, table, field string) (*Insert, error)

	// GetTableUpdatePermission returns update permissions for a specific table
	GetTableUpdatePermission(roleKey, schema, table, field string) (*Update, error)

	// GetTableUpsertPermission returns upsert permissions for a specific table
	GetTableUpsertPermission(roleKey, schema, table, field string) (*Upsert, error)

	// GetTableDeletePermission returns delete permissions for a specific table
	GetTableDeletePermission(roleKey, schema, table, field string) (*Delete, error)

	// GetTablePermissions returns all permissions for a specific table
	GetTablePermissions(roleKey, schema, table, field string) (*RoleTable, error)

	// GetRoleMatch returns the match condition for a role if any
	GetRoleMatch(roleKey string) (string, error)
}
