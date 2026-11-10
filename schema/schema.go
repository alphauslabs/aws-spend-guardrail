// Package schema defines the Jennah datasets and tables the guard reads and
// writes. It is the contract between the guard, the CLI and, later, the
// learning agent.
//
// Layout changes may only add tables or columns, so a guard written for an
// older layout keeps working against a newer one. Bump LayoutVersion on every
// change.
package schema

import (
	"fmt"
	"strings"

	datastorev1 "github.com/alphauslabs/jennah-sdk-go/jennah/datastore/v1"
)

// LayoutVersion is the version of the table layouts below.
const LayoutVersion = 1

// Config dataset: one table of immutable config versions.
const (
	ConfigTable       = "config_versions"
	ColVersion        = "version"      // INT64, primary key
	ColDocument       = "document"     // JSON
	ColPublishedAt    = "published_at" // TIMESTAMP
	ColNote           = "note"         // STRING, nullable
	ColLayoutVersion  = "layout_version"
	EventsTable       = "events"
	ColEventID        = "event_id"    // STRING, primary key
	ColOccurredAt     = "occurred_at" // TIMESTAMP
	ColKind           = "kind"        // STRING
	ColScope          = "scope"       // STRING, nullable
	ColConfigVersion  = "config_version"
	ColGuardVersion   = "guard_version"
	ColDetail         = "detail" // JSON
	maxMSPIDLen       = 90       // keeps guardrail.<msp>.<payer>.config within Jennah's 128
	datasetPrefix     = "guardrail."
	configDatasetPart = ".config"
	inboxDatasetPart  = ".inbox"
)

// Event kinds written to the inbox.
const (
	KindFreeze           = "freeze"
	KindWouldFreeze      = "would_freeze"
	KindLift             = "lift"
	KindConfigApplied    = "config_applied"           // a version applied in full
	KindConfigPartial    = "config_partially_applied" // only a loosening version's tightening parts
	KindConfigRejected   = "config_rejected"
	KindLooseningRaised  = "loosening_raised"
	KindLooseningOK      = "loosening_approved"
	KindLooseningDenied  = "loosening_rejected"
	KindLooseningExpired = "loosening_expired"
	KindLooseningVoid    = "loosening_cancelled"
	KindDrift            = "drift"
	KindHeartbeat        = "heartbeat"
)

// ValidateMSPID checks that an MSP id can form dataset ids under Jennah's id
// grammar (lowercase letters, digits, and inner '_' or '-'). Dots are refused
// so the per-MSP selector guardrail.<msp>.* reaches exactly that MSP's payer
// orgs.
func ValidateMSPID(msp string) error {
	if msp == "" || len(msp) > maxMSPIDLen {
		return fmt.Errorf("msp id must be 1 to %d characters", maxMSPIDLen)
	}
	for i := 0; i < len(msp); i++ {
		c := msp[i]
		edge := i == 0 || i == len(msp)-1
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case !edge && (c == '_' || c == '-'):
		default:
			return fmt.Errorf("msp id %q: use lowercase letters, digits, and '_' or '-' inside", msp)
		}
	}
	return nil
}

// ValidatePayer checks a payer account id: 12 digits.
func ValidatePayer(payer string) error {
	if len(payer) != 12 || strings.Trim(payer, "0123456789") != "" {
		return fmt.Errorf("payer account %q: must be a 12-digit account id", payer)
	}
	return nil
}

// An MSP can have several payer orgs, each with its own guard and its own pair
// of datasets: guardrail.<msp>.<payer>.config and guardrail.<msp>.<payer>.inbox.

// ConfigDatasetID is the config dataset of one payer org of an MSP.
func ConfigDatasetID(msp, payer string) string {
	return datasetPrefix + msp + "." + payer + configDatasetPart
}

// InboxDatasetID is the inbox dataset of one payer org of an MSP.
func InboxDatasetID(msp, payer string) string {
	return datasetPrefix + msp + "." + payer + inboxDatasetPart
}

// MSPSelector is the dataset selector that reaches every payer org of an MSP.
func MSPSelector(msp string) string { return datasetPrefix + msp + ".*" }

// PayerSelector is the dataset selector that reaches one payer org's datasets.
func PayerSelector(msp, payer string) string { return datasetPrefix + msp + "." + payer + ".*" }

func col(name string, t datastorev1.ColumnType, nullable bool) *datastorev1.ColumnDeclaration {
	return &datastorev1.ColumnDeclaration{Name: name, Type: t, Nullable: nullable}
}

// ConfigTables declares the config dataset's tables.
func ConfigTables() []*datastorev1.TableDeclaration {
	return []*datastorev1.TableDeclaration{{
		Name: ConfigTable,
		Columns: []*datastorev1.ColumnDeclaration{
			col(ColVersion, datastorev1.ColumnType_COLUMN_TYPE_INT64, false),
			col(ColDocument, datastorev1.ColumnType_COLUMN_TYPE_JSON, false),
			col(ColPublishedAt, datastorev1.ColumnType_COLUMN_TYPE_TIMESTAMP, false),
			col(ColNote, datastorev1.ColumnType_COLUMN_TYPE_STRING, true),
			col(ColLayoutVersion, datastorev1.ColumnType_COLUMN_TYPE_INT64, false),
		},
		PrimaryKey: []string{ColVersion},
	}}
}

// InboxTables declares the inbox dataset's tables.
func InboxTables() []*datastorev1.TableDeclaration {
	return []*datastorev1.TableDeclaration{{
		Name: EventsTable,
		Columns: []*datastorev1.ColumnDeclaration{
			col(ColEventID, datastorev1.ColumnType_COLUMN_TYPE_STRING, false),
			col(ColOccurredAt, datastorev1.ColumnType_COLUMN_TYPE_TIMESTAMP, false),
			col(ColKind, datastorev1.ColumnType_COLUMN_TYPE_STRING, false),
			col(ColScope, datastorev1.ColumnType_COLUMN_TYPE_STRING, true),
			col(ColConfigVersion, datastorev1.ColumnType_COLUMN_TYPE_INT64, true),
			col(ColGuardVersion, datastorev1.ColumnType_COLUMN_TYPE_STRING, false),
			col(ColDetail, datastorev1.ColumnType_COLUMN_TYPE_JSON, true),
			col(ColLayoutVersion, datastorev1.ColumnType_COLUMN_TYPE_INT64, false),
		},
		PrimaryKey: []string{ColEventID},
	}}
}

// Permissions each guard credential needs, and on which dataset.
type CredentialNeeds struct {
	Name        string
	DatasetID   string
	Permissions []string
}

// GuardCredentials lists the reach of one payer org's guard reader and writer.
func GuardCredentials(msp, payer string) []CredentialNeeds {
	return []CredentialNeeds{
		{
			Name:        "reader",
			DatasetID:   ConfigDatasetID(msp, payer),
			Permissions: []string{"datastore.data:read", "datastore.schema:read", "approval.requests:create", "approval.requests:read", "approval.requests:cancel"},
		},
		{
			Name:        "writer",
			DatasetID:   InboxDatasetID(msp, payer),
			Permissions: []string{"datastore.data:write"},
		},
	}
}
