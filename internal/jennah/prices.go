package jennah

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/schema"
	sdk "github.com/alphauslabs/jennah-sdk-go"
	datastorev1 "github.com/alphauslabs/jennah-sdk-go/jennah/datastore/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Price chunk and commit sizes (design D1 of add-shared-prices). Jennah
// refuses a request over 4 MiB.
const (
	MaxPriceChunkBytes  = 1 << 20
	maxPriceCommitBytes = 3 << 20
)

// ErrChunkTooLarge is returned by PublishPrices for a chunk over the cap.
var ErrChunkTooLarge = errors.New("price chunk too large")

// ChunkRef is one chunk listed in a price manifest.
type ChunkRef struct {
	Pack   string `json:"pack"`
	Region string `json:"region"`
	SHA256 string `json:"sha256"`
	Models int    `json:"models"`
}

// PriceVersion is a published price version's manifest.
type PriceVersion struct {
	Number      int64
	PublishedAt time.Time
	Note        string
	Layout      int64
	Chunks      []ChunkRef
}

// Chunk returns the manifest entry for a pack and region.
func (v *PriceVersion) Chunk(pack, region string) (ChunkRef, bool) {
	for _, c := range v.Chunks {
		if c.Pack == pack && c.Region == region {
			return c, true
		}
	}
	return ChunkRef{}, false
}

// PriceChunk is one chunk to publish.
type PriceChunk struct {
	Pack, Region string
	Document     json.RawMessage
	Models       int
}

// HashChunk is the manifest hash of a chunk's document.
func HashChunk(doc []byte) string {
	sum := sha256.Sum256(doc)
	return hex.EncodeToString(sum[:])
}

// LatestPrices returns the newest price version's manifest, or nil when none
// is published.
func (c *Client) LatestPrices(ctx context.Context) (*PriceVersion, error) {
	resp, err := c.c.Dataset(schema.PricesDatasetID).Data.Query(ctx, &datastorev1.QueryDataRequest{
		Relational: &datastorev1.RelationalQuery{
			Table:   schema.PriceVersionsTable,
			OrderBy: []*datastorev1.OrderBy{{Column: schema.ColVersion, Descending: true}},
			Limit:   1,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("read latest price version: %w", err)
	}
	rows := resp.GetRelational().GetRows()
	if len(rows) == 0 {
		return nil, nil
	}
	cols := rows[0].GetColumns()
	v := &PriceVersion{
		Number:      cols[schema.ColVersion].GetInt64Value(),
		PublishedAt: cols[schema.ColPublishedAt].GetTimestampValue().AsTime(),
		Note:        cols[schema.ColNote].GetStringValue(),
		Layout:      cols[schema.ColLayoutVersion].GetInt64Value(),
	}
	if err := json.Unmarshal([]byte(cols[schema.ColManifest].GetJsonValue()), &v.Chunks); err != nil {
		return nil, fmt.Errorf("price version %d: bad manifest: %w", v.Number, err)
	}
	return v, nil
}

// PriceChunkDoc reads one chunk by its full key. It returns nil when the
// chunk doesn't exist.
func (c *Client) PriceChunkDoc(ctx context.Context, version int64, pack, region string) ([]byte, error) {
	resp, err := c.c.Dataset(schema.PricesDatasetID).Data.Query(ctx, &datastorev1.QueryDataRequest{
		Relational: &datastorev1.RelationalQuery{
			Table: schema.PriceChunksTable,
			Where: []*datastorev1.Predicate{
				{Column: schema.ColVersion, Operator: datastorev1.Predicate_OPERATOR_EQUALS, Value: int64Val(version)},
				{Column: schema.ColPack, Operator: datastorev1.Predicate_OPERATOR_EQUALS, Value: strVal(pack)},
				{Column: schema.ColRegion, Operator: datastorev1.Predicate_OPERATOR_EQUALS, Value: strVal(region)},
			},
			Limit: 1,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("read price chunk %s/%s of version %d: %w", pack, region, version, err)
	}
	rows := resp.GetRelational().GetRows()
	if len(rows) == 0 {
		return nil, nil
	}
	return []byte(rows[0].GetColumns()[schema.ColDocument].GetJsonValue()), nil
}

// PublishPrices writes a new price version: its chunks first, in commits under
// the request limit, then its manifest, which makes the version visible. It
// returns the new version, or 0 and no error when the chunks match the latest
// version exactly. A concurrent publish of the same version number returns
// ErrVersionExists.
func (c *Client) PublishPrices(ctx context.Context, chunks []PriceChunk, note string, now time.Time) (int64, error) {
	sort.Slice(chunks, func(i, j int) bool {
		return chunks[i].Pack+"/"+chunks[i].Region < chunks[j].Pack+"/"+chunks[j].Region
	})
	refs := make([]ChunkRef, 0, len(chunks))
	for _, ch := range chunks {
		if len(ch.Document) > MaxPriceChunkBytes {
			return 0, fmt.Errorf("%w: %s/%s is %d bytes, over %d", ErrChunkTooLarge, ch.Pack, ch.Region, len(ch.Document), MaxPriceChunkBytes)
		}
		refs = append(refs, ChunkRef{Pack: ch.Pack, Region: ch.Region, SHA256: HashChunk(ch.Document), Models: ch.Models})
	}
	latest, err := c.LatestPrices(ctx)
	if err != nil {
		return 0, err
	}
	next := int64(1)
	if latest != nil {
		if sameChunks(latest.Chunks, refs) {
			return 0, nil
		}
		next = latest.Number + 1
	}
	ds := c.c.Dataset(schema.PricesDatasetID)

	// Chunks first, batched under the request limit.
	var ops []*datastorev1.RowOperation
	size := 0
	flush := func() error {
		if len(ops) == 0 {
			return nil
		}
		_, err := ds.Data.Commit(ctx, &datastorev1.CommitDataRequest{Operations: ops})
		ops, size = nil, 0
		if err != nil && !sdk.IsAlreadyCommitted(err) {
			return fmt.Errorf("write price chunks of version %d: %w", next, err)
		}
		return nil
	}
	for _, ch := range chunks {
		if size+len(ch.Document) > maxPriceCommitBytes {
			if err := flush(); err != nil {
				return 0, err
			}
		}
		ops = append(ops, &datastorev1.RowOperation{
			Table: schema.PriceChunksTable,
			Type:  datastorev1.OperationType_OPERATION_TYPE_UPSERT,
			Row: &datastorev1.Row{Columns: map[string]*datastorev1.Value{
				schema.ColVersion:  int64Val(next),
				schema.ColPack:     strVal(ch.Pack),
				schema.ColRegion:   strVal(ch.Region),
				schema.ColDocument: {Kind: &datastorev1.Value_JsonValue{JsonValue: string(ch.Document)}},
			}},
		})
		size += len(ch.Document)
	}
	if err := flush(); err != nil {
		return 0, err
	}

	// The manifest last: the version exists once this row does.
	manifest, _ := json.Marshal(refs)
	row := &datastorev1.Row{Columns: map[string]*datastorev1.Value{
		schema.ColVersion:       int64Val(next),
		schema.ColPublishedAt:   {Kind: &datastorev1.Value_TimestampValue{TimestampValue: timestamppb.New(now)}},
		schema.ColLayoutVersion: int64Val(schema.PricesLayoutVersion),
		schema.ColManifest:      {Kind: &datastorev1.Value_JsonValue{JsonValue: string(manifest)}},
	}}
	if note != "" {
		row.Columns[schema.ColNote] = strVal(note)
	}
	_, err = ds.Data.Commit(ctx, &datastorev1.CommitDataRequest{Operations: []*datastorev1.RowOperation{{
		Table: schema.PriceVersionsTable, Type: datastorev1.OperationType_OPERATION_TYPE_INSERT, Row: row,
	}}})
	if sdk.IsAlreadyCommitted(err) {
		return 0, ErrVersionExists
	}
	if err != nil {
		return 0, fmt.Errorf("write price version %d: %w", next, err)
	}
	return next, nil
}

func sameChunks(a, b []ChunkRef) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Pack != b[i].Pack || a[i].Region != b[i].Region || a[i].SHA256 != b[i].SHA256 {
			return false
		}
	}
	return true
}
