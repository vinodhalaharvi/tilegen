// Scaffolded by tilegen. This file is yours: tilegen never overwrites it.

package tiles

import (
	"fmt"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

// The dynamodb tile, registered from a (tile-spec dynamodb ...) form.
// DynamoDB: one table per entity keyed by id, items marshalled attribute by attribute
func init() {
	compile.RegisterStoreBackend(&compile.Backend{
		Name: "dynamodb",
		Tile: "dynamodb",
		Doc:  "DynamoDB: one table per entity keyed by id, items marshalled attribute by attribute",
		Cost: compile.Cost{
			{Dim: "llm-work", Value: 8, Source: "derived", Note: "attribute values, the key schema, pagination and results in no order, written by hand for every method"},
			{Dim: "maintenance", Value: 5, Source: "derived", Note: "no schema beyond the key, a list or a count is a paid read of the whole table, and tests need an emulator or a live table"},
			{Dim: "dependency", Value: 4, Source: "derived", Note: "the AWS SDK's module tree"},
			{Dim: "runtime", Value: 2, Source: "derived", Note: "an HTTPS round trip per call"},
			{Dim: "operations", Value: 3, Source: "derived", Note: "nothing to patch or back up by hand, but capacity, IAM and cost to watch"},
		},
		IllegalWhen: map[string]string{
			"no-broker": "a cloud service the program cannot start without: an account, a table, credentials and a network path to them",
		},
		Unverified: map[string]string{
			"inbox":           "an inbox is one transaction over a handler's writes and the record that it ran; TransactWriteItems can write both, and this tile does not generate one",
			"lookup-by-field": "a Query on another field needs a global secondary index, and a Scan with a filter reads the whole table; this tile creates neither",
			"outbox":          "an outbox is one transaction over the entity and the event; TransactWriteItems can write both, and this tile does not generate one",
		},
		Satisfies: map[string]string{
			"cross-process": "a regional service every process reaches over HTTPS",
			"durable":       "a write is acknowledged once it is stored in more than one availability zone",
		},
		Imports: map[string]string{
			"dynamodb": "github.com/aws/aws-sdk-go-v2/service/dynamodb",
		},
		Topics:       []string{"dynamodb", "aws"},
		ErrorPackage: "github.com/aws/aws-sdk-go-v2/service/dynamodb/types",
		Engine:       "dynamodb",
		Implement:    (&dynamodbTile{}).Implement,
	})
}

// dynamodbTile carries the parts of the dynamodb tile that are judgment rather than
// declaration. It holds nothing: it gives the two holes an address.
type dynamodbTile struct {
}

func (s *dynamodbTile) Implement(in compile.StoreInput) (compile.StoreParts, error) {
	return compile.StoreParts{
		Fields: []*sexp.Node{
			sexp.L(sexp.Sym("field"), sexp.Sym("client"), sexp.Sym("*dynamodb.Client")),
			sexp.L(sexp.Sym("field"), sexp.Sym("table"), sexp.Sym("string")),
		},
		Params: sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("client"), sexp.Sym("*dynamodb.Client"))),
		Body:   fmt.Sprintf("return &%s{client: client, table: %q}", in.Impl, compile.LowerFirstWord(in.Entity)+"s"),
		Hint:   s.Hint,
	}, nil
}

func (s *dynamodbTile) Hint(meth *sexp.Node) string {
	kind, _ := compile.MethodOp(meth)
	switch kind {
	case "get":
		return "Call s.client.GetItem with TableName aws.String(s.table) and Key map[string]types.AttributeValue{\"id\": &types.AttributeValueMemberS{Value: the ID as a string}}. An output with no Item means the key is absent: return ErrNotFound. Otherwise attributevalue.UnmarshalMap(out.Item, &value)."
	case "list":
		return "Page through the table with dynamodb.NewScanPaginator(s.client, &dynamodb.ScanInput{TableName: aws.String(s.table)}), unmarshal each page with attributevalue.UnmarshalListOfMaps, and sort the result by ID: a Scan returns items in no useful order, and reads the whole table."
	case "save":
		return "attributevalue.MarshalMap the value, set the item's \"id\" attribute to the ID as a string (the table's key), and call s.client.PutItem, which replaces the whole item."
	case "delete":
		return "Call s.client.DeleteItem with the same TableName and Key as Get. Deleting a missing item is not an error."
	case "count":
		return "Page through a Scan with Select: types.SelectCount and add up each page's Count. DescribeTable's ItemCount is refreshed only every few hours and is not the answer."
	}
	// The *-by ops reach here only as custom methods: nothing in this tile
	// builds an index, and unverified keeps it out of a store that asks to
	// look up by a field.
	return compile.CustomHint(meth)
}
