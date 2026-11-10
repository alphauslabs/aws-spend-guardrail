package schema

import (
	"testing"

	datastorev1 "github.com/alphauslabs/jennah-sdk-go/jennah/datastore/v1"
)

func columns(tables []*datastorev1.TableDeclaration, table string) map[string]datastorev1.ColumnType {
	out := map[string]datastorev1.ColumnType{}
	for _, t := range tables {
		if t.GetName() != table {
			continue
		}
		for _, c := range t.GetColumns() {
			out[c.GetName()] = c.GetType()
		}
	}
	return out
}

// The guard and CLI read and write exactly these columns (internal/jennah).
func TestDeclaredColumnsMatchWhatTheGuardUses(t *testing.T) {
	cfg := columns(ConfigTables(), ConfigTable)
	for name, typ := range map[string]datastorev1.ColumnType{
		ColVersion:       datastorev1.ColumnType_COLUMN_TYPE_INT64,
		ColDocument:      datastorev1.ColumnType_COLUMN_TYPE_JSON,
		ColPublishedAt:   datastorev1.ColumnType_COLUMN_TYPE_TIMESTAMP,
		ColNote:          datastorev1.ColumnType_COLUMN_TYPE_STRING,
		ColLayoutVersion: datastorev1.ColumnType_COLUMN_TYPE_INT64,
	} {
		if cfg[name] != typ {
			t.Errorf("config_versions.%s: declared %v, used as %v", name, cfg[name], typ)
		}
	}
	ev := columns(InboxTables(), EventsTable)
	for name, typ := range map[string]datastorev1.ColumnType{
		ColEventID:       datastorev1.ColumnType_COLUMN_TYPE_STRING,
		ColOccurredAt:    datastorev1.ColumnType_COLUMN_TYPE_TIMESTAMP,
		ColKind:          datastorev1.ColumnType_COLUMN_TYPE_STRING,
		ColScope:         datastorev1.ColumnType_COLUMN_TYPE_STRING,
		ColConfigVersion: datastorev1.ColumnType_COLUMN_TYPE_INT64,
		ColGuardVersion:  datastorev1.ColumnType_COLUMN_TYPE_STRING,
		ColDetail:        datastorev1.ColumnType_COLUMN_TYPE_JSON,
		ColLayoutVersion: datastorev1.ColumnType_COLUMN_TYPE_INT64,
	} {
		if ev[name] != typ {
			t.Errorf("events.%s: declared %v, used as %v", name, ev[name], typ)
		}
	}
	if pk := ConfigTables()[0].GetPrimaryKey(); len(pk) != 1 || pk[0] != ColVersion {
		t.Errorf("config_versions key %v", pk)
	}
	if pk := InboxTables()[0].GetPrimaryKey(); len(pk) != 1 || pk[0] != ColEventID {
		t.Errorf("events key %v", pk)
	}
}

func TestMSPIDs(t *testing.T) {
	for _, ok := range []string{"msp_a", "acme-cloud", "m1"} {
		if err := ValidateMSPID(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "MSP", "msp.a", "_msp", "msp-", "a b"} {
		if ValidateMSPID(bad) == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
	if ConfigDatasetID("msp_a") != "guardrail.msp_a.config" || MSPSelector("msp_a") != "guardrail.msp_a.*" {
		t.Error("dataset naming changed")
	}
}

func TestReaderCannotWriteAndWriterOnlyWrites(t *testing.T) {
	for _, c := range GuardCredentials("msp_a") {
		for _, p := range c.Permissions {
			if c.Name == "reader" && p == "datastore.data:write" {
				t.Error("reader must not write")
			}
		}
		if c.Name == "writer" && (len(c.Permissions) != 1 || c.Permissions[0] != "datastore.data:write" || c.DatasetID != InboxDatasetID("msp_a")) {
			t.Errorf("writer reach: %+v", c)
		}
	}
}
