package store

import (
	"context"
	"testing"
)

func TestMySQLSchemaGuardPreservesLiteralNullAndEmptyDefaults(t *testing.T) {
	s := mysqlTestStore(t)
	ctx := context.Background()
	if _, err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, definition := range []string{
		"ENUM('null','custom') NULL DEFAULT 'null'",
		"ENUM('','custom') NULL DEFAULT ''",
		"ENUM('zip','custom') NULL DEFAULT NULL",
	} {
		if _, err := s.DB.Exec("ALTER TABLE task_detail MODIFY archive_type " + definition); err != nil {
			t.Fatal(err)
		}
		before, err := readEnumColumn(ctx, s.DB, criticalEnums[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.RepairCriticalSchema(ctx); err != nil {
			t.Fatal(err)
		}
		after, err := readEnumColumn(ctx, s.DB, criticalEnums[0])
		if err != nil {
			t.Fatal(err)
		}
		if before.def != after.def || before.nullable != after.nullable {
			t.Fatal("default/nullability changed", before.def, after.def)
		}
	}
	// Missing tables or a read-only store cannot be repaired by guessing.
	ro := &Store{DB: s.DB, Dialect: "mysql", readOnly: true}
	if _, err := ro.RepairCriticalSchema(ctx); err == nil {
		t.Fatal("read-only repair allowed")
	}
}
