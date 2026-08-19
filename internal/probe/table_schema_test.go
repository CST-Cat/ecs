package probe

import "testing"

func TestRepresentativeTableHasMachineSchema(t *testing.T) {
	table := streamMemoryTable(nil)
	if table.Key == "" || len(table.Columns) == 0 || len(table.Columns) != len(table.ColumnKeys) {
		t.Fatalf("representative table schema = %+v", table)
	}
}
