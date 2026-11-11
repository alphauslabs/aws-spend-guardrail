package jennahtest

import (
	"context"
	"testing"

	datastorev1 "github.com/alphauslabs/jennah-sdk-go/jennah/datastore/v1"
)

func TestFakeRetention(t *testing.T) {
	ctx := context.Background()
	s, _ := Start(t)
	const ds = "d"
	if _, err := s.CreateDataset(ctx, &datastorev1.CreateDatasetRequest{DatasetId: ds}); err != nil {
		t.Fatal(err)
	}
	decl := func(r *datastorev1.RowRetention) *datastorev1.TableStatus {
		resp, err := s.DeclareTables(ctx, &datastorev1.DeclareTablesRequest{DatasetId: ds, Tables: []*datastorev1.TableDeclaration{{
			Name: "events", PrimaryKey: []string{"id"}, Retention: r,
			Columns: []*datastorev1.ColumnDeclaration{{Name: "id", Type: datastorev1.ColumnType_COLUMN_TYPE_STRING}},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		return resp.GetTables()[0]
	}
	inForce := func() *datastorev1.RowRetention {
		resp, err := s.GetSchema(ctx, &datastorev1.GetSchemaRequest{DatasetId: ds})
		if err != nil {
			t.Fatal(err)
		}
		return resp.GetTables()[0].GetRetention()
	}

	decl(nil)
	if inForce() != nil {
		t.Fatal("no retention declared, none in force")
	}
	st := decl(&datastorev1.RowRetention{Column: "ts", Days: 400})
	if inForce().GetDays() != 400 || st.GetRetentionChange() == nil || !st.GetRetentionChange().GetRemovesRows() {
		t.Fatalf("set: in force %v, status %v", inForce(), st)
	}
	declares := s.Declares
	if st := decl(nil); inForce().GetDays() != 400 || st.GetRetentionChange() != nil || s.Declares != declares {
		t.Fatalf("an unset retention keeps what the table has: %v %v", inForce(), st)
	}
	if st := decl(&datastorev1.RowRetention{Column: "ts", Days: 800}); st.GetRetentionChange().GetRemovesRows() {
		t.Fatal("lengthening removes no rows")
	}
	if st := decl(&datastorev1.RowRetention{Days: 0}); inForce() != nil || st.GetRetentionChange().GetTo() != nil {
		t.Fatalf("days 0 removes the retention: %v %v", inForce(), st)
	}
}
