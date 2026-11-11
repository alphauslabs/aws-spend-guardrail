package schema

import (
	"strings"
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
	if ConfigDatasetID("msp_a", "123456789012") != "guardrail.msp_a.123456789012.config" ||
		InboxDatasetID("msp_a", "123456789012") != "guardrail.msp_a.123456789012.inbox" ||
		MSPSelector("msp_a") != "guardrail.msp_a.*" ||
		PayerSelector("msp_a", "123456789012") != "guardrail.msp_a.123456789012.*" {
		t.Error("dataset naming changed")
	}
	long := strings.Repeat("a", maxMSPIDLen)
	if ValidateMSPID(long) != nil || len(ConfigDatasetID(long, "123456789012")) > 128 {
		t.Error("the longest MSP id must still form a valid dataset id")
	}
	if ValidatePayer("123456789012") != nil || ValidatePayer("12345") == nil || ValidatePayer("12345678901a") == nil {
		t.Error("payer validation")
	}
}

func TestReaderCannotWriteAndWriterOnlyWrites(t *testing.T) {
	for _, c := range GuardCredentials("msp_a", "123456789012") {
		for _, p := range c.Permissions {
			if c.Name == "reader" && p == "datastore.data:write" {
				t.Error("reader must not write")
			}
		}
		if c.Name == "writer" && (len(c.Permissions) != 1 || c.Permissions[0] != "datastore.data:write" || c.DatasetID != InboxDatasetID("msp_a", "123456789012")) {
			t.Errorf("writer reach: %+v", c)
		}
	}
}

func TestInboxKeepsEventsFor400Days(t *testing.T) {
	inbox := InboxTables()[0]
	r := inbox.GetRetention()
	if r.GetColumn() != ColOccurredAt || r.GetDays() != 400 || InboxRetentionDays != 400 {
		t.Fatalf("inbox retention: %v", r)
	}
	if ConfigTables()[0].GetRetention() != nil {
		t.Fatal("config versions are kept forever: no retention on the config table")
	}
}
